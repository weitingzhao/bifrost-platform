package store

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
)

// What keeps a step that is in progress from done (R5:5.5-19,
// DESIGN-r5-5.5-19-waiting-chain §4). Each blocker says only what a record
// is and which ids lead to it; never whether anyone is moving it now or
// whose turn it is: the Console judges that from its fresh snapshot by id.
// A blocker is a closed object per state (§7), so it is built as a map with
// exactly the contract's keys.
type Blocker = map[string]any

// Bounds on one step's explanation (§5).
const (
	explainCandidates = 10 // task candidates per tasks_short
	explainCards      = 3  // cards per group
	explainBlocks     = 5  // STG blocks per member
	explainItems      = 50 // all items of one step
)

// explainAtom is the blockers of one atom that is not true.
func (f *stepFacts) explainAtom(n *plan.Node) []Blocker {
	switch n.Kind {
	case plan.AtomStepTasks:
		return f.explainTasks(n.State, n.Min)
	case plan.AtomStepLanded:
		return f.explainLanded()
	}
	return f.explainSTG()
}

// explainTasks: the tasks step_tasks counts, by the same filter
// (stepevidence.go StepTasks), and those of them not in state yet.
func (f *stepFacts) explainTasks(state string, min int64) []Blocker {
	if f.err != nil {
		return nil
	}
	var all, have int64
	if f.err = f.tx.QueryRow(f.ctx, `SELECT count(*), count(*) FILTER (WHERE state = $2) FROM agentrt.task
	  WHERE plan_node_id = $1 AND subject_task_id IS NULL`, f.step, state).Scan(&all, &have); f.err != nil {
		return nil
	}
	switch {
	case all == 0:
		return []Blocker{{"atom": plan.AtomStepTasks, "state": "no_task"}}
	case have >= min:
		return nil
	}
	rows, err := f.tx.Query(f.ctx, `SELECT task_id, state FROM agentrt.task WHERE plan_node_id = $1 AND subject_task_id IS NULL AND state <> $2
	  ORDER BY task_id LIMIT $3`, f.step, state, explainCandidates)
	if err != nil {
		f.err = err
		return nil
	}
	cands, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (map[string]any, error) {
		var id, st string
		err := row.Scan(&id, &st)
		return map[string]any{"task_id": id, "task_state": st}, err
	})
	if err != nil {
		f.err = err
		return nil
	}
	if cands == nil {
		cands = []map[string]any{} // an array on the wire, never null
	}
	b := Blocker{"atom": plan.AtomStepTasks, "state": "tasks_short", "have": have, "need": min, "candidates": cands}
	if rest := all - have - int64(len(cands)); rest > 0 {
		b["omitted"] = rest
	}
	return []Blocker{b}
}

// explainLanded: each implementation step_landed still waits on, by the
// final status landing() gave it (§4.2): the same reading as the value.
func (f *stepFacts) explainLanded() []Blocker {
	l := f.landing()
	if l == nil || f.err != nil {
		return nil
	}
	if len(l.roots) == 0 {
		return []Blocker{{"atom": plan.AtomStepLanded, "state": "no_implementation"}}
	}
	var out []Blocker
	for _, r := range l.roots {
		if f.err = f.ctx.Err(); f.err != nil {
			return nil
		}
		if b := f.explainRoot(r); b != nil {
			out = append(out, b)
		}
		if f.err != nil {
			return nil
		}
	}
	return out
}

