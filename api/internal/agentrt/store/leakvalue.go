package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// The leak value projection (GET /leak-value): next to the value of each
// check, the errors that reached STG or PROD, by the place they leaked from,
// per ISO week in UTC. Computed on every read from the runtime's own rows,
// in one read-only snapshot, never stored, as the progress projection is
// (progress.go). A leak to STG is an STG release try whose verify outcome
// is failed or which was quarantined (note.stg_release on
// landing:bifrost-platform, stg_release_protocol.go), counted once per
// member root; a try whose verification the records leave unknown is
// counted as unknown. A leak to PROD is an issue whose latest
// classification is a fault (issues.go), through its work's mission to the
// task that names it. A place is the module above the task's plan node:
// the nearest arch row, else the plan item (the schema puts a task's step
// under an item, never under an arch row, so today it is the item), as the
// plan view labels modules; "<repo>, off plan" for an off-plan task, and
// "unattributed" where no task can be named. A place that keeps leaking is marked for a check to be
// added back; the controller's pass (controller/leakvalue.go) proposes the
// goal and records a leak_value note on the controller's stream.

// LeakValueWeeksDefault and LeakValueWeeksMax bound the full weeks a read
// shows (the current week is shown besides).
const (
	LeakValueWeeksDefault = 8
	LeakValueWeeksMax     = 12
)

// LeakValueUnattributed is the place of a leak no task can be named for.
const LeakValueUnattributed = "unattributed"

// The mark: a place with leakValueMarkLeaks or more leaks over at least
// leakValueMarkWeeks of the last leakValueMarkWindow full weeks.
const (
	leakValueMarkLeaks  = 2
	leakValueMarkWeeks  = 2
	leakValueMarkWindow = 4
	leakValueWeek       = 7 * 24 * time.Hour
)

// LeakValueDoc is the projection.
type LeakValueDoc struct {
	AsOf  string         `json:"as_of"`
	Weeks int            `json:"weeks"`
	Rows  []LeakValueRow `json:"rows"`
}

// LeakValueRow is one place: its leaks per week, oldest first, and its mark
// (nil for none). UnknownReasons says, for the cells' unknown counts, what
// the records cannot say. Leaks are the place's leaks of the mark's window,
// oldest first, for the goal that adds a check back.
type LeakValueRow struct {
	Place          string          `json:"place"`
	Label          string          `json:"label"`
	Cells          []LeakValueCell `json:"cells"`
	Mark           *LeakValueMark  `json:"mark"`
	UnknownReasons []string        `json:"unknown_reasons,omitempty"`
	Leaks          []LeakValueLeak `json:"-"`
}

// LeakValueCell is one ISO week (2026-W45) of a place.
type LeakValueCell struct {
	Week    string `json:"week"`
	STG     int    `json:"stg"`
	PROD    int    `json:"prod"`
	Unknown int    `json:"unknown"`
}

// LeakValueMark marks a place for a check to be added back since the first
// full week of the window it leaked in; Mission is the goal the controller
// proposed for it, "" before it has.
type LeakValueMark struct {
	Kind    string `json:"kind"`
	Since   string `json:"since"`
	Mission string `json:"mission,omitempty"`
}

// LeakValueLeak is one leak and the records it is read from.
type LeakValueLeak struct {
	Env    string // "stg" or "prod"
	Week   string
	At     time.Time
	Record string
}

// leakValueRows is what the fold reads: the STG release records, the
// faults, and the tasks they name with their places.
type leakValueRows struct {
	stg    []leakValueSTGRecord
	faults []leakValueFault
	tasks  map[string]leakValueTask
}

type leakValueSTGRecord struct {
	seq int64
	at  time.Time
	rec StgRecord
}

// leakValueFault is an issue classified as a fault by check at at; task is
// the task of its work, "" for none (mission the work's mission, "" for no
// work).
type leakValueFault struct {
	issue   string
	check   int64
	at      time.Time
	mission string
	task    string
}

// leakValueTask is a task's place: the module above its plan node (row, an
// arch row or else an item, with its label and title), or off plan in repo.
type leakValueTask struct {
	offPlan                 bool
	repo                    string
	row, rowLabel, rowTitle string
}

// leakValueWeekStart is the Monday 00:00 UTC that starts t's ISO week.
func leakValueWeekStart(t time.Time) time.Time {
	t = t.UTC()
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
}

