package planstore

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Where the Owner's complaints, pains and needs go (Owner 2026-11-25 in the
// legislation thread: 「很多失望，痛点，谩骂，抱怨，归根结底都是系统设计没有实现
// 业务目标，所以才需要做这个法律框架和真正的可执行的法……需要有一个地方放用户的吐
// 槽，痛点，需求就应该放在这里」; and 「你第三轮扫出来的那些吐槽需要有一个去处」).
//
// An issue is one thing he keeps saying, drawn from what he said (owner_said):
// his words, how often, when first and last, the messages, and where it goes
// (LAW-1 七、八: complaints, pains and needs are distilled into law or
// dropped): a new law, an amendment, the law is there and enforcing it is what
// is missing, a domain's own rule, dropped, or a conflict he decides. Each
// change of an issue is a new owner_issue record on owner-issue:<ISSUE-n>; the
// last one is the issue. Once the law that answers it is in force, the issue's
// reading says whether the same pain came back: the evidence he said after the
// law went in force.

// OwnerIssue is one issue as recorded.
type OwnerIssue struct {
	Issue  string   `json:"issue"`
	Title  string   `json:"title"`
	Kinds  []string `json:"kinds"`
	Action string   `json:"action"`
	Detail string   `json:"detail"`
	// To is the line or domain that acts on it (plan, admin, audit, or a
	// domain id); empty while the legislation line has not routed it.
	To     string `json:"to,omitempty"`
	LawRef string `json:"law_ref,omitempty"`
	Gap    string `json:"gap,omitempty"`
	Status string `json:"status"`
	// Evidence are the owner_said message ids the issue is drawn from.
	Evidence []string  `json:"evidence"`
	Quotes   []string  `json:"quotes,omitempty"`
	Times    int       `json:"times"`
	FirstAt  time.Time `json:"first_at"`
	LastAt   time.Time `json:"last_at"`
	// LawInForceAt is when the law that answers the issue went in force.
	LawInForceAt *time.Time `json:"law_in_force_at,omitempty"`
	Round        int        `json:"round,omitempty"`
}

// OwnerIssueRead is an issue as read: the record, its version and who wrote
// it, and whether the pain came back after the law.
type OwnerIssueRead struct {
	OwnerIssue
	Version int       `json:"version"`
	By      string    `json:"by"`
	At      time.Time `json:"at"`
	// SaidAfterLaw counts the evidence he said after LawInForceAt (nil while
	// no law answers the issue): above zero, the law did not end the pain.
	SaidAfterLaw *int `json:"said_after_law,omitempty"`
}

var (
	issueCode   = regexp.MustCompile(`^ISSUE-[1-9][0-9]{0,4}$`)
	issueTo     = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)
	issueKinds  = map[string]bool{"direction": true, "standard": true, "wall": true, "pain": true, "need": true}
	issueAction = map[string]bool{"law-new": true, "law-amend": true, "enforce": true, "rule": true, "drop": true, "conflict": true}
	issueStatus = map[string]bool{"open": true, "drafted": true, "in_force": true, "closed": true, "dropped": true}
)

// OwnerIssueStream is an issue's stream.
func OwnerIssueStream(issue string) string { return "owner-issue:" + issue }

func fitsIn(s string, most int) bool { return s != "" && utf8.ValidString(s) && len(s) <= most }

