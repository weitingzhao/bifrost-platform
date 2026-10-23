package store

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Reading business issues (DESIGN-crew-intake §5.3, §8; slice 2). Two
// projections, and nothing in between:
//   - the crew's, for the issue readers (an operator or admin token on the
//     readers list, actuation.RequireIssueReader): every issue, in full;
//   - the side that brought it, for an intake client: the issues it sees
//     (intakeSees), with only its own and its person's texts, what the crew
//     marked for it (to_requester) and the ask open now; never the class's
//     reasons, the design, a review, a seat's check or an internal note.
//
// Nothing here is reached by the generic reads: the texts are rows of
// issue_text, which no artifact or event route reads, and the issue streams
// are hidden from GET /events (agentrtapi hiddenFrom).

// The views of the crew's list (§8): what it shows, by state.
var issueViews = map[string][]string{
	"new":           {"clarifying", "brief_confirmed"},
	"triage":        {"classified", "designing", "in_review", "owner_deciding"},
	"active":        {"active"},
	"awaiting_user": {"awaiting_user"},
	"parked":        {"parked"},
	"closed":        {"closed"},
}

// IssueViews are the names of the crew list's views.
func IssueViews() []string {
	out := make([]string, 0, len(issueViews))
	for k := range issueViews {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// IssueListFilter narrows the crew's list: a view or a state, a product,
// whom it waits on; at most Limit (1 to 500, default 100), newest first,
// below the issue Before when given.
type IssueListFilter struct {
	View, State, Product, WaitingOn, Before string
	Limit                                   int
}

// updateTypesSQL are the issue events an intake client is sent, as SQL over
// an event row e (§2.1 get_updates: received, the asks, classified, taken,
// reply, closed, reopened, merged). Everything else on an issue's stream is
// the crew's own record.
const updateTypesSQL = `(e.event_type IN ('issue_submitted','issue_brief_drafted','issue_asked','issue_classified','issue_taken',
  'issue_replied','issue_merged') OR (e.event_type = 'issue_state_changed' AND e.body_json->>'change' IN ('close','reopen')))`

// receiptSQL is a row's receipt (§8: three things kept apart): how many
// updates the agent that brought the issue has not acknowledged, when it
// last acknowledged, and the newest ask with whether it was answered. i is
// the issue row.
const receiptSQL = `CASE WHEN i.created_by_actor_id LIKE 'agent:%' THEN (SELECT jsonb_build_object(
    'agent', c.intake_client_id,
    'pending', (SELECT count(*) FROM agentrt.event e WHERE e.event_stream_id = 'issue:' || i.issue_id AND ` + updateTypesSQL + `
      AND (c.acked_txid IS NULL OR (e.txid, e.event_id) > (c.acked_txid, c.acked_event))),
    'acked_at', c.acked_at)
  FROM agentrt.intake_client c WHERE c.intake_client_id = i.created_by_actor_id) END`

// issueRowSQL is one issue as the crew's list shows it (§8's row), i the
// issue and p its person.
const issueRowSQL = `jsonb_build_object('issue_id', i.issue_id, 'product', i.product, 'kind', i.kind, 'title', i.title,
  'served_person', CASE WHEN i.served_person IS NULL THEN NULL ELSE jsonb_build_object('id', i.served_person, 'name', p.display_name) END,
  'asks_person', i.asks_person,
  'via', jsonb_build_object('actor', i.created_by_actor_id, 'channel', (SELECT s.body_json->>'channel' FROM agentrt.event s
    WHERE s.event_stream_id = 'issue:' || i.issue_id AND s.event_type = 'issue_submitted' ORDER BY s.event_id LIMIT 1)),
  'state', `+shownStateSQL+`, 'waiting_on', `+shownWaitingSQL+`, 'conclusion', i.conclusion, 'risk_class', i.risk_class, 'priority', i.priority,
  'duplicate_of', i.duplicate_of, 'version', i.version,
  'raised_times', 1 + (SELECT count(*) FROM agentrt.issue d WHERE d.duplicate_of = i.issue_id),
  'review', (SELECT jsonb_build_object('round', c.review_round, 'result', c.result) FROM agentrt.issue_check c
    WHERE c.issue_id = i.issue_id AND c.check_kind = 'review' ORDER BY c.issue_check_id DESC LIMIT 1),
  'received_at', i.created_at,
  'updated_at', (SELECT max(e.at) FROM agentrt.event e WHERE e.event_stream_id = 'issue:' || i.issue_id),
  'next', `+shownNextSQL+`,
  'work', (SELECT COALESCE(jsonb_agg(jsonb_build_object('mission_id', w.mission_id, 'commitment', w.commitment, 'state', m.state,
      'live', w.ended_at IS NULL) ORDER BY w.issue_work_id), '[]'::jsonb)
    FROM agentrt.issue_work w JOIN agentrt.mission m ON m.mission_id = w.mission_id WHERE w.issue_id = i.issue_id),
  'open_ask', (SELECT jsonb_build_object('ask_id', k.issue_ask_id, 'purpose', k.purpose, 'answered', k.verdict IS NOT NULL)
    FROM agentrt.issue_ask k WHERE k.issue_id = i.issue_id AND k.superseded_at IS NULL ORDER BY k.issue_ask_id DESC LIMIT 1),
  'receipt', ` + receiptSQL + `)`

// ListIssues is the crew's list (list_issues, GET /issues): rows of §8,
// newest first, and whether there were more.
func ListIssues(ctx context.Context, db DB, f IssueListFilter) (map[string]any, error) {
	var states []string
	switch {
	case f.View != "" && f.State != "":
		return nil, invalid("a view or a state, not both")
	case f.View != "":
		s, ok := issueViews[f.View]
		if !ok {
			return nil, invalid("a view is one of %v, not %q", IssueViews(), f.View)
		}
		states = s
	case f.State != "":
		if !slices.Contains(issueStates, f.State) {
			return nil, invalid("a state is one of %v, not %q", issueStates, f.State)
		}
		states = []string{f.State}
	}
	if f.WaitingOn != "" && !slices.Contains([]string{"crew", "user", "owner"}, f.WaitingOn) {
		return nil, invalid("waiting_on is crew, user or owner, not %q", f.WaitingOn)
	}
	if f.Product != "" && !productRE.MatchString(f.Product) {
		return nil, invalid("%q is not a product key", f.Product)
	}
	before := 0
	if f.Before != "" {
		if !issueIDRE.MatchString(f.Before) {
			return nil, invalid("before is an issue id (BI-<n>), not %q", f.Before)
		}
		before = issueNumber(f.Before)
	}
	limit := f.Limit
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 500 {
		return nil, invalid("limit is 1 to 500")
	}
	var doc []byte
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT jsonb_build_object(
		    'issues', COALESCE(jsonb_agg(jsonb_strip_nulls(x.r) ORDER BY x.n DESC) FILTER (WHERE x.k <= $5), '[]'::jsonb),
		    'truncated', COALESCE(max(x.k) > $5, false))
		  FROM (SELECT substr(i.issue_id, 4)::int AS n, row_number() OVER (ORDER BY substr(i.issue_id, 4)::int DESC) AS k, `+issueRowSQL+` AS r
		    FROM agentrt.issue i LEFT JOIN agentrt.person p ON p.person_id = i.served_person
		    WHERE ($1::text[] IS NULL OR `+shownStateSQL+` = ANY($1)) AND ($2 = '' OR i.product = $2) AND ($3 = '' OR `+shownWaitingSQL+` = $3)
		      AND ($4 = 0 OR substr(i.issue_id, 4)::int < $4)
		    ORDER BY substr(i.issue_id, 4)::int DESC LIMIT $5 + 1) x`, states, f.Product, f.WaitingOn, before, limit).Scan(&doc)
	})
	if err != nil {
		return nil, err
	}
	var out map[string]any
	return out, json.Unmarshal(doc, &out)
}

// IssueFull is one issue for the crew (get_issue_full, GET /issues/{id}, the
// Console's read-only issue page, §5.3, §8): the row, the current brief and
// design with the design's decision table, every text, ask, check and piece
// of work, the issue's events (ids, enums and hashes) and its Owner's cards.
func IssueFull(ctx context.Context, db DB, id string) (map[string]any, error) {
	if !issueIDRE.MatchString(id) {
		return nil, invalid("%q is not an issue id (BI-<n>)", id)
	}
	out := map[string]any{}
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		r, err := readIssue(ctx, tx, id, false)
		if err != nil {
			return err
		}
		var row []byte
		if err := tx.QueryRow(ctx, `SELECT jsonb_strip_nulls(`+issueRowSQL+`) || jsonb_strip_nulls(jsonb_build_object(
		    'created_by', i.created_by_actor_id, 'dedupe_key', i.dedupe_key, 'business_owner', i.business_owner, 'build_owner', i.build_owner))
		  FROM agentrt.issue i LEFT JOIN agentrt.person p ON p.person_id = i.served_person WHERE i.issue_id = $1`, id).Scan(&row); err != nil {
			return err
		}
		var issue map[string]any
		if err := json.Unmarshal(row, &issue); err != nil {
			return err
		}
		if r.briefText != 0 {
			issue["brief"] = map[string]any{"text_id": r.briefText, "sha256": r.briefSHA, "confirmed": r.BriefConfirmed}
		}
		if r.designText != 0 {
			points, err := designPoints(ctx, tx, r)
			if err != nil {
				return err
			}
			decisions := make([]any, 0, len(points))
			for _, p := range points {
				decisions = append(decisions, map[string]any{"decision": p.Key, "proceed": p.Proceed})
			}
			issue["design"] = map[string]any{"text_id": r.designText, "sha256": r.designSHA, "decisions": decisions}
		}
		out["issue"] = issue
		for key, q := range map[string]string{
			"texts": `SELECT COALESCE(jsonb_agg(jsonb_strip_nulls(jsonb_build_object('text_id', t.issue_text_id, 'kind', t.text_kind,
			    'person', t.person, 'channel', t.channel, 'said_at', t.said_at, 'where', t.said_where, 'body', t.body, 'sha256', t.sha256,
			    'to_requester', t.to_requester, 'by', t.created_by_actor_id, 'at', t.created_at)) ORDER BY t.issue_text_id), '[]'::jsonb)
			  FROM agentrt.issue_text t WHERE t.issue_id = $1`,
			"asks": `SELECT COALESCE(jsonb_agg(jsonb_strip_nulls(jsonb_build_object('ask_id', k.issue_ask_id, 'person', k.person,
			    'purpose', k.purpose, 'bound_text_id', k.bound_text_id, 'bound_sha256', k.bound_sha256, 'brief_sha256', k.brief_sha256,
			    'commitment', k.commitment, 'mission_id', k.mission_id, 'env', k.env, 'commits', to_jsonb(k.commits), 'verdict', k.verdict,
			    'words_text_id', k.words_text_id, 'said_at', k.said_at, 'answered_by', k.answered_by_actor_id, 'answer_channel', k.answer_channel,
			    'answered_at', k.answered_at, 'superseded_at', k.superseded_at)) ORDER BY k.issue_ask_id), '[]'::jsonb)
			  FROM agentrt.issue_ask k WHERE k.issue_id = $1`,
			"checks": `SELECT COALESCE(jsonb_agg(jsonb_strip_nulls(jsonb_build_object('check_id', c.issue_check_id, 'kind', c.check_kind,
			    'result', c.result, 'category', c.category, 'brief_sha256', c.brief_sha256, 'design_sha256', c.design_sha256,
			    'report_text_id', c.report_text_id, 'seat_report', c.seat_artifact_id, 'reviewer_tool', c.reviewer_tool,
			    'author_tool', c.author_tool, 'round', c.review_round, 'question_id', c.question_id, 'answered_by', c.answered_by,
			    'answer_channel', c.answer_channel, 'ask_id', c.ask_id, 'by', c.created_by_actor_id, 'at', c.created_at))
			    ORDER BY c.issue_check_id), '[]'::jsonb)
			  FROM agentrt.issue_check c WHERE c.issue_id = $1`,
			"timeline": `SELECT COALESCE(jsonb_agg(jsonb_build_object('event_id', e.event_id, 'type', e.event_type, 'actor', e.actor_id,
			    'at', e.at, 'mission_id', e.mission_id, 'body', e.body_json) ORDER BY e.event_id), '[]'::jsonb)
			  FROM agentrt.event e WHERE e.event_stream_id = 'issue:' || $1`,
		} {
			var doc []byte
			if err := tx.QueryRow(ctx, q, id).Scan(&doc); err != nil {
				return fmt.Errorf("%s of %s: %w", key, id, err)
			}
			var v any
			if err := json.Unmarshal(doc, &v); err != nil {
				return err
			}
			out[key] = v
		}
		cards, err := issueCards(ctx, tx, id)
		if err != nil {
			return err
		}
		out["cards"] = cards
		return nil
	})
	return out, err
}

// IssueSeen is an issue as an intake client reads it (get_issue, §2.1, §5.3,
// the side that brought it): its state; its person's words and answers and
// this agent's own reading, proposal and notes; what the crew wrote for it
// (to_requester: briefs and replies); the ask open now, with the text it
// asks about. An issue it does not see is answered as one that does not
// exist.
func IssueSeen(ctx context.Context, db DB, w Writer, id string) (map[string]any, error) {
	if !issueIDRE.MatchString(id) {
		return nil, invalid("%q is not an issue id (BI-<n>)", id)
	}
	var out map[string]any
	err := intakeTx(ctx, db, w, func(tx pgx.Tx, _ intakeClient) error {
		r, err := readIssue(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if err := intakeSees(ctx, tx, w.Actor, r); err != nil {
			return err
		}
		var doc []byte
		err = tx.QueryRow(ctx, `SELECT jsonb_strip_nulls(jsonb_build_object('issue_id', i.issue_id, 'product', i.product, 'kind', i.kind,
		    'title', i.title, 'served_person', i.served_person, 'state', `+shownStateSQL+`, 'waiting_on', `+shownWaitingSQL+`, 'conclusion', i.conclusion,
		    'duplicate_of', i.duplicate_of, 'version', i.version,
		    'updated_at', (SELECT max(e.at) FROM agentrt.event e WHERE e.event_stream_id = 'issue:' || i.issue_id),
		    'texts', (SELECT COALESCE(jsonb_agg(jsonb_strip_nulls(jsonb_build_object('text_id', t.issue_text_id, 'kind', t.text_kind,
		        'person', t.person, 'said_at', t.said_at, 'where', t.said_where, 'body', t.body, 'at', t.created_at)) ORDER BY t.issue_text_id), '[]'::jsonb)
		      FROM agentrt.issue_text t WHERE t.issue_id = i.issue_id
		        AND ((t.text_kind IN ('words','response') AND t.person IS NOT DISTINCT FROM i.served_person AND i.served_person IS NOT NULL)
		          OR (t.text_kind IN ('reading','proposal','note') AND t.created_by_actor_id = $2 AND NOT t.to_requester))),
		    'replies', (SELECT COALESCE(jsonb_agg(jsonb_build_object('text_id', t.issue_text_id, 'kind', t.text_kind, 'body', t.body,
		        'at', t.created_at) ORDER BY t.issue_text_id), '[]'::jsonb)
		      FROM agentrt.issue_text t WHERE t.issue_id = i.issue_id AND t.to_requester),
		    'open_ask', (SELECT jsonb_strip_nulls(jsonb_build_object('ask_id', k.issue_ask_id, 'purpose', k.purpose, 'person', k.person,
		        'text_id', k.bound_text_id, 'text', (SELECT t.body FROM agentrt.issue_text t WHERE t.issue_text_id = k.bound_text_id AND t.to_requester),
		        'env', k.env, 'commits', to_jsonb(k.commits)))
		      FROM agentrt.issue_ask k WHERE k.issue_id = i.issue_id AND k.verdict IS NULL AND k.superseded_at IS NULL
		      ORDER BY k.issue_ask_id DESC LIMIT 1)))
		  FROM agentrt.issue i WHERE i.issue_id = $1`, id, w.Actor).Scan(&doc)
		if err != nil {
			return err
		}
		return json.Unmarshal(doc, &out)
	})
	return out, err
}

// MyIssuesFilter narrows an intake client's list: a state, a product, at
// most Limit (1 to 200, default 50), newest first.
type MyIssuesFilter struct {
	State, Product string
	Limit          int
}

// MyIssues are the issues an intake client sees (my_issues, §2.1): its own
// proposals and those serving the person it is bound to, of its products;
// each with whom it waits on and the ask open now.
func MyIssues(ctx context.Context, db DB, w Writer, f MyIssuesFilter) (map[string]any, error) {
	if f.State != "" && !slices.Contains(issueStates, f.State) {
		return nil, invalid("a state is one of %v, not %q", issueStates, f.State)
	}
	if f.Product != "" && !productRE.MatchString(f.Product) {
		return nil, invalid("%q is not a product key", f.Product)
	}
	limit := f.Limit
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 {
		return nil, invalid("limit is 1 to 200")
	}
	var out map[string]any
	err := intakeTx(ctx, db, w, func(tx pgx.Tx, c intakeClient) error {
		var doc []byte
		err := tx.QueryRow(ctx, `SELECT jsonb_build_object(
		    'issues', COALESCE(jsonb_agg(x.r ORDER BY x.n DESC) FILTER (WHERE x.k <= $5), '[]'::jsonb),
		    'truncated', COALESCE(max(x.k) > $5, false))
		  FROM (SELECT substr(i.issue_id, 4)::int AS n, row_number() OVER (ORDER BY substr(i.issue_id, 4)::int DESC) AS k,
		      jsonb_strip_nulls(jsonb_build_object('issue_id', i.issue_id, 'product', i.product, 'kind', i.kind, 'title', i.title,
		        'state', `+shownStateSQL+`, 'conclusion', i.conclusion, 'waiting_on', `+shownWaitingSQL+`, 'version', i.version, 'duplicate_of', i.duplicate_of,
		        'updated_at', (SELECT max(e.at) FROM agentrt.event e WHERE e.event_stream_id = 'issue:' || i.issue_id),
		        'open_ask', (SELECT jsonb_build_object('ask_id', k.issue_ask_id, 'purpose', k.purpose) FROM agentrt.issue_ask k
		          WHERE k.issue_id = i.issue_id AND k.verdict IS NULL AND k.superseded_at IS NULL ORDER BY k.issue_ask_id DESC LIMIT 1))) AS r
		    FROM agentrt.issue i
		    WHERE i.product = ANY($2) AND ((i.served_person IS NOT NULL AND i.served_person = $3) OR (i.served_person IS NULL AND i.created_by_actor_id = $1))
		      AND ($4 = '' OR `+shownStateSQL+` = $4) AND ($6 = '' OR i.product = $6)
		    ORDER BY substr(i.issue_id, 4)::int DESC LIMIT $5 + 1) x`, w.Actor, c.products, c.behalfOf, f.State, limit, f.Product).Scan(&doc)
		if err != nil {
			return err
		}
		return json.Unmarshal(doc, &out)
	})
	return out, err
}

// intakeTx runs a read (or a liveness write) of an intake client in one
// transaction, after the checks its writes make (§2.1: reads, writes and
// replays alike): the writer's lock, its actor an agent's, its token the one
// bound to it, not revoked.
func intakeTx(ctx context.Context, db DB, w Writer, fn func(pgx.Tx, intakeClient) error) error {
	if kindOf(w.Actor) != "agent" {
		return invalid("%s is no intake client", w.Actor)
	}
	return InTx(ctx, db, func(tx pgx.Tx) error {
		if err := holdsSessionTx(ctx, tx, w); err != nil {
			return err
		}
		c, err := readIntakeClient(ctx, tx, w.Actor, false)
		if err != nil {
			return err
		}
		return fn(tx, c)
	})
}

// IntakeGuide is the server's half of intake_guide (§2.1, §3.1): who the
// client is, whom it speaks for, its products (and whether product is one),
// its daily limits and what it used of them in the last 24 hours, and how
// far it was served and acknowledged (a bridge that lost its cursor reads
// on from its acknowledgement, §3.3). The guide's text and version travel
// with the local bridge.
func IntakeGuide(ctx context.Context, db DB, w Writer, product string) (map[string]any, error) {
	var out map[string]any
	err := intakeTx(ctx, db, w, func(tx pgx.Tx, c intakeClient) error {
		var submits, adds, own int
		if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE event_type = 'issue_submitted'),
		    count(*) FILTER (WHERE event_type = 'issue_added'),
		    count(*) FILTER (WHERE event_type = 'issue_submitted' AND NOT body_json ? 'served_person')
		  FROM agentrt.event WHERE actor_id = $1 AND event_type IN ('issue_submitted','issue_added')
		    AND at > clock_timestamp() - interval '24 hours'`, w.Actor).Scan(&submits, &adds, &own); err != nil {
			return err
		}
		out = map[string]any{"intake_client_id": c.id, "token_name": c.token, "products": c.products,
			"limits": map[string]any{"submits": c.submits, "adds": c.adds, "proposals": c.proposals},
			"used":   map[string]any{"submits": submits, "adds": adds, "proposals": own},
			"guide":  "The guide's text and version come with the local bridge; this is the platform's half."}
		if c.behalfOf != "" {
			out["on_behalf_of"] = c.behalfOf
		}
		if product != "" {
			out["product"] = map[string]any{"key": product, "in_scope": slices.Contains(c.products, product)}
		}
		if c.hasServed {
			out["served"] = c.served.String()
		}
		if c.hasAcked {
			out["acked"] = c.acked.String()
		}
		return nil
	})
	return out, err
}

// trimIssuePrefix is a stream's issue id ("" for another stream).
func trimIssuePrefix(stream string) string {
	id, ok := strings.CutPrefix(stream, "issue:")
	if !ok {
		return ""
	}
	return id
}