// LeakValueWeek names t's ISO week, as 2026-W45.
func LeakValueWeek(t time.Time) string {
	y, w := t.UTC().ISOWeek()
	return fmt.Sprintf("%04d-W%02d", y, w)
}

// LeakValuePlaceDigest is a place as a goal's idempotency key names it.
func LeakValuePlaceDigest(place string) string {
	sum := sha256.Sum256([]byte(place))
	return hex.EncodeToString(sum[:])[:16]
}

// leakValuePlace is a task's place and its label: its module, labelled as
// the plan view labels a module (its label, then its title); off plan, its
// repository; a task the records do not name, or one on the plan under no
// module, unattributed.
func leakValuePlace(tasks map[string]leakValueTask, task string) (place, label, unknown string) {
	t, ok := tasks[task]
	switch {
	case task == "" || !ok:
		return LeakValueUnattributed, LeakValueUnattributed, ""
	case t.offPlan:
		repo := t.repo
		if repo == "" {
			repo = "no repo"
		}
		p := repo + ", off plan"
		return p, p, ""
	case t.row == "":
		return LeakValueUnattributed, LeakValueUnattributed, "task " + task + " is on the plan under no arch row or item"
	}
	label = t.rowLabel
	if t.rowTitle != "" && t.rowTitle != t.rowLabel {
		label = strings.TrimSpace(label + " " + t.rowTitle)
	}
	if label == "" {
		label = t.row
	}
	return t.row, label, ""
}

