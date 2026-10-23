package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// Business issues (W-31 BI-1 slice 1; bifrost-trade-infra agent-config/work/
// crew/DESIGN-crew-intake-2026-10-22.md §4; the Owner chose I-D1…I-D8 A on
// 2026-10-23): what a person or an agent brings to the crew, in the runtime's
// own tables (migrations 0009). An issue is not a mission: it is brought
// (issue_submitted), its brief is written and confirmed by the person it
// serves, it is classified and prioritized, and only take_issue makes its
// mission, in one transaction with the work it binds (issuetake.go). Its
// state only says where it is and whom it waits on; taking and closing are
// decided by the evidence of the current brief B and design D, which every
// ask, check and piece of work is bound to by sha256 (§4.3).
//
// Every text (a person's words, an agent's reading and proposal, replies,
// briefs, designs, reports, answers) is a row of issue_text, never an
// artifact, so the generic artifact and event reads never reach it (§5.3);
// an event holds ids, enums and hashes. A text's, an ask's and a check's id
// is taken from its table's sequence before the event that names it is
// written (nextIDs), as the guards want the event to name each row.
//
// Lock order (§4.3), every issue write: the writer's lock (ensureWriter),
// then the product lock (take_issue only), then the issue rows (FOR UPDATE;
// a merge takes both in ascending number), then the asks it reads or
// changes, then the mission number lock (take_issue, proposeMissionTx), then
// the event streams. A mission's terminal trigger (0009) changes only issue
// work rows, under the mission row lock moveMission holds, and takes no
// issue row.

// Values the issue tables check (0009).
var (
	issueIDRE       = regexp.MustCompile(`^BI-[1-9][0-9]*$`)
	agentIDRE       = regexp.MustCompile(`^agent:[a-z0-9-]{1,32}$`)
	personIDRE      = regexp.MustCompile(`^person:[a-z0-9-]{1,32}$`)
	productRE       = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	commitRE        = regexp.MustCompile(`^[0-9a-f]{40}$`)
	decisionRE      = regexp.MustCompile(`^D[1-9][0-9]*$`)
	letterRE        = regexp.MustCompile(`^[A-Z]$`)
	toolRE          = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	artifactIDRE    = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	hex64RE         = regexp.MustCompile(`^[0-9a-f]{64}$`)
	issueKinds      = []string{"complaint", "question", "request", "proposal"}
	textChannels    = []string{ChannelIntake, "chrome", "public", ChannelThread, ChannelOwnerRelay, ChannelConsole, ChannelCard}
	askPurposes     = []string{AskConfirmBrief, AskNeedsInfo, AskReadyToTry, AskCheckAnswer}
	askVerdicts     = []string{"confirmed", "rejected", "partly", "withdrawn", "answered"}
	riskClasses     = rules.IntakeClasses
	issueCategories = []string{"guidance", "improvement", "method_change", "data_not_shown", "fault", "out_of_scope"}
	priorities      = []string{"p1", "p2", "p3", "later", "declined"}
	issueChanges    = []string{"park", "resume", "reopen", "close", "supersede", "ask_owner"}
	issueStates     = []string{"clarifying", "brief_confirmed", "classified", "designing", "in_review", "owner_deciding",
		"active", "awaiting_user", "parked", "closed"}
	conclusions = []string{ConclusionResolved, ConclusionPartial, ConclusionAlreadyPossible, ConclusionDeclined, ConclusionMerged}
)

// The purposes of an ask (§2.1).
const (
	AskConfirmBrief = "confirm_brief"
	AskNeedsInfo    = "needs_info"
	AskReadyToTry   = "ready_to_try"
	AskCheckAnswer  = "check_answer"
)

// What each purpose's answer may say (the table's check, §2.1).
var askVerdictsOf = map[string][]string{
	AskConfirmBrief: {"confirmed", "rejected", "withdrawn"},
	AskNeedsInfo:    {"answered", "withdrawn"},
	AskReadyToTry:   {"confirmed", "partly", "rejected", "withdrawn"},
	AskCheckAnswer:  {"confirmed", "rejected", "withdrawn"},
}

// The channels a text comes by (0009 intake_voice_ok): an external agent's
// local bridge, a crew thread, a thread relaying what the Owner relayed, the
// Console, a card the Owner answered.
const (
	ChannelIntake     = "intake_local"
	ChannelThread     = "thread"
	ChannelOwnerRelay = "owner_relay"
	ChannelConsole    = "console"
	ChannelCard       = "card"
)

