package store

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// ReadRoundTimeline reads a round's steps with their events and cards from
// one read-only repeatable-read snapshot, as PlanViewPage reads the plan:
// one read for the Work page instead of one plan read per step. With node
// set, only that step, which must be under root. Every source is read in
// the transaction; FoldStepTimeline and FoldStepCards say what it means.
func ReadRoundTimeline(ctx context.Context, db DB, root, node string) (map[string]any, error) {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	type step struct {
		ID, Label, Title, State, Kind string
	}
	rows, err := tx.Query(ctx, viewTree+`SELECT n.plan_node_id,n.label,n.title,COALESCE(n.state,''),n.node_kind FROM tree JOIN agentrt.plan_node n USING(plan_node_id) ORDER BY n.ordinal,n.plan_node_id LIMIT 501`, root)
	if err != nil {
		return nil, err
	}
	steps, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (step, error) {
		var s step
		return s, r.Scan(&s.ID, &s.Label, &s.Title, &s.State, &s.Kind)
	})
	if err != nil {
		return nil, err
	}
	if len(steps) == 0 {
		return nil, pgx.ErrNoRows
	}
	if len(steps) > 500 {
		return nil, invalid("plan exceeds 500 nodes")
	}
	if node != "" {
		i := slices.IndexFunc(steps, func(s step) bool { return s.ID == node })
		if i < 0 {
			return nil, invalid("%s is not a step of %s", node, root)
		}
		steps = steps[i : i+1]
	}
	ids := make([]string, len(steps))
	for i, s := range steps {
		ids[i] = s.ID
	}
	warnings := []string{}

	// The steps' own events, and their tasks' events (event_task_idx).
	tasks := map[string]string{} // task → step
	repos := []string{}
	rows, err = tx.Query(ctx, `SELECT task_id,plan_node_id,COALESCE(repo,'') FROM agentrt.task WHERE plan_node_id=ANY($1) ORDER BY task_id`, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, on, repo string
		if err = rows.Scan(&id, &on, &repo); err != nil {
			rows.Close()
			return nil, err
		}
		tasks[id] = on
		if repo != "" && !slices.Contains(repos, repo) {
			repos = append(repos, repo)
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	taskIDs := make([]string, 0, len(tasks))
	for id := range tasks {
		taskIDs = append(taskIDs, id)
	}
	slices.Sort(taskIDs)
	planEvents, err := timelineEvents(ctx, tx, `plan_node_id=ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	taskEvents, err := timelineEvents(ctx, tx, `task_id=ANY($1)`, taskIDs)
	if err != nil {
		return nil, err
	}

	// Attempts on the external host are the coordinator's own work.
	externalHosts := []string{}
	if err = tx.QueryRow(ctx, `SELECT COALESCE(array_agg(DISTINCT h.host_id ORDER BY h.host_id),'{}') FROM agentrt.attempt a JOIN agentrt.host h USING(host_id) WHERE a.task_id=ANY($1) AND h.host_kind=$2`, taskIDs, HostExternal).Scan(&externalHosts); err != nil {
		return nil, err
	}

	// The landing streams of the tasks' repositories and the STG release
	// stream, read as readPlanFacts reads them, keeping the entries. A
	// stream that does not fold is a warning, and its entries are left out.
	streams := []string{StgReleaseStream}
	for _, r := range repos {
		if s := "landing:" + r; !slices.Contains(streams, s) {
			streams = append(streams, s)
		}
	}
	slices.Sort(streams)
	rows, err = tx.Query(ctx, `SELECT event_stream_id,event_id,at,body_json->'landing' FROM agentrt.event WHERE event_type='note' AND actor_id='controller' AND event_stream_id=ANY($1) AND body_json ? 'landing' ORDER BY event_stream_id,seq`, streams)
	if err != nil {
		return nil, err
	}
	type note struct {
		stream string
		id     int64
		at     time.Time
		raw    []byte
	}
	raw, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (note, error) {
		var n note
		return n, r.Scan(&n.stream, &n.id, &n.at, &n.raw)
	})
	if err != nil {
		return nil, err
	}
	ops := map[string]landing.Operation{}
	var entries []StepLandingEntry
	states := map[string]*landing.State{}
	kept := map[string][]StepLandingEntry{}
	bad := map[string]bool{}
	for _, e := range raw {
		if bad[e.stream] {
			continue
		}
		state := states[e.stream]
		if state == nil {
			state = &landing.State{}
			states[e.stream] = state
		}
		record, err := parseLandingNote(e.raw)
		if err == nil {
			err = state.Apply(landing.Entry{At: e.at, Record: record})
		}
		if err != nil {
			bad[e.stream] = true
			warnings = append(warnings, fmt.Sprintf("Landing stream %s does not fold: %v", e.stream, err))
			continue
		}
		kept[e.stream] = append(kept[e.stream], StepLandingEntry{EventID: e.id, Entry: landing.Entry{At: e.at, Record: record}})
	}
	for _, s := range streams {
		if bad[s] || states[s] == nil {
			continue
		}
		entries = append(entries, kept[s]...)
		for id, op := range states[s].Operations {
			ops[id] = op
		}
	}

	// STG-verified revisions of the STG release protocol, as the stg_pending
	// and stg_verified entries the timeline folds.
	stg, err := StgReleaseState(ctx, tx)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("STG release stream %s does not fold: %v", StgReleaseStream, err))
	} else {
		released, err := stgVerifiedEntries(ctx, tx, stg)
		if err != nil {
			return nil, err
		}
		entries = append(entries, released...)
	}

	// The entries of each step: its tasks' operations, and the STG
	// operations that carry a revision one of them landed.
	opStep := map[string]string{}
	for id, op := range ops {
		if s, ok := tasks[op.Task]; ok && op.Task != "" {
			opStep[id] = s
		}
	}
	landedBy := map[string][]string{} // R → steps that landed it
	for _, e := range entries {
		if s := opStep[e.Record.String("op")]; s != "" && e.Record.String("kind") == "landed" {
			landedBy[e.Record.String("R")] = append(landedBy[e.Record.String("R")], s)
		}
	}
	carries := map[string][]string{} // STG op → steps whose landed R it carries
	for _, e := range entries {
		if e.Record.String("kind") != "stg_pending" {
			continue
		}
		rs, _ := e.Record["R"].([]any)
		for _, r := range rs {
			s, _ := r.(string)
			carries[e.Record.String("op")] = append(carries[e.Record.String("op")], landedBy[s]...)
		}
	}
	landingOf := map[string][]StepLandingEntry{}
	verified := false
	for _, e := range entries {
		op := e.Record.String("op")
		on := []string{}
		if s := opStep[op]; s != "" {
			on = append(on, s)
		}
		if k := e.Record.String("kind"); k == "stg_pending" || k == "stg_verified" {
			on = append(on, carries[op]...)
			verified = verified || (k == "stg_verified" && len(carries[op]) > 0)
		}
		slices.Sort(on)
		for _, s := range slices.Compact(on) {
			landingOf[s] = append(landingOf[s], e)
		}
	}

	// PROD deploys, read only when a step reached STG.
	deploys := []StepTimelineEvent{}
	if verified {
		if deploys, err = timelineEvents(ctx, tx, `event_type='deploy_seen' AND body_json->>'env'=$1`, "prod"); err != nil {
			return nil, err
		}
	}

	// External work of the round, every recorded version of it.
	ext, err := snapshotExternal(ctx, tx, root, now)
	if err != nil {
		return nil, err
	}
	extIDs := []string{}
	for _, e := range ext {
		if n, _ := e["plan_node_id"].(string); slices.Contains(ids, n) {
			extIDs = append(extIDs, e["external_work_id"].(string))
		}
	}
	externalOf := map[string][]StepExternalRecord{}
	if len(extIDs) > 0 {
		rows, err = tx.Query(ctx, `SELECT event_id,at,body_json->'external_work' FROM agentrt.event WHERE event_type='note' AND body_json ? 'external_work' AND body_json->'external_work'->>'root'=$1 AND body_json->'external_work'->>'external_work_id'=ANY($2) ORDER BY event_id`, root, extIDs)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var r StepExternalRecord
			var b []byte
			if err = rows.Scan(&r.EventID, &r.At, &b); err == nil {
				err = json.Unmarshal(b, &r.Note)
			}
			if err != nil {
				rows.Close()
				return nil, err
			}
			externalOf[r.Note.Node] = append(externalOf[r.Note.Node], r)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return nil, err
		}
	}

	// Cards: the bindings of the steps, the landing and STG cards of their
	// tasks, and the copies of the questions and approvals they name.
	in := StepCardsInput{Tasks: tasks, Ops: map[string]landing.Operation{}}
	for id, op := range ops {
		if opStep[id] != "" {
			in.Ops[id] = op
		}
	}
	rows, err = tx.Query(ctx, `SELECT plan_node_id,bound_kind,bound_ref,event_id FROM agentrt.plan_node_binding WHERE plan_node_id=ANY($1) ORDER BY event_id`, ids)
	if err != nil {
		return nil, err
	}
	in.Bindings, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (StepBinding, error) {
		var b StepBinding
		return b, r.Scan(&b.PlanNodeID, &b.Kind, &b.Ref, &b.EventID)
	})
	if err != nil {
		return nil, err
	}
	questions, approvals := []string{}, []string{}
	for _, b := range in.Bindings {
		if b.Kind == StepCardQuestion {
			questions = append(questions, b.Ref)
		} else {
			approvals = append(approvals, b.Ref)
		}
	}
	cardStreams := []string{}
	for _, r := range repos {
		cardStreams = append(cardStreams, CardsStream(r))
	}
	slices.Sort(cardStreams)
	for _, s := range cardStreams {
		cards, err := LandingCards(ctx, tx, s)
		if err != nil {
			return nil, err
		}
		times, err := cardTimes(ctx, tx, s)
		if err != nil {
			return nil, err
		}
		for _, prep := range slices.Sorted(func(yield func(string) bool) {
			for p := range cards {
				if !yield(p) {
					return
				}
			}
		}) {
			c := cards[prep]
			_, ofTask := tasks[c.Root]
			if !ofTask && (c.Op == "" || in.Ops[c.Op].Task == "") && !slices.Contains(questions, c.QuestionID) {
				continue
			}
			t := times[prep]
			in.Cards = append(in.Cards, StepCardRecord{Card: c, AckedAt: t.acked, ClosedAt: t.closed, EventID: t.event})
			if c.QuestionID != "" {
				questions = append(questions, c.QuestionID)
			}
		}
	}
	if in.Questions, err = seenCopies(ctx, tx, "question_seen", "question", questions, taskIDs); err != nil {
		return nil, err
	}
	if in.Approvals, err = seenCopies(ctx, tx, "approval_seen", "approval", approvals, taskIDs); err != nil {
		return nil, err
	}
	for _, e := range planEvents {
		if e.Type == "owner_signoff" {
			ref, _ := e.Body["ref"].(string)
			in.Signoffs = append(in.Signoffs, SignoffEvent{PlanNodeID: e.PlanNode, Ref: ref, By: e.Actor, At: e.At, EventID: e.EventID})
		}
	}
	cards := FoldStepCards(in)

	planOf, tasksOf := map[string][]StepTimelineEvent{}, map[string][]StepTimelineEvent{}
	for _, e := range planEvents {
		planOf[e.PlanNode] = append(planOf[e.PlanNode], e)
	}
	for _, e := range taskEvents {
		tasksOf[tasks[e.TaskID]] = append(tasksOf[tasks[e.TaskID]], e)
	}
	out := []map[string]any{}
	for _, s := range steps {
		events := FoldStepTimeline(StepTimelineInput{Step: s.ID, Plan: planOf[s.ID], Tasks: tasksOf[s.ID], Landing: landingOf[s.ID],
			Deploys: deploys, External: externalOf[s.ID], ExternalHosts: externalHosts})
		stepCards := cards[s.ID]
		if stepCards == nil {
			stepCards = []StepCard{}
		}
		out = append(out, map[string]any{"plan_node_id": s.ID, "label": s.Label, "title": s.Title, "state": s.State, "node_kind": s.Kind,
			"events": events, "cards": stepCards})
	}
	unbound := cards[""]
	if unbound == nil {
		unbound = []StepCard{}
	}
	return map[string]any{"schema": "agentrt.plan_timeline_response/v1", "root": root, "as_of": timeText(now), "steps": out,
		"unbound_cards": unbound, "warnings": warnings}, nil
}

// parseLandingNote parses a note.landing record as readPlanFacts does.
func parseLandingNote(raw []byte) (landing.Record, error) {
	v, err := canon.Parse(raw)
	if err != nil {
		return nil, err
	}
	return landing.Parse(v)
}

// timelineEvents reads the events a condition on one argument selects, in
// event order.
func timelineEvents(ctx context.Context, tx pgx.Tx, where string, arg any) ([]StepTimelineEvent, error) {
	rows, err := tx.Query(ctx, `SELECT event_id,event_type,at,actor_id,COALESCE(plan_node_id,''),COALESCE(task_id,''),COALESCE(attempt_id,''),body_json FROM agentrt.event WHERE `+where+` ORDER BY event_id`, arg)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (StepTimelineEvent, error) {
		var e StepTimelineEvent
		return e, r.Scan(&e.EventID, &e.Type, &e.At, &e.Actor, &e.PlanNode, &e.TaskID, &e.AttemptID, &e.Body)
	})
}

// stgVerifiedEntries are the STG release protocol's verified tries as the
// stg_pending and stg_verified entries of one operation per try: its
// members' revisions when it was snapshotted, and the P it verified.
func stgVerifiedEntries(ctx context.Context, tx pgx.Tx, s StgState) ([]StepLandingEntry, error) {
	type try struct {
		op    string
		rs    []any
		snap  StgEvent
		check StgEvent
	}
	var tries []try
	var seqs []int64
	for _, batch := range slices.Sorted(func(yield func(string) bool) {
		for b := range s.Batches {
			if !yield(b) {
				return
			}
		}
	}) {
		b := s.Batches[batch]
		rs := []string{}
		for m := range b.Members {
			if m.R != "" && !slices.Contains(rs, m.R) {
				rs = append(rs, m.R)
			}
		}
		slices.Sort(rs)
		for _, n := range slices.Sorted(func(yield func(int64) bool) {
			for n := range b.Tries {
				if !yield(n) {
					return
				}
			}
		}) {
			t := b.Tries[n]
			if t == nil || t.Verify.Seq == 0 || t.Verify.Record.String("outcome") != "verified" || len(rs) == 0 {
				continue
			}
			x := try{op: fmt.Sprintf("stg-release:%s:%d", batch, n), snap: t.Snapshot, check: t.Verify}
			for _, r := range rs {
				x.rs = append(x.rs, r)
			}
			if x.snap.Seq == 0 {
				x.snap = t.Verify
			}
			tries = append(tries, x)
			seqs = append(seqs, x.snap.Seq, x.check.Seq)
		}
	}
	if len(tries) == 0 {
		return nil, nil
	}
	ids := map[int64]int64{}
	rows, err := tx.Query(ctx, `SELECT seq,event_id FROM agentrt.event WHERE event_stream_id=$1 AND seq=ANY($2)`, StgReleaseStream, seqs)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var seq, id int64
		if err = rows.Scan(&seq, &id); err != nil {
			rows.Close()
			return nil, err
		}
		ids[seq] = id
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	out := []StepLandingEntry{}
	for _, t := range tries {
		out = append(out,
			StepLandingEntry{EventID: ids[t.snap.Seq], Entry: landing.Entry{At: t.snap.At, Record: landing.Record{"kind": "stg_pending", "op": t.op, "R": t.rs}}},
			StepLandingEntry{EventID: ids[t.check.Seq], Entry: landing.Entry{At: t.check.At, Record: landing.Record{"kind": "stg_verified", "op": t.op, "P": t.check.Record.String("P")}}})
	}
	return out, nil
}

// cardMoments are a card's prepared event and the times it was acked and
// closed, which LandingCards does not keep.
type cardMoments struct {
	event         int64
	acked, closed time.Time
}

func cardTimes(ctx context.Context, tx pgx.Tx, stream string) (map[string]cardMoments, error) {
	rows, err := tx.Query(ctx, `SELECT event_id,at,body_json FROM agentrt.event WHERE event_stream_id=$1 AND event_type='note' ORDER BY seq`, stream)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]cardMoments{}
	for rows.Next() {
		var id int64
		var at time.Time
		var body map[string]any
		if err := rows.Scan(&id, &at, &body); err != nil {
			return nil, err
		}
		for _, k := range []string{"landing_card_prepared", "landing_card_acked", "landing_card_closed"} {
			m, ok := body[k].(map[string]any)
			if !ok {
				continue
			}
			prep, _ := m["prep"].(string)
			c := out[prep]
			switch k {
			case "landing_card_prepared":
				c = cardMoments{event: id}
			case "landing_card_acked":
				c.acked = at
			case "landing_card_closed":
				c.closed = at
			}
			out[prep] = c
		}
	}
	return out, rows.Err()
}

// seenCopies reads the approval_seen or question_seen copies of the refs
// given, or of the tasks given, in event order.
func seenCopies(ctx context.Context, tx pgx.Tx, typ, key string, refs, tasks []string) ([]SeenCopy, error) {
	if len(refs) == 0 && len(tasks) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `SELECT event_id,at,COALESCE(task_id,''),COALESCE(body_json->>$2,''),COALESCE(body_json->>'status',''),COALESCE(body_json->>'option',''),COALESCE(body_json->>'answered_by',''),COALESCE(body_json->>'answered_at','') FROM agentrt.event WHERE event_type=$1 AND (body_json->>$2=ANY($3) OR task_id=ANY($4)) ORDER BY event_id`, typ, key, refs, tasks)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (SeenCopy, error) {
		var c SeenCopy
		return c, r.Scan(&c.EventID, &c.At, &c.TaskID, &c.Ref, &c.Status, &c.Option, &c.AnsweredBy, &c.AnsweredAt)
	})
}