// leakValueFold folds rows into the projection at now over weeks full weeks
// and the current one. Leaks before the window are not shown; the mark
// reads the last leakValueMarkWindow full weeks whatever weeks is.
func leakValueFold(rows leakValueRows, now time.Time, weeks int) LeakValueDoc {
	current := leakValueWeekStart(now)
	first := current.AddDate(0, 0, -7*weeks)
	markFrom := current.AddDate(0, 0, -7*leakValueMarkWindow)
	labels := make([]string, 0, weeks+1)
	for w := first; !w.After(current); w = w.AddDate(0, 0, 7) {
		labels = append(labels, LeakValueWeek(w))
	}
	type place struct {
		row       *LeakValueRow
		markWeeks map[string]int // leaks per full week of the mark's window
		shown     bool
	}
	places := map[string]*place{}
	get := func(id, label string) *place {
		p := places[id]
		if p == nil {
			cells := make([]LeakValueCell, len(labels))
			for i, l := range labels {
				cells[i].Week = l
			}
			p = &place{row: &LeakValueRow{Place: id, Label: label, Cells: cells}, markWeeks: map[string]int{}}
			places[id] = p
		}
		return p
	}
	cell := func(p *place, at time.Time) *LeakValueCell {
		if at.Before(first) || !at.Before(current.Add(leakValueWeek)) {
			return nil
		}
		p.shown = true
		return &p.row.Cells[int(leakValueWeekStart(at).Sub(first)/leakValueWeek)]
	}
	leak := func(task, env string, at time.Time, record string) {
		id, label, unknown := leakValuePlace(rows.tasks, task)
		p := get(id, label)
		if unknown != "" && !slices.Contains(p.row.UnknownReasons, unknown) {
			p.row.UnknownReasons = append(p.row.UnknownReasons, unknown)
		}
		if c := cell(p, at); c != nil {
			if env == "stg" {
				c.STG++
			} else {
				c.PROD++
			}
		}
		if !at.Before(markFrom) && at.Before(current) {
			p.markWeeks[LeakValueWeek(at)]++
			p.row.Leaks = append(p.row.Leaks, LeakValueLeak{Env: env, Week: LeakValueWeek(at), At: at, Record: record})
		}
	}

	// STG: each batch's members, each try's set, its red outcomes and its
	// unknown verifications, in stream order.
	type try struct {
		batch    string
		no       int64
		set      []string
		red      *leakValueSTGRecord
		redWhat  string
		unknown  *leakValueSTGRecord
		verified bool
	}
	members := map[string][]string{}
	tries := map[string]*try{}
	var order []string
	stg := slices.Clone(rows.stg)
	slices.SortFunc(stg, func(a, b leakValueSTGRecord) int { return int(a.seq - b.seq) })
	for i := range stg {
		r := &stg[i]
		batch := r.rec.String("batch")
		if batch == "" {
			continue
		}
		kind := r.rec.String("kind")
		if kind == "opened" {
			members[batch] = leakValueRoots(r.rec, "members")
			continue
		}
		if kind != "snapshot" && kind != "verify" && kind != "quarantined" && kind != "diag" {
			continue
		}
		key := fmt.Sprintf("%s#%d", batch, r.rec.Int("try"))
		t := tries[key]
		if t == nil {
			t = &try{batch: batch, no: r.rec.Int("try")}
			tries[key] = t
			order = append(order, key)
		}
		switch kind {
		case "snapshot":
			t.set = leakValueRoots(r.rec, "set")
		case "verify":
			if r.rec.String("outcome") == "failed" {
				if t.red == nil {
					t.red, t.redWhat = r, "verify failed"
				}
			} else {
				t.verified = true
			}
		case "quarantined":
			if t.red == nil {
				t.red, t.redWhat = r, "quarantined ("+r.rec.String("why")+")"
			}
		case "diag":
			if r.rec.String("what") == "verify_unknown" && t.unknown == nil {
				t.unknown = r
			}
		}
	}
	for _, key := range order {
		t := tries[key]
		roots := t.set
		if len(roots) == 0 {
			roots = members[t.batch]
		}
		roots = slices.Compact(slices.Sorted(slices.Values(roots)))
		switch {
		case t.red != nil:
			for _, root := range roots {
				leak(root, "stg", t.red.at, fmt.Sprintf("STG release %s try %d %s (%s seq %d, %s), member %s",
					t.batch, t.no, t.redWhat, StgReleaseStream, t.red.seq, t.red.at.UTC().Format(time.RFC3339), root))
			}
		case t.unknown != nil && !t.verified:
			for _, root := range roots {
				id, label, _ := leakValuePlace(rows.tasks, root)
				p := get(id, label)
				reason := fmt.Sprintf("STG release %s try %d: the records do not say its verify outcome (%s seq %d)", t.batch, t.no, StgReleaseStream, t.unknown.seq)
				if c := cell(p, t.unknown.at); c != nil {
					c.Unknown++
					if !slices.Contains(p.row.UnknownReasons, reason) {
						p.row.UnknownReasons = append(p.row.UnknownReasons, reason)
					}
				}
			}
		}
	}

	// PROD: each fault, to its work's task.
	for _, f := range rows.faults {
		record := fmt.Sprintf("issue %s classified fault (check %d, %s)", f.issue, f.check, f.at.UTC().Format(time.RFC3339))
		switch {
		case f.mission == "":
			record += ", no work taken"
		case f.task == "":
			record += ", work " + f.mission + " with no task"
		default:
			record += ", work " + f.mission + " task " + f.task
		}
		leak(f.task, "prod", f.at, record)
	}

	doc := LeakValueDoc{AsOf: now.UTC().Format(time.RFC3339Nano), Weeks: weeks, Rows: []LeakValueRow{}}
	for _, p := range places {
		total, inWeeks, since := 0, 0, ""
		for w := markFrom; w.Before(current); w = w.AddDate(0, 0, 7) {
			if n := p.markWeeks[LeakValueWeek(w)]; n > 0 {
				total += n
				inWeeks++
				if since == "" {
					since = LeakValueWeek(w)
				}
			}
		}
		if p.row.Place != LeakValueUnattributed && total >= leakValueMarkLeaks && inWeeks >= leakValueMarkWeeks {
			p.row.Mark = &LeakValueMark{Kind: "add_back", Since: since}
		}
		if p.shown || p.row.Mark != nil {
			slices.SortStableFunc(p.row.Leaks, func(a, b LeakValueLeak) int { return a.At.Compare(b.At) })
			doc.Rows = append(doc.Rows, *p.row)
		}
	}
	// The places that leaked most first, unattributed last.
	sum := func(r LeakValueRow) int {
		n := 0
		for _, c := range r.Cells {
			n += c.STG + c.PROD
		}
		return n
	}
	slices.SortFunc(doc.Rows, func(a, b LeakValueRow) int {
		switch {
		case (a.Place == LeakValueUnattributed) != (b.Place == LeakValueUnattributed):
			if a.Place == LeakValueUnattributed {
				return 1
			}
			return -1
		case sum(a) != sum(b):
			return sum(b) - sum(a)
		}
		return strings.Compare(a.Place, b.Place)
	})
	return doc
}