// The conclusions of an issue (§4.3).
const (
	ConclusionResolved        = "resolved"
	ConclusionPartial         = "partial"
	ConclusionAlreadyPossible = "already_possible"
	ConclusionDeclined        = "declined"
	ConclusionMerged          = "merged"
)

// The kinds of evidence (issue_check.check_kind).
const (
	checkClassification = "classification"
	checkPriority       = "priority"
	checkReview         = "review"
	checkOwnerDecision  = "owner_decision"
	checkClose          = "close_check"
)

// OwnerPerson is the person an agent's own proposal asks: the Owner (§4.2).
const OwnerPerson = "person:owner"

// Bounds of what an external agent submits (§2.1): a title of 80
// characters, each text of 2000. MaxIssueWords bounds how many segments of
// words one submission or addition carries (the design does not say; 20, so
// one call cannot fill a person's history in one go). A crew text (brief,
// design, review report, reply, note) is bounded by the table (1 MiB).
const (
	MaxIssueTitle     = 80
	MaxIssueTextChars = 2000
	MaxIssueWords     = 20
	maxCrewTextBytes  = 1 << 20
	maxWhereChars     = 200
)

// IssueStream is the event stream of an issue's history.
func IssueStream(id string) string { return "issue:" + id }

var (
	// ErrStale: the issue (or the brief, design or ask a write names) has
	// moved on since the caller read it; StaleError carries how it is now.
	ErrStale = errors.New("store: the issue has moved on")
	// ErrQuota: an external agent's daily limit is reached (§2.1, 429).
	ErrQuota = errors.New("store: a daily limit is reached")
	// ErrRevoked: the intake client is revoked (§5.2).
	ErrRevoked = errors.New("store: the intake client is revoked")
)

// StaleError is ErrStale with the issue as it is now.
type StaleError struct {
	Why   string
	Issue IssueView
}

func (e *StaleError) Error() string { return fmt.Sprintf("%v: %s", ErrStale, e.Why) }
func (e *StaleError) Unwrap() error { return ErrStale }

// IssueView is an issue's state as a write's answer and a stale write's
// error give it: no text.
type IssueView struct {
	ID, Product, Kind, State, WaitingOn, Conclusion, Priority, RiskClass string
	ServedPerson, AsksPerson, DuplicateOf                                string
	Version                                                              int
	BriefSHA256, DesignSHA256                                            string
	BriefConfirmed                                                       bool
}

// Doc is the view as a response has it.
func (v IssueView) Doc() map[string]any {
	d := map[string]any{"issue_id": v.ID, "product": v.Product, "kind": v.Kind, "state": v.State, "waiting_on": v.WaitingOn,
		"version": v.Version, "asks_person": v.AsksPerson, "brief_confirmed": v.BriefConfirmed}
	for k, s := range map[string]string{"conclusion": v.Conclusion, "priority": v.Priority, "risk_class": v.RiskClass,
		"served_person": v.ServedPerson, "duplicate_of": v.DuplicateOf, "brief_sha256": v.BriefSHA256, "design_sha256": v.DesignSHA256} {
		if s != "" {
			d[k] = s
		}
	}
	return d
}

// issueRow is an issue as a write reads it.
type issueRow struct {
	IssueView
	title, creator             string
	briefText, designText, ask int64
	briefSHA, designSHA        string
}

func readIssue(ctx context.Context, tx pgx.Tx, id string, lock bool) (issueRow, error) {
	q := `SELECT i.issue_id, i.product, i.kind, i.title, i.state, i.waiting_on, COALESCE(i.conclusion, ''), COALESCE(i.priority, ''),
	  COALESCE(i.risk_class, ''), COALESCE(i.served_person, ''), i.asks_person, COALESCE(i.duplicate_of, ''), i.created_by_actor_id,
	  COALESCE(i.brief_text_id, 0), COALESCE(i.design_text_id, 0), COALESCE(i.brief_confirmed_ask, 0), i.version,
	  COALESCE(b.sha256, ''), COALESCE(d.sha256, '')
	  FROM agentrt.issue i LEFT JOIN agentrt.issue_text b ON b.issue_text_id = i.brief_text_id
	  LEFT JOIN agentrt.issue_text d ON d.issue_text_id = i.design_text_id WHERE i.issue_id = $1`
	if lock {
		q += ` FOR UPDATE OF i`
	}
	var r issueRow
	err := tx.QueryRow(ctx, q, id).Scan(&r.ID, &r.Product, &r.Kind, &r.title, &r.State, &r.WaitingOn, &r.Conclusion, &r.Priority,
		&r.RiskClass, &r.ServedPerson, &r.AsksPerson, &r.DuplicateOf, &r.creator, &r.briefText, &r.designText, &r.ask, &r.Version,
		&r.briefSHA, &r.designSHA)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, invalid("there is no issue %s", id)
	}
	r.BriefSHA256, r.DesignSHA256, r.BriefConfirmed = r.briefSHA, r.designSHA, r.ask != 0
	return r, err
}