func checkOwnerIssue(m OwnerIssue) error {
	bad := func(msg string) error { return eventlog.Invalid("%s: %s", m.Issue, msg) }
	switch {
	case !issueCode.MatchString(m.Issue):
		return eventlog.Invalid("issue is ISSUE-<n>")
	case !fitsIn(m.Title, 400):
		return bad("title is required, at most 400 bytes")
	case len(m.Kinds) == 0:
		return bad("kinds name at least one of direction, standard, wall, pain, need")
	case !issueAction[m.Action]:
		return bad("action is law-new, law-amend, enforce, rule, drop or conflict")
	case !fitsIn(m.Detail, 4000):
		return bad("detail is required, at most 4000 bytes")
	case m.To != "" && !issueTo.MatchString(m.To):
		return bad("to is a line or domain id")
	case m.LawRef != "" && !fitsIn(m.LawRef, 200), m.Gap != "" && !fitsIn(m.Gap, 2000):
		return bad("law_ref at most 200 bytes, gap at most 2000")
	case !issueStatus[m.Status]:
		return bad("status is open, drafted, in_force, closed or dropped")
	case len(m.Evidence) == 0 || len(m.Evidence) > 5000:
		return bad("evidence names 1 to 5000 messages")
	case len(m.Quotes) > 5:
		return bad("at most 5 quotes")
	case m.Times < 1:
		return bad("times is at least 1")
	case m.FirstAt.IsZero() || m.LastAt.Before(m.FirstAt):
		return bad("first_at is required and last_at is not before it")
	case m.Status == "in_force" && m.LawInForceAt == nil:
		return bad("an issue in force names law_in_force_at")
	}
	for _, k := range m.Kinds {
		if !issueKinds[k] {
			return bad("kind " + k + " is not direction, standard, wall, pain or need")
		}
	}
	for _, id := range m.Evidence {
		if !ownerSaidID.MatchString(id) {
			return bad("evidence is owner_said message ids")
		}
	}
	for _, q := range m.Quotes {
		if !fitsIn(q, 400) {
			return bad("a quote is at most 400 bytes")
		}
	}
	return nil
}

func ownerIssueBody(m OwnerIssue) map[string]any {
	b := map[string]any{"issue": m.Issue, "title": m.Title, "kinds": m.Kinds, "action": m.Action, "detail": m.Detail,
		"status": m.Status, "evidence": m.Evidence, "times": m.Times,
		"first_at": m.FirstAt.UTC().Format(time.RFC3339), "last_at": m.LastAt.UTC().Format(time.RFC3339)}
	for k, v := range map[string]string{"to": m.To, "law_ref": m.LawRef, "gap": m.Gap} {
		if v != "" {
			b[k] = v
		}
	}
	if len(m.Quotes) > 0 {
		b["quotes"] = m.Quotes
	}
	if m.LawInForceAt != nil {
		b["law_in_force_at"] = m.LawInForceAt.UTC().Format(time.RFC3339)
	}
	if m.Round > 0 {
		b["round"] = m.Round
	}
	return b
}

// sameIssue says whether a recorded body says what b says (its version aside).
func sameIssue(recorded, b map[string]any) bool {
	strip := func(m map[string]any) string {
		c := map[string]any{}
		for k, v := range m {
			if k != "version" {
				c[k] = v
			}
		}
		raw, _ := json.Marshal(c)
		return string(raw)
	}
	return strip(recorded) == strip(b)
}

// RecordOwnerIssues records each issue whose content changed as its next
// version and says how many it recorded; an issue posted as it stands changes
// nothing, so the legislation line may post the whole list again. A thread
// writing for the first time is registered, as on every write route.
func RecordOwnerIssues(ctx context.Context, db eventlog.DB, w actor.Writer, issues []OwnerIssue) (int, error) {
	if len(issues) == 0 || len(issues) > 500 {
		return 0, eventlog.Invalid("1 to 500 issues a call")
	}
	seen := map[string]bool{}
	for _, m := range issues {
		if err := checkOwnerIssue(m); err != nil {
			return 0, err
		}
		if seen[m.Issue] {
			return 0, eventlog.Invalid("%s is named twice", m.Issue)
		}
		seen[m.Issue] = true
	}
	recorded := 0
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		recorded = 0
		if err := actor.Ensure(ctx, tx, w, true); err != nil {
			return err
		}
		for _, m := range issues {
			body := ownerIssueBody(m)
			last, ok, err := eventlog.Last(ctx, tx, OwnerIssueStream(m.Issue))
			if err != nil {
				return err
			}
			version := 1
			if ok {
				if sameIssue(last.Body, body) {
					continue
				}
				v, _ := strconv.Atoi(fmt.Sprint(last.Body["version"]))
				version = v + 1
			}
			body["version"] = version
			if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: OwnerIssueStream(m.Issue), Type: "owner_issue", Actor: w.Actor, Body: body}); err != nil {
				return err
			}
			recorded++
		}
		return nil
	})
	return recorded, err
}