// leakValueRoots is the roots of the members a record lists under k.
func leakValueRoots(r StgRecord, k string) []string {
	var out []string
	for _, m := range r.members(k) {
		out = append(out, m.Root)
	}
	return out
}

// LeakValue computes the projection over weeks full weeks and the current
// one, at at (zero: the database's clock), with each mark's mission: the
// goal of the place's latest leak_value note.
func LeakValue(ctx context.Context, db DB, at time.Time, weeks int) (LeakValueDoc, error) {
	if weeks < 1 || weeks > LeakValueWeeksMax {
		return LeakValueDoc{}, invalid("weeks is from 1 to %d", LeakValueWeeksMax)
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return LeakValueDoc{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT COALESCE($1, clock_timestamp())`, nullTime(at)).Scan(&now); err != nil {
		return LeakValueDoc{}, err
	}
	from := leakValueWeekStart(now).AddDate(0, 0, -7*max(weeks, leakValueMarkWindow))
	rows, err := leakValueRead(ctx, tx, from)
	if err != nil {
		return LeakValueDoc{}, err
	}
	doc := leakValueFold(rows, now, weeks)
	missions, err := leakValueMissions(ctx, tx)
	if err != nil {
		return LeakValueDoc{}, err
	}
	for i := range doc.Rows {
		if m := doc.Rows[i].Mark; m != nil {
			m.Mission = missions[doc.Rows[i].Place]
		}
	}
	return doc, nil
}

// leakValueRead reads the STG release records that can name a leak from
// from on (the batches' members whenever they were opened), the faults
// classified from from on, and the tasks they name.
func leakValueRead(ctx context.Context, tx pgx.Tx, from time.Time) (leakValueRows, error) {
	out := leakValueRows{tasks: map[string]leakValueTask{}}
	rows, err := tx.Query(ctx, `SELECT seq, at, body_json->'stg_release' FROM agentrt.event
	  WHERE event_stream_id = $1 AND event_type = 'note' AND body_json ? 'stg_release'
	    AND (body_json->'stg_release'->>'kind' IN ('opened','snapshot')
	      OR (body_json->'stg_release'->>'kind' IN ('verify','quarantined','diag') AND at >= $2))
	  ORDER BY seq`, StgReleaseStream, from)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var r leakValueSTGRecord
		var raw []byte
		if err = rows.Scan(&r.seq, &r.at, &raw); err != nil {
			rows.Close()
			return out, err
		}
		// A record the protocol refuses names nothing.
		v, perr := canon.Parse(raw)
		if perr != nil {
			continue
		}
		if r.rec, perr = ParseStgRelease(v); perr != nil {
			continue
		}
		out.stg = append(out.stg, r)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	// The latest classification of each issue, when it is a fault, and the
	// first working task of its latest work's mission.
	rows, err = tx.Query(ctx, `SELECT c.issue_id, c.issue_check_id, c.created_at, COALESCE(w.mission_id, ''), COALESCE(t.task_id, '')
	  FROM (SELECT DISTINCT ON (issue_id) issue_id, issue_check_id, created_at, category FROM agentrt.issue_check
	    WHERE check_kind = 'classification' ORDER BY issue_id, issue_check_id DESC) c
	  LEFT JOIN LATERAL (SELECT w.mission_id FROM agentrt.issue_work w WHERE w.issue_id = c.issue_id
	    ORDER BY w.commitment DESC, w.issue_work_id DESC LIMIT 1) w ON true
	  LEFT JOIN LATERAL (SELECT t.task_id FROM agentrt.task t WHERE t.mission_id = w.mission_id
	    ORDER BY t.role IN ('verify','review','plan','brief'), t.created_at, t.task_id LIMIT 1) t ON true
	  WHERE c.category = 'fault' AND c.created_at >= $1
	  ORDER BY c.created_at, c.issue_id`, from)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var f leakValueFault
		if err = rows.Scan(&f.issue, &f.check, &f.at, &f.mission, &f.task); err != nil {
			rows.Close()
			return out, err
		}
		out.faults = append(out.faults, f)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	var ids []string
	for _, r := range out.stg {
		for _, k := range []string{"members", "set"} {
			ids = append(ids, leakValueRoots(r.rec, k)...)
		}
	}
	for _, f := range out.faults {
		if f.task != "" {
			ids = append(ids, f.task)
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	ids = slices.Compact(slices.Sorted(slices.Values(ids)))
	// Each task's module: the nearest arch_row or item above its plan node.
	rows, err = tx.Query(ctx, `WITH RECURSIVE up AS (
	    SELECT t.task_id, n.plan_node_id, n.parent_plan_node_id, n.node_kind, n.label, n.title, 0 AS depth
	      FROM agentrt.task t JOIN agentrt.plan_node n ON n.plan_node_id = t.plan_node_id WHERE t.task_id = ANY($1)
	    UNION ALL
	    SELECT up.task_id, n.plan_node_id, n.parent_plan_node_id, n.node_kind, n.label, n.title, up.depth + 1
	      FROM up JOIN agentrt.plan_node n ON n.plan_node_id = up.parent_plan_node_id
	      WHERE up.node_kind NOT IN ('arch_row','item') AND up.depth < 32)
	  SELECT t.task_id, t.off_plan, COALESCE(t.repo, ''), COALESCE(r.plan_node_id, ''), COALESCE(r.label, ''), COALESCE(r.title, '')
	    FROM agentrt.task t
	    LEFT JOIN LATERAL (SELECT up.plan_node_id, up.label, up.title FROM up WHERE up.task_id = t.task_id AND up.node_kind IN ('arch_row','item')
	      ORDER BY up.depth LIMIT 1) r ON true
	   WHERE t.task_id = ANY($1)`, ids)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var t leakValueTask
		if err = rows.Scan(&id, &t.offPlan, &t.repo, &t.row, &t.rowLabel, &t.rowTitle); err != nil {
			return out, err
		}
		out.tasks[id] = t
	}
	return out, rows.Err()
}

// leakValueMissions is, by place, the mission of its latest leak_value note.
func leakValueMissions(ctx context.Context, tx pgx.Tx) (map[string]string, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (body_json->'leak_value'->>'place') body_json->'leak_value'->>'place',
	    COALESCE(body_json->'leak_value'->>'mission', '')
	  FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note' AND actor_id = 'controller' AND body_json ? 'leak_value'
	  ORDER BY body_json->'leak_value'->>'place', seq DESC`, ControllerStream)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var place, mission string
		if err = rows.Scan(&place, &mission); err != nil {
			return nil, err
		}
		out[place] = mission
	}
	return out, rows.Err()
}

