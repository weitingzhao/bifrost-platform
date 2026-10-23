package store

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Bringing an issue (DESIGN-crew-intake §2.1, §3.2): the person's words, the
// agent's reading and the agent's own proposal, kept apart; for a person,
// her words are required; for the agent itself ("self") there are none, and
// it is a proposal or a question.

// IssueWords is one segment of a person's own words: verbatim, when and
// where (a page or a feature) she said it.
type IssueWords struct {
	Text   string
	SaidAt time.Time
	Where  string
}

// IssueSubmission is a new issue (submit_issue, file_issue).
type IssueSubmission struct {
	Product, Kind, Title string
	// OnBehalfOf is the person served (person:<name>) or "self", an agent's
	// own proposal.
	OnBehalfOf string
	Words      []IssueWords
	Reading    string // the agent's understanding
	Proposal   string // the agent's own suggestion
	DedupeKey  string // the agent's own key for "the same thing again"
	// Channel: "" is the writer's own (an agent's local bridge, a thread, the
	// Console); a thread relaying what the Owner relayed says owner_relay.
	Channel string
}

// SelfIssue is OnBehalfOf for an agent's own proposal.
const SelfIssue = "self"

func (s IssueSubmission) request() map[string]any {
	words := make([]any, 0, len(s.Words))
	for _, w := range s.Words {
		words = append(words, map[string]any{"text": w.Text, "said_at": timeOr(w.SaidAt), "where": w.Where})
	}
	return map[string]any{"product": s.Product, "kind": s.Kind, "title": s.Title, "on_behalf_of": s.OnBehalfOf, "words": words,
		"reading": s.Reading, "proposal": s.Proposal, "dedupe_key": s.DedupeKey, "channel": s.Channel}
}

func timeOr(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return timeText(t)
}

// writerChannel is the channel a writer brings texts by, checked against
// what it asked for: an external agent its local bridge (the other two are
// the later connections, §6), a thread itself or the Owner's relay, the
// Owner the Console.
func writerChannel(w Writer, asked string) (string, error) {
	var allowed []string
	switch kindOf(w.Actor) {
	case "agent":
		allowed = []string{ChannelIntake, "chrome", "public"}
	case "thread":
		allowed = []string{ChannelThread, ChannelOwnerRelay}
	case "owner":
		allowed = []string{ChannelConsole}
	default:
		return "", invalid("%s does not bring issues: an intake client, a crew thread or the Owner does", w.Actor)
	}
	if asked == "" {
		return allowed[0], nil
	}
	if !slices.Contains(allowed, asked) {
		return "", invalid("%s brings texts by %v, not %s", w.Actor, allowed, asked)
	}
	return asked, nil
}

// checkWords checks a submission's or an addition's texts.
func checkWords(words []IssueWords, reading, proposal string) error {
	if len(words) > MaxIssueWords {
		return invalid("%d segments of words, more than %d", len(words), MaxIssueWords)
	}
	for i, w := range words {
		if err := checkText(fmt.Sprintf("words %d", i+1), w.Text, MaxIssueTextChars); err != nil {
			return err
		}
		if w.Where != "" {
			if err := checkText(fmt.Sprintf("where words %d were said", i+1), w.Where, maxWhereChars); err != nil {
				return err
			}
		}
	}
	for what, s := range map[string]string{"the agent's reading": reading, "the agent's proposal": proposal} {
		if s != "" {
			if err := checkText(what, s, MaxIssueTextChars); err != nil {
				return err
			}
		}
	}
	return nil
}

// speaksFor checks whom a writer may bring words for on a channel (§1,
// §2.1): an agent only its bound person (the database checks it again); a
// thread the Owner, or anyone when it relays the Owner's relay; the Owner
// himself.
func speaksFor(w Writer, channel, person string) error {
	switch kindOf(w.Actor) {
	case "thread":
		if channel == ChannelThread && person != OwnerPerson {
			return invalid("a thread brings %s's words as the Owner's relay (channel owner_relay)", person)
		}
	case "owner":
		if person != OwnerPerson {
			return invalid("the Owner brings his own words; another person's come by her agent or by his relay")
		}
	}
	return nil
}