// lockIssue reads an issue for a write: locked, and at the version the
// caller read (expected); otherwise the write is stale and nothing is
// written (§2.1: 409 and the latest state).
func lockIssue(ctx context.Context, tx pgx.Tx, id string, expected int) (issueRow, error) {
	if !issueIDRE.MatchString(id) {
		return issueRow{}, invalid("%q is not an issue id (BI-<n>)", id)
	}
	r, err := readIssue(ctx, tx, id, true)
	if err != nil {
		return r, err
	}
	if r.Version != expected {
		return r, &StaleError{Why: fmt.Sprintf("%s is at version %d, not %d", id, r.Version, expected), Issue: r.IssueView}
	}
	return r, nil
}

// IssueState is an issue's view, read on its own.
func IssueState(ctx context.Context, db DB, id string) (IssueView, error) {
	var v IssueView
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		r, err := readIssue(ctx, tx, id, false)
		v = r.IssueView
		return err
	})
	return v, err
}

// open says the issue takes crew work: not closed (merged is closed) and not
// parked.
func (r issueRow) open() error {
	switch r.State {
	case "closed":
		return fmt.Errorf("%w: %s is closed (%s)", ErrState, r.ID, r.Conclusion)
	case "parked":
		return fmt.Errorf("%w: %s is parked; resume it first", ErrState, r.ID)
	}
	return nil
}

// waitsOn is whom an ask of this issue waits on: the person it serves, or,
// for an agent's own proposal, the Owner (§4.2, I-D6).
func (r issueRow) waitsOn() string {
	if r.ServedPerson == "" {
		return "owner"
	}
	return "user"
}

// liveWork is the issue's live work (ended_at NULL), if any.
func liveWork(ctx context.Context, tx pgx.Tx, issue string) (mission string, err error) {
	err = tx.QueryRow(ctx, `SELECT mission_id FROM agentrt.issue_work WHERE issue_id = $1 AND ended_at IS NULL`, issue).Scan(&mission)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return mission, err
}

// openAsk says whether the issue has an open ask of one of purposes.
func openAsk(ctx context.Context, tx pgx.Tx, issue string, purposes ...string) (bool, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM agentrt.issue_ask WHERE issue_id = $1 AND purpose = ANY($2)
	  AND verdict IS NULL AND superseded_at IS NULL`, issue, purposes).Scan(&n)
	return n > 0, err
}

// nextIDs takes n ids from an issue table's identity sequence, so the event
// written first can name the rows inserted after it (0009 event_names_row).
func nextIDs(ctx context.Context, tx pgx.Tx, table string, n int) ([]int64, error) {
	if !slices.Contains([]string{"issue_text", "issue_ask", "issue_check"}, table) {
		return nil, fmt.Errorf("store: %s takes no ids ahead", table)
	}
	rows, err := tx.Query(ctx, `SELECT nextval(pg_get_serial_sequence($1, $2)) FROM generate_series(1, $3)`, "agentrt."+table, table+"_id", n)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

// issueText is a text to insert.
type issueText struct {
	id                    int64
	kind, person, channel string
	saidAt                time.Time
	where, body           string
	toRequester           bool
}

func textSHA(body string) string {
	h := sha256.Sum256([]byte(body))
	return hex.EncodeToString(h[:])
}

func insertText(ctx context.Context, tx pgx.Tx, issue, actor string, t issueText) (string, error) {
	sum := textSHA(t.body)
	var saidAt any
	if !t.saidAt.IsZero() {
		saidAt = t.saidAt
	}
	_, err := tx.Exec(ctx, `INSERT INTO agentrt.issue_text (issue_text_id, issue_id, text_kind, person, channel, said_at, said_where,
	    body, sha256, to_requester, created_by_actor_id)
	  OVERRIDING SYSTEM VALUE VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		t.id, issue, t.kind, nullable(t.person), t.channel, saidAt, nullable(t.where), t.body, sum, t.toRequester, actor)
	return sum, err
}