// LeakValueNote is a leak_value note's body: the goal mission proposed for
// place in week.
type LeakValueNote struct {
	Place, Week, Mission string
}

// NoteLeakValue records text and n on the controller's stream.
func NoteLeakValue(ctx context.Context, db DB, text string, n LeakValueNote) error {
	return InTx(ctx, db, func(tx pgx.Tx) error {
		_, err := Append(ctx, tx, Event{Stream: ControllerStream, Type: "note", Actor: "controller",
			Body: map[string]any{"text": text, "leak_value": map[string]any{"place": n.Place, "week": n.Week, "mission": n.Mission}}})
		return err
	})
}

// LeakValueNoted says whether place has a leak_value note for week, and
// the mission of the latest note for place that is still open (not done,
// stopped or expired), "" for none.
func LeakValueNoted(ctx context.Context, db DB, place, week string) (noted bool, open string, err error) {
	err = InTx(ctx, db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'
		  AND actor_id = 'controller' AND body_json->'leak_value'->>'place' = $2 AND body_json->'leak_value'->>'week' = $3)`,
			ControllerStream, place, week).Scan(&noted); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `SELECT m.mission_id FROM agentrt.event e
		  JOIN agentrt.mission m ON m.mission_id = e.body_json->'leak_value'->>'mission'
		  WHERE e.event_stream_id = $1 AND e.event_type = 'note' AND e.actor_id = 'controller'
		    AND e.body_json->'leak_value'->>'place' = $2 AND m.state NOT IN ('done', 'stopped', 'expired')
		  ORDER BY e.seq DESC LIMIT 1`, ControllerStream, place).Scan(&open)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	})
	return noted, open, err
}