func issueFromBody(b map[string]any) OwnerIssue {
	s := func(k string) string { v, _ := b[k].(string); return v }
	list := func(k string) []string {
		var out []string
		if l, ok := b[k].([]any); ok {
			for _, e := range l {
				if v, ok := e.(string); ok {
					out = append(out, v)
				}
			}
		}
		return out
	}
	num := func(k string) int { n, _ := strconv.Atoi(fmt.Sprint(b[k])); return n }
	at := func(k string) time.Time { t, _ := time.Parse(time.RFC3339, s(k)); return t }
	m := OwnerIssue{Issue: s("issue"), Title: s("title"), Kinds: list("kinds"), Action: s("action"), Detail: s("detail"),
		To: s("to"), LawRef: s("law_ref"), Gap: s("gap"), Status: s("status"), Evidence: list("evidence"), Quotes: list("quotes"),
		Times: num("times"), FirstAt: at("first_at"), LastAt: at("last_at"), Round: num("round")}
	if t := at("law_in_force_at"); !t.IsZero() {
		m.LawInForceAt = &t
	}
	return m
}

// OwnerIssues reads every issue as it stands, the most often said first, with
// whether its pain came back after the law that answers it.
func OwnerIssues(ctx context.Context, db eventlog.DB) ([]OwnerIssueRead, error) {
	var out []OwnerIssueRead
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := eventlog.OfType(ctx, tx, "owner_issue", time.Time{}, time.Now().Add(time.Hour))
		if err != nil {
			return err
		}
		last := map[string]eventlog.Row{}
		for _, r := range rows {
			if l, ok := last[r.Stream]; !ok || r.Seq > l.Seq {
				last[r.Stream] = r
			}
		}
		for _, r := range last {
			v, _ := strconv.Atoi(fmt.Sprint(r.Body["version"]))
			out = append(out, OwnerIssueRead{OwnerIssue: issueFromBody(r.Body), Version: v, By: r.Actor, At: r.At})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := countSaidAfterLaw(ctx, db, out); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Times != out[j].Times {
			return out[i].Times > out[j].Times
		}
		return issueNumber(out[i].Issue) < issueNumber(out[j].Issue)
	})
	return out, nil
}

func issueNumber(code string) int { n, _ := strconv.Atoi(strings.TrimPrefix(code, "ISSUE-")); return n }

// countSaidAfterLaw fills SaidAfterLaw for the issues a law answers, from
// what the Owner said since the earliest of those laws.
func countSaidAfterLaw(ctx context.Context, db eventlog.DB, issues []OwnerIssueRead) error {
	var since *time.Time
	for _, m := range issues {
		if m.LawInForceAt != nil && (since == nil || m.LawInForceAt.Before(*since)) {
			since = m.LawInForceAt
		}
	}
	if since == nil {
		return nil
	}
	said := map[string]time.Time{}
	from := *since
	for {
		page, err := OwnerSaidSince(ctx, db, from, 1000)
		if err != nil {
			return err
		}
		fresh := 0
		for _, s := range page {
			if _, ok := said[s.MessageID]; !ok {
				said[s.MessageID] = s.SaidAt
				fresh++
			}
		}
		if len(page) < 1000 || fresh == 0 {
			break
		}
		from = page[len(page)-1].SaidAt
	}
	for i, m := range issues {
		if m.LawInForceAt == nil {
			continue
		}
		n := 0
		for _, id := range m.Evidence {
			if at, ok := said[id]; ok && at.After(*m.LawInForceAt) {
				n++
			}
		}
		issues[i].SaidAfterLaw = &n
	}
	return nil
}