// SubmitIssue brings a new issue: BI-<n>, one more than the largest there
// is, clarifying, waiting on the crew. An external agent may submit only for
// the person it is bound to, in its products, within its daily limits
// (ErrQuota). The answer lists open issues of the same person (or the same
// agent's own) and product with the same dedupe key.
func SubmitIssue(ctx context.Context, db DB, w Writer, key string, s IssueSubmission) (map[string]any, error) {
	if key == "" {
		return nil, invalid("an idempotency key is required")
	}
	channel, err := writerChannel(w, s.Channel)
	if err != nil {
		return nil, err
	}
	switch {
	case !productRE.MatchString(s.Product):
		return nil, invalid("%q is not a product key", s.Product)
	case !slices.Contains(issueKinds, s.Kind):
		return nil, invalid("kind is one of %v, not %q", issueKinds, s.Kind)
	case s.OnBehalfOf != SelfIssue && !personIDRE.MatchString(s.OnBehalfOf):
		return nil, invalid("on_behalf_of is a person (person:<name>) or self, not %q", s.OnBehalfOf)
	}
	if err := checkText("the title", s.Title, MaxIssueTitle); err != nil {
		return nil, err
	}
	if s.DedupeKey != "" {
		if err := checkText("the dedupe key", s.DedupeKey, 200); err != nil {
			return nil, err
		}
	}
	if err := checkWords(s.Words, s.Reading, s.Proposal); err != nil {
		return nil, err
	}
	served := s.OnBehalfOf
	if s.OnBehalfOf == SelfIssue {
		served = ""
		switch {
		case len(s.Words) > 0:
			return nil, invalid("an agent's own issue carries no person's words")
		case s.Kind != "proposal" && s.Kind != "question":
			return nil, invalid("an agent's own issue is a proposal or a question")
		case s.Reading == "" && s.Proposal == "":
			return nil, invalid("an agent's own issue says what it understands or proposes")
		case channel == ChannelOwnerRelay || kindOf(w.Actor) == "owner":
			return nil, invalid("an own proposal is an agent's or a thread's, not a relay of the Owner's")
		}
	} else {
		if len(s.Words) == 0 {
			return nil, invalid("an issue brought for %s carries her own words", s.OnBehalfOf)
		}
		if s.Kind == "proposal" {
			return nil, invalid("a proposal is an agent's own; a person's issue is a complaint, a question or a request")
		}
		if err := speaksFor(w, channel, s.OnBehalfOf); err != nil {
			return nil, err
		}
	}
	// An agent's scope, checked before a replay too.
	scope := func(tx pgx.Tx) error {
		if kindOf(w.Actor) != "agent" {
			return nil
		}
		c, err := readIntakeClient(ctx, tx, w.Actor, false)
		if err != nil {
			return err
		}
		if !slices.Contains(c.products, s.Product) {
			return invalid("%s brings issues of %v, not %s", w.Actor, c.products, s.Product)
		}
		if served != "" && served != c.behalfOf {
			return invalid("%s speaks for %s only", w.Actor, orNobody(c.behalfOf))
		}
		return nil
	}
	return idempotentHeld(ctx, db, w, "issue-submit", key, s.request(), scope, func(tx pgx.Tx) (map[string]any, error) {
		if err := ensureWriter(ctx, tx, w, true); err != nil {
			return nil, err
		}
		if kindOf(w.Actor) == "agent" {
			if err := intakeDaily(ctx, tx, w.Actor, "issue_submitted", served == ""); err != nil {
				return nil, err
			}
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742121)`); err != nil {
			return nil, err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(substr(issue_id, 4)::int), 0) + 1 FROM agentrt.issue`).Scan(&n); err != nil {
			return nil, err
		}
		id := fmt.Sprintf("BI-%d", n)
		texts := submissionTexts(s.Words, s.Reading, s.Proposal, served, channel)
		ids, err := nextIDs(ctx, tx, "issue_text", len(texts))
		if err != nil {
			return nil, err
		}
		body := map[string]any{"product": s.Product, "kind": s.Kind, "channel": channel, "text_ids": anyInts(ids)}
		if served != "" {
			body["served_person"] = served
		}
		if _, err := appendIssue(ctx, tx, "issue_submitted", w.Actor, id, "", body); err != nil {
			return nil, err
		}
		asks := served
		if asks == "" {
			asks = OwnerPerson
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.issue (issue_id, product, kind, title, served_person, asks_person, state, waiting_on,
		    dedupe_key, created_by_actor_id) VALUES ($1,$2,$3,$4,$5,$6,'clarifying','crew',$7,$8)`,
			id, s.Product, s.Kind, s.Title, nullable(served), asks, nullable(s.DedupeKey), w.Actor); err != nil {
			return nil, err
		}
		for i, t := range texts {
			t.id = ids[i]
			if _, err := insertText(ctx, tx, id, w.Actor, t); err != nil {
				return nil, err
			}
		}
		dups, err := maybeDuplicates(ctx, tx, id, s.Product, served, w.Actor, s.DedupeKey)
		if err != nil {
			return nil, err
		}
		return written(ctx, tx, id, map[string]any{"maybe_duplicate_of": dups,
			"next": "The crew reads it and writes a brief for the person to confirm; nothing is built before that."})
	})
}

// agentSees is the scope check of an agent's write on an issue: it sees the
// issue (intakeSees); nothing for another writer.
func agentSees(ctx context.Context, w Writer, issue string) func(pgx.Tx) error {
	return func(tx pgx.Tx) error {
		if kindOf(w.Actor) != "agent" {
			return nil
		}
		r, err := readIssue(ctx, tx, issue, false)
		if err != nil {
			return err
		}
		return intakeSees(ctx, tx, w.Actor, r)
	}
}

func orNobody(p string) string {
	if p == "" {
		return "nobody but itself"
	}
	return p
}

// submissionTexts are the rows a submission or an addition writes: each
// segment of words, the reading, the proposal.
func submissionTexts(words []IssueWords, reading, proposal, person, channel string) []issueText {
	var out []issueText
	for _, w := range words {
		out = append(out, issueText{kind: "words", person: person, channel: channel, saidAt: w.SaidAt, where: w.Where, body: w.Text})
	}
	if reading != "" {
		out = append(out, issueText{kind: "reading", channel: channel, body: reading})
	}
	if proposal != "" {
		out = append(out, issueText{kind: "proposal", channel: channel, body: proposal})
	}
	return out
}

// maybeDuplicates are open issues of the same person (for an own proposal,
// of the same writer) and product with the same dedupe key (§2.1: only the
// same person's).
func maybeDuplicates(ctx context.Context, tx pgx.Tx, id, product, served, creator, key string) ([]string, error) {
	if key == "" {
		return []string{}, nil
	}
	rows, err := tx.Query(ctx, `SELECT issue_id FROM agentrt.issue WHERE product = $1 AND dedupe_key = $2 AND issue_id <> $3
	  AND state <> 'closed' AND (CASE WHEN $4 = '' THEN served_person IS NULL AND created_by_actor_id = $5 ELSE served_person = $4 END)
	  ORDER BY substr(issue_id, 4)::int`, product, key, id, served, creator)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if out == nil {
		out = []string{}
	}
	return out, err
}

// IssueAddition adds to an issue (add_to_issue): at least one of words,
// reading, proposal.
type IssueAddition struct {
	IssueID  string
	Words    []IssueWords
	Reading  string
	Proposal string
	Channel  string
}

func (a IssueAddition) request() map[string]any {
	r := IssueSubmission{Words: a.Words, Reading: a.Reading, Proposal: a.Proposal, Channel: a.Channel}.request()
	r["issue_id"] = a.IssueID
	return r
}

// AddToIssue adds texts to an open issue: its person's words (only for an
// issue that serves a person), the agent's reading or proposal. An external
// agent adds only to issues it sees and within its daily limit.
func AddToIssue(ctx context.Context, db DB, w Writer, key string, a IssueAddition) (map[string]any, error) {
	if key == "" {
		return nil, invalid("an idempotency key is required")
	}
	channel, err := writerChannel(w, a.Channel)
	if err != nil {
		return nil, err
	}
	if len(a.Words) == 0 && a.Reading == "" && a.Proposal == "" {
		return nil, invalid("an addition carries words, a reading or a proposal")
	}
	if err := checkWords(a.Words, a.Reading, a.Proposal); err != nil {
		return nil, err
	}
	if !issueIDRE.MatchString(a.IssueID) {
		return nil, invalid("%q is not an issue id (BI-<n>)", a.IssueID)
	}
	return idempotentHeld(ctx, db, w, "issue-add", key, a.request(), agentSees(ctx, w, a.IssueID), func(tx pgx.Tx) (map[string]any, error) {
		if err := ensureWriter(ctx, tx, w, true); err != nil {
			return nil, err
		}
		r, err := readIssue(ctx, tx, a.IssueID, true)
		if err != nil {
			return nil, err
		}
		if kindOf(w.Actor) == "agent" {
			if err := intakeDaily(ctx, tx, w.Actor, "issue_added", false); err != nil {
				return nil, err
			}
		}
		if r.State == "closed" {
			return nil, fmt.Errorf("%w: %s is closed (%s); bring a new issue", ErrState, r.ID, strings.TrimSpace(r.Conclusion+" "+r.DuplicateOf))
		}
		if len(a.Words) > 0 {
			if r.ServedPerson == "" {
				return nil, invalid("%s is an agent's own proposal: it carries no person's words", r.ID)
			}
			if err := speaksFor(w, channel, r.ServedPerson); err != nil {
				return nil, err
			}
		}
		texts := submissionTexts(a.Words, a.Reading, a.Proposal, r.ServedPerson, channel)
		ids, err := nextIDs(ctx, tx, "issue_text", len(texts))
		if err != nil {
			return nil, err
		}
		ev, err := appendIssue(ctx, tx, "issue_added", w.Actor, r.ID, "", map[string]any{"channel": channel, "text_ids": anyInts(ids)})
		if err != nil {
			return nil, err
		}
		for i, t := range texts {
			t.id = ids[i]
			if _, err := insertText(ctx, tx, r.ID, w.Actor, t); err != nil {
				return nil, err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE agentrt.issue SET version = version + 1 WHERE issue_id = $1`, r.ID); err != nil {
			return nil, err
		}
		return written(ctx, tx, r.ID, map[string]any{"update_id": ev.EventID})
	})
}