// explainRoot is one root's blocker, nil for one that landed or was
// delivered by another.
func (f *stepFacts) explainRoot(r implRoot) Blocker {
	at := func(t time.Time) any {
		if t.IsZero() {
			return nil
		}
		return t.UTC().Format(time.RFC3339Nano)
	}
	base := func(state string) Blocker {
		return Blocker{"atom": plan.AtomStepLanded, "state": state, "task_id": r.task}
	}
	switch r.status {
	case rootLanded, rootDelivered:
		return nil
	case rootNone:
		return base("no_landing_evidence")
	case rootPending, rootUnknown:
		if !r.has {
			b := base("landing_unclaimed")
			b["accepted_at"] = nil
			if r.accepted != nil {
				b["accepted_at"] = at(*r.accepted)
			}
			return b
		}
		state, phase := "landing_pending", r.o.Phase
		if r.status == rootUnknown {
			state = "landing_main_unsettled"
		} else if r.o.Terminal() {
			phase = "replay_due" // ended without ending the root: its next operation is due
		}
		b := base(state)
		b["op"], b["phase"], b["last_record_at"] = r.op, phase, at(r.o.LastAt)
		b["grant"] = nil
		if r.o.Phase == "awaiting_grant" {
			b["grant"] = f.explainGrant(r.repo, r.op)
		}
		f.addRootCards(b, r)
		return b
	case rootFailed:
		if r.o.Phase == "failed" && r.o.Outcome == "declined" {
			b := base("landing_declined")
			b["op"], b["rework"] = r.op, f.explainRework(r)
			return b
		}
		b := base("landing_failed")
		// failed{why} names its reason; any other end (reverted after it
		// landed, discarded) is the result itself, and its carried outcome
		// (a reverted landing's main_result{ok}) is not.
		outcome := r.o.Outcome
		if r.o.Phase != "failed" || outcome == "" {
			outcome = r.o.Phase
		}
		b["op"], b["outcome"] = r.op, outcome
		f.addRootCards(b, r)
		return b
	}
	b := base("landing_unmapped")
	b["op"], b["phase"], b["outcome"] = r.op, r.o.Phase, r.o.Outcome
	return b
}

// explainGrant is the last landing_grant_request recorded for op (read
// for every root explained at once, preload): an id to open, never whether
// it waits on anyone (Needs you judges that).
func (f *stepFacts) explainGrant(repo, op string) any {
	ex, err := f.explainRepo(repo)
	if err != nil {
		f.err = err
		return nil
	}
	if g, ok := ex.grants[op]; ok {
		return g
	}
	return nil
}

// addRootCards puts on b the cards of r's current root triggers: same
// repository, root, operation, kind and trigger_seq (§4.2), newest first,
// at most explainCards, with cards_omitted for the rest.
func (f *stepFacts) addRootCards(b Blocker, r implRoot) {
	ex, err := f.explainRepo(r.repo)
	if err != nil {
		f.err = err
		return
	}
	var mine []CardRecord
	for _, tr := range ex.triggers {
		if f.err = f.ctx.Err(); f.err != nil {
			return
		}
		if tr.Family == "stg" || tr.Root != r.task || tr.Op != r.op {
			continue
		}
		mine = append(mine, ex.byTrigger[cardIndexOf("", tr.Kind, tr.Seq, "", "", r.task, r.op)]...)
	}
	cards, omitted := cardsOut(mine)
	b["cards"] = cards
	if omitted > 0 {
		b["cards_omitted"] = omitted
	}
}

// cardsOut is a group of cards as the contract's Card, newest first (by
// trigger, then n), at most explainCards, and how many were left out.
func cardsOut(cs []CardRecord) ([]map[string]any, int) {
	slices.SortFunc(cs, func(a, b CardRecord) int {
		if a.TriggerSeq != b.TriggerSeq {
			return int(b.TriggerSeq - a.TriggerSeq)
		}
		return b.N - a.N
	})
	out := []map[string]any{}
	for i, c := range cs {
		if i == explainCards {
			return out, len(cs) - i
		}
		out = append(out, cardOut(c))
	}
	return out, 0
}

// cardOut is one card as the contract's Card: its durable fields as they
// are, an entry only.
func cardOut(c CardRecord) map[string]any {
	m := map[string]any{"question_id": nil, "prep": c.Prep, "kind": c.Kind, "family": c.Family,
		"ask_started": c.AskStarted, "closed": c.Closed, "closed_why": nil}
	if m["family"] == "" {
		m["family"] = "landing"
	}
	if c.QuestionID != "" {
		m["question_id"] = c.QuestionID
	}
	if c.Closed && c.ClosedWhy != "" {
		m["closed_why"] = c.ClosedWhy
	}
	return m
}