// checkText checks a text: valid UTF-8 without NUL, not blank, at most
// chars characters (or, for a crew text, maxCrewTextBytes bytes).
func checkText(what, s string, chars int) error {
	switch {
	case !utf8.ValidString(s) || strings.ContainsRune(s, 0):
		return invalid("%s is not UTF-8 text", what)
	case strings.TrimSpace(s) == "":
		return invalid("%s is empty", what)
	case chars > 0 && utf8.RuneCountInString(s) > chars:
		return invalid("%s is %d characters, more than %d", what, utf8.RuneCountInString(s), chars)
	case len(s) > maxCrewTextBytes:
		return invalid("%s is %d bytes, more than %d", what, len(s), maxCrewTextBytes)
	}
	return nil
}

// appendIssue appends an issue event to the issue's stream (body names it).
func appendIssue(ctx context.Context, tx pgx.Tx, typ, actor, issue, mission string, body map[string]any) (Appended, error) {
	body["issue_id"] = issue
	return Append(ctx, tx, Event{Stream: IssueStream(issue), Type: typ, Actor: actor, MissionID: mission, Body: body})
}

// supersede marks the issue's asks that match cond (a condition on
// agentrt.issue_ask, its own placeholders from $2 on) superseded: one
// issue_state_changed per mission the asks are of, since the envelope of the
// event that changes an ask names its mission (require_event, 0002). The
// asks are locked before any event is written.
func supersede(ctx context.Context, tx pgx.Tx, actor, issue, cond string, args ...any) ([]int64, error) {
	rows, err := tx.Query(ctx, `SELECT issue_ask_id, COALESCE(mission_id, '') FROM agentrt.issue_ask
	  WHERE issue_id = $1 AND superseded_at IS NULL AND (`+cond+`) ORDER BY issue_ask_id FOR UPDATE`, append([]any{issue}, args...)...)
	if err != nil {
		return nil, err
	}
	byMission := map[string][]int64{}
	var all []int64
	for rows.Next() {
		var id int64
		var m string
		if err := rows.Scan(&id, &m); err != nil {
			rows.Close()
			return nil, err
		}
		byMission[m] = append(byMission[m], id)
		all = append(all, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	missions := make([]string, 0, len(byMission))
	for m := range byMission {
		missions = append(missions, m)
	}
	slices.Sort(missions)
	for _, m := range missions {
		ids := byMission[m]
		if _, err := appendIssue(ctx, tx, "issue_state_changed", actor, issue, m,
			map[string]any{"change": "supersede", "ask_ids": anyInts(ids)}); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE agentrt.issue_ask SET superseded_at = clock_timestamp() WHERE issue_ask_id = ANY($1)`, ids); err != nil {
			return nil, err
		}
	}
	return all, nil
}

// crewWriter is a writer of the crew's own issue events: a thread (an
// operator token on the crew's list, routes in slice 2) or the Owner (§2.2).
func crewWriter(w Writer) (string, error) {
	switch kindOf(w.Actor) {
	case "thread":
		return ChannelThread, nil
	case "owner":
		return ChannelConsole, nil
	}
	return "", invalid("%s is not the crew: issues are worked by the crew's threads and the Owner", w.Actor)
}

// issueOp runs a crew write on one issue under the writer's key: the writer
// checked (a thread registered on its first write), then op.
func issueOp(ctx context.Context, db DB, w Writer, route, key string, request map[string]any,
	op func(tx pgx.Tx, channel string) (map[string]any, error)) (map[string]any, error) {
	channel, err := crewWriter(w)
	if err != nil {
		return nil, err
	}
	if key == "" {
		return nil, invalid("an idempotency key is required")
	}
	return idempotentAs(ctx, db, w, route, key, request, nil, func(tx pgx.Tx) (map[string]any, error) {
		if err := ensureWriter(ctx, tx, w, true); err != nil {
			return nil, err
		}
		return op(tx, channel)
	})
}

// written is a write's answer: the issue as it is after it.
func written(ctx context.Context, tx pgx.Tx, id string, extra map[string]any) (map[string]any, error) {
	r, err := readIssue(ctx, tx, id, false)
	if err != nil {
		return nil, err
	}
	out := r.Doc()
	// As every read shows it (issueshown.go).
	if out["state"], out["waiting_on"], err = shownOf(ctx, tx, id); err != nil {
		return nil, err
	}
	for k, v := range extra {
		out[k] = v
	}
	return out, nil
}

// anyInts is ids as an event body holds a list (canonical JSON takes []any).
func anyInts(ids []int64) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}