// explainRework is a declined root's rework records (§4.3): the
// landing_rework_seen for the step's landed siblings now (current), else
// the latest for its operation (not current); whether the current set is
// too large to ask; and the latest card for the current set's trigger with
// how it was handled. Always an object: nothing recorded is nulls. The
// records come from preload's one read for every root explained.
func (f *stepFacts) explainRework(r implRoot) map[string]any {
	out := map[string]any{"seen": nil, "not_asked": false, "card": nil}
	ex, err := f.explainRepo(r.repo)
	if err != nil {
		f.err = err
		return out
	}
	rows := ex.seen[[2]string{r.task, r.op}] // in stream order
	// The landed siblings landing() read for this root, in this same
	// transaction: a declined root that stays a no always has that read.
	l := f.landing()
	if l == nil || f.err != nil {
		return out
	}
	sibs, read := l.siblings[r.task]
	if !read {
		var err error
		if sibs, err = landedSiblings(f.ctx, f.tx, ex.fold, r.repo, f.step, r.task); err != nil {
			f.err = err
			return out
		}
	}
	var seq int64
	if len(sibs) > 0 {
		key := ReworkSetKey(sibs)
		for _, x := range rows { // the first for the set, as reworkSeenFor reads it
			if f.err = f.ctx.Err(); f.err != nil {
				return out
			}
			if x.setKey == key {
				seq = x.seq
				break
			}
		}
		if seq > 0 {
			out["seen"] = map[string]any{"set_key": key, "trigger_seq": seq, "current": true}
			// The card engine's own judgment, already made for this root when
			// the repository's triggers were read (explainRepo): no
			// rework_landed trigger for this set means it is not asked.
			asked := false
			for _, tr := range ex.triggers {
				if f.err = f.ctx.Err(); f.err != nil {
					return out
				}
				if tr.Kind == CardReworkLanded && tr.Root == r.task && tr.Op == r.op && tr.Seq == seq {
					asked = true
					break
				}
			}
			out["not_asked"] = !asked
		}
	}
	if seq == 0 {
		if n := len(rows); n > 0 {
			out["seen"] = map[string]any{"set_key": rows[n-1].setKey, "trigger_seq": rows[n-1].seq, "current": false}
		}
		return out
	}
	var card *CardRecord
	for _, c := range ex.byTrigger[cardIndexOf("", CardReworkLanded, seq, "", "", r.task, r.op)] {
		if f.err = f.ctx.Err(); f.err != nil {
			return out
		}
		if card == nil || c.N > card.N {
			c := c
			card = &c
		}
	}
	if card == nil {
		return out
	}
	cm := cardOut(*card)
	delete(cm, "kind")
	delete(cm, "family")
	cm["n"], cm["outcome"] = card.N, nil
	if o, ok := ex.outcomes[card.QuestionID]; ok && card.QuestionID != "" {
		cm["outcome"] = o
	}
	out["card"] = cm
	return out
}

// repoExplain is what one explanation reads once per repository: its
// landing fold, its card stream folded and its card triggers now.
type repoExplain struct {
	fold     landing.State
	cards    []CardRecord
	triggers []CardTrigger
	// B5's fold and its card triggers, for the repository B5 releases.
	stg         *StgState
	stgTriggers []CardTrigger
	// Indexes built once (preload): cards by their trigger's identity; each
	// operation's last grant request; each (root, operation)'s rework
	// records in stream order; the last outcome of each rework card.
	byTrigger map[cardIndex][]CardRecord
	// ctx is the explanation's: the loops over cards, blocks and triggers
	// stop with it.
	ctx      context.Context
	grants   map[string]map[string]any
	seen     map[[2]string][]reworkSeenRow
	outcomes map[string]map[string]any
}

// cardIndex is a card's trigger identity: what matchingStgTrigger and the
// root card triggers match on.
type cardIndex struct {
	family, kind string
	seq          int64
	block, batch string
	root, op     string
}

// cardIndexOf builds the index key; family "" stands for a root card.
func cardIndexOf(family, kind string, seq int64, block, batch, root, op string) cardIndex {
	if family != "stg" {
		family = ""
	}
	return cardIndex{family, kind, seq, block, batch, root, op}
}

type reworkSeenRow struct {
	seq    int64
	setKey string
}

// stopped says the explanation's context ended; a loop that sees it stops
// at once and the caller's own check reports it (the whole result is
// dropped: ExplainRootWithin).
func (ex *repoExplain) stopped() bool { return ex.ctx != nil && ex.ctx.Err() != nil }

// indexCards indexes the repository's cards by their trigger's identity.
func (ex *repoExplain) indexCards() {
	ex.byTrigger = map[cardIndex][]CardRecord{}
	for _, c := range ex.cards {
		if ex.stopped() {
			return
		}
		k := cardIndexOf(c.Family, c.Kind, c.TriggerSeq, c.Block, c.Batch, c.Root, c.Op)
		if c.Family == "stg" {
			k.root, k.op = "", "" // an STG card names neither
		} else {
			k.block, k.batch = "", ""
		}
		ex.byTrigger[k] = append(ex.byTrigger[k], c)
	}
}

// preload reads, in one query each, what the explanation shows for the
// roots it explains in repo (§5: batched, indexed, shared by every step of
// this explanation): each latest operation's last grant request, each
// root's rework records, and the outcomes of their rework cards.
func (ex *repoExplain) preload(f *stepFacts, repo string) error {
	ex.indexCards()
	ex.grants, ex.seen, ex.outcomes = map[string]map[string]any{}, map[[2]string][]reworkSeenRow{}, map[string]map[string]any{}
	var roots, ops []string
	for root := range f.explainRoots[repo] {
		roots = append(roots, root)
		if op := ex.fold.LatestOperation(root); op != "" {
			ops = append(ops, op)
		}
	}
	if len(roots) == 0 {
		return nil
	}
	stream := "landing:" + repo
	rows, err := f.tx.Query(f.ctx, `SELECT DISTINCT ON (body_json->'landing_grant_request'->>'op') body_json->'landing_grant_request', at
	  FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note' AND body_json->'landing_grant_request'->>'op' = ANY($2)
	  ORDER BY body_json->'landing_grant_request'->>'op', seq DESC`, stream, ops)
	if err != nil {
		return err
	}
	for rows.Next() {
		var m map[string]any
		var at time.Time
		if err := rows.Scan(&m, &at); err != nil {
			rows.Close()
			return err
		}
		n, _ := cardNumber(m["approval_number"])
		op, _ := m["op"].(string)
		ex.grants[op] = map[string]any{"approval_id": m["approval_id"], "number": n, "last_status": m["status"], "recorded_at": at.UTC().Format(time.RFC3339Nano)}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	rows, err = f.tx.Query(f.ctx, `SELECT task_id, seq, body_json->'landing_rework_seen'->>'op', body_json->'landing_rework_seen'->>'set_key'
	  FROM agentrt.event WHERE event_stream_id = $1 AND task_id = ANY($2) AND event_type = 'note' AND body_json ? 'landing_rework_seen' ORDER BY seq`, stream, roots)
	if err != nil {
		return err
	}
	for rows.Next() {
		var root, op, key string
		var seq int64
		if err := rows.Scan(&root, &seq, &op, &key); err != nil {
			rows.Close()
			return err
		}
		ex.seen[[2]string{root, op}] = append(ex.seen[[2]string{root, op}], reworkSeenRow{seq, key})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	var questions []string
	for _, c := range ex.cards {
		if c.Kind == CardReworkLanded && c.QuestionID != "" && f.explainRoots[repo][c.Root] {
			questions = append(questions, c.QuestionID)
		}
	}
	if len(questions) == 0 {
		return nil
	}
	rows, err = f.tx.Query(f.ctx, `SELECT DISTINCT ON (body_json->'landing_card_outcome'->>'question_id') body_json->'landing_card_outcome', seq
	  FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note' AND body_json->'landing_card_outcome'->>'question_id' = ANY($2)
	  ORDER BY body_json->'landing_card_outcome'->>'question_id', seq DESC`, stream, questions)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var m map[string]any
		var seq int64
		if err := rows.Scan(&m, &seq); err != nil {
			return err
		}
		q, _ := m["question_id"].(string)
		ex.outcomes[q] = map[string]any{"outcome": m["outcome"], "why": m["why"], "seq": seq}
	}
	return rows.Err()
}

// explainRepo reads repo's records once for this stepFacts.
func (f *stepFacts) explainRepo(repo string) (*repoExplain, error) {
	if f.repos == nil {
		f.repos = map[string]*repoExplain{}
	}
	if ex, ok := f.repos[repo]; ok {
		return ex, nil
	}
	fold, err := LandingState(f.ctx, f.tx, "landing:"+repo)
	if err != nil {
		return nil, err
	}
	byPrep, err := LandingCards(f.ctx, f.tx, CardsStream(repo))
	if err != nil {
		return nil, err
	}
	cards := make([]CardRecord, 0, len(byPrep))
	for _, c := range byPrep {
		cards = append(cards, c)
	}
	slices.SortFunc(cards, func(a, b CardRecord) int {
		if a.Prep < b.Prep {
			return -1
		}
		if a.Prep > b.Prep {
			return 1
		}
		return 0
	})
	ex := &repoExplain{fold: fold, cards: cards, ctx: f.ctx}
	if "landing:"+repo == StgReleaseStream {
		if err := ex.readSTG(f); err != nil {
			return nil, err
		}
	}
	only := f.explainRoots[repo]
	if only == nil {
		only = map[string]bool{} // no root of this repository explained: none is read
	}
	if ex.triggers, err = landingCardTriggers(f.ctx, f.tx, repo, f.explainAt, fold, ex.stg, only); err != nil {
		return nil, err
	}
	if err := ex.preload(f, repo); err != nil {
		return nil, err
	}
	f.repos[repo] = ex
	return ex, nil
}

// explainSTG: each landed operation step_stg_settled still waits on, in
// stgSettled's own order (§4.4): withheld, then admitted to a batch not
// closed, then delivered or covered (no blocker), then the final no.
func (f *stepFacts) explainSTG() []Blocker {
	l := f.landing()
	if l == nil || f.err != nil {
		return nil
	}
	if l.value != plan.True {
		return []Blocker{{"atom": plan.AtomStepSTG, "state": "stg_needs_landing"}}
	}
	var out []Blocker
	unsupported := map[string]bool{}
	for _, r := range l.landed {
		if f.err = f.ctx.Err(); f.err != nil {
			return nil
		}
		if "landing:"+r.repo != StgReleaseStream {
			if !noSTGRelease[r.repo] && !unsupported[r.repo] {
				unsupported[r.repo] = true
				out = append(out, Blocker{"atom": plan.AtomStepSTG, "state": "stg_not_supported", "repo": r.repo})
			}
			continue
		}
		ex, err := f.explainRepo(stgReleaseRepo)
		if err == nil && ex.stg == nil {
			err = ex.readSTG(f)
		}
		if err != nil {
			f.err = err
			return nil
		}
		b := ex.stgMember(r.key)
		if f.err = f.ctx.Err(); f.err != nil {
			return nil
		}
		if b != nil {
			out = append(out, b)
		}
	}
	return out
}

// stgReleaseRepo is the repository B5 releases.
var stgReleaseRepo = strings.TrimPrefix(StgReleaseStream, "landing:")

// readSTG reads the B5 fold once for this explanation.
func (ex *repoExplain) readSTG(f *stepFacts) error {
	s, err := StgReleaseState(f.ctx, f.tx)
	if err != nil {
		return err
	}
	ex.stg, ex.stgTriggers = &s, stgTriggers(s)
	return nil
}

// stgMember is member m's blocker, nil when B5 settled it delivered.
func (ex *repoExplain) stgMember(m StgMemberKey) Blocker {
	s := *ex.stg
	base := func(state string) Blocker {
		return Blocker{"atom": plan.AtomStepSTG, "state": state, "task_id": m.Root, "op": m.Op}
	}
	withBlocks := func(b Blocker) Blocker {
		blocks, omitted := ex.stgBlocks(func(rec StgRecord) bool { return slices.Contains(blockMembers(rec), m) })
		b["blocks"] = blocks
		if omitted > 0 {
			b["blocks_omitted"] = omitted
		}
		return b
	}
	if w := s.Withheld[m]; w.Seq > 0 {
		b := base("stg_withheld")
		b["kind"] = w.Record.String("why")
		ex.putCards(b, "cards", func(tr CardTrigger) bool { return tr.Kind == CardSTGIneligible && tr.Seq == w.Seq })
		return withBlocks(b)
	}
	member, admitted := s.Members[m]
	if admitted {
		if bt := s.Batches[member.Batch]; bt == nil || bt.Phase != "CLOSED" {
			b := base("stg_batch_open")
			b["batch_id"], b["batch_state"], b["try"], b["quarantine"] = member.Batch, "missing", nil, nil
			if bt != nil {
				b["batch_state"] = bt.Phase
				if t := bt.Tries[bt.Try]; t != nil && t.Snapshot.Seq > 0 {
					b["try"] = stgTryOut(bt.Try, t)
					if t.IsQuarantined() {
						q := map[string]any{"reason": t.Quarantined.Record.String("why"), "trigger_seq": t.Quarantined.Seq}
						ex.putCards(q, "cards", func(tr CardTrigger) bool {
							return tr.Kind == CardSTGBlocked && tr.Batch == member.Batch && tr.Seq == t.Quarantined.Seq
						})
						b["quarantine"] = q
					}
				}
			}
			return withBlocks(b)
		}
	}
	if member.To == "delivered" || s.covered(m) {
		return nil
	}
	switch member.To {
	case "exhausted":
		b := base("stg_exhausted")
		ex.putCards(b, "cards", func(tr CardTrigger) bool { return tr.Kind == CardSTGFailed && slices.Contains(tr.Members, m) })
		return withBlocks(b)
	case "disposed":
		return withBlocks(base("stg_disposed"))
	}
	b := base("stg_not_admitted")
	b["to"] = nil
	if member.To != "" {
		b["to"] = member.To
	}
	b["unexplained"] = []string{"budget", "policy", "repo_cards"}
	// Any open block holds every new batch (stg_release.go, opened), named
	// member or not.
	repo, omitted := ex.stgBlocks(func(StgRecord) bool { return true })
	b["repo_blocks"] = repo
	if omitted > 0 {
		b["repo_blocks_omitted"] = omitted
	}
	return withBlocks(b)
}

// blockMembers are the members a blocked record names.
func blockMembers(r StgRecord) []StgMemberKey {
	var out []StgMemberKey
	for _, m := range r.object("detail").members("members") {
		out = append(out, m.StgMemberKey)
	}
	return out
}

// stgBlocks are the open blocking intervals keep selects, by since then id,
// at most explainBlocks, each with the cards of its own trigger.
func (ex *repoExplain) stgBlocks(keep func(StgRecord) bool) ([]map[string]any, int) {
	type open struct {
		id    string
		since time.Time
		b     StgBlock
	}
	var all []open
	for id, b := range ex.stg.Blocks {
		if ex.stopped() {
			return []map[string]any{}, 0
		}
		if !b.IsOpen() || !keep(b.Opened.Record) {
			continue
		}
		since := b.Opened.Record.Time("first_at")
		if since.IsZero() {
			since = b.Opened.At
		}
		all = append(all, open{id, since, b})
	}
	slices.SortFunc(all, func(a, b open) int {
		if c := a.since.Compare(b.since); c != 0 {
			return c
		}
		return strings.Compare(a.id, b.id)
	})
	out := []map[string]any{}
	for i, o := range all {
		if i == explainBlocks {
			return out, len(all) - i
		}
		blk := map[string]any{"id": o.id, "kind": o.b.Opened.Record.String("why"), "since": o.since.UTC().Format(time.RFC3339Nano),
			"trigger_seq": o.b.Opened.Seq}
		ex.putCards(blk, "cards", func(tr CardTrigger) bool { return tr.Block == o.id && tr.Seq == o.b.Opened.Seq && tr.Batch == "" })
		out = append(out, blk)
	}
	return out, 0
}

// putCards sets key on m to the STG cards of the triggers match selects,
// matched as matchingStgTrigger matches them: family, kind, trigger_seq,
// block and batch.
func (ex *repoExplain) putCards(m map[string]any, key string, match func(CardTrigger) bool) {
	var mine []CardRecord
	for _, tr := range ex.stgTriggers {
		if ex.stopped() {
			break
		}
		if match(tr) {
			mine = append(mine, ex.byTrigger[cardIndexOf("stg", tr.Kind, tr.Seq, tr.Block, tr.Batch, "", "")]...)
		}
	}
	cards, omitted := cardsOut(mine)
	m[key] = cards
	if omitted > 0 {
		m[key+"_omitted"] = omitted
	}
}

// stgTryOut is a batch's current try as recorded: the snapshot always,
// started, bound and the run only once written.
func stgTryOut(n int64, t *StgTry) map[string]any {
	out := map[string]any{"n": n, "snapshot_at": t.Snapshot.At.UTC().Format(time.RFC3339Nano), "started_at": nil, "bound_uid": nil, "run": nil}
	if t.Started.Seq > 0 {
		out["started_at"] = t.Started.At.UTC().Format(time.RFC3339Nano)
		if run := t.Started.Record.String("run"); run != "" {
			out["run"] = run
		}
	}
	if t.Bound.Seq > 0 {
		if uid := t.Bound.Record.String("uid"); uid != "" {
			out["bound_uid"] = uid
		}
	}
	return out
}
