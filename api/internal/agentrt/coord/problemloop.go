package coord

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/problemloop"
)

// The problem loop (2026-11-18, LAW-8): a problem is sent (a letter of
// a row with params.ask_verdict), seen (the receiver acknowledged it), judged
// (a verdict), then handled or fixed at the source. A verdict is a message,
// as a seat's status is (seatstatus.go): a typed envelope of type
// "problem_verdict" sent with the existing message route to the board, its
// text the verdict as JSON, checked here on read. No table, event type or
// write route is new, and a verdict is not a status, so the status board and
// the seat record leave it alone (ParseSeatStatus answers ok false).

const (
	// MaxVerdictRef and MaxVerdictSummary bound the free text of a verdict, in bytes.
	MaxVerdictRef     = 200
	MaxVerdictSummary = 300
	// maxProblemRows bounds what one read takes from the messages.
	maxProblemRows = 5000
	// maxProblemCases bounds the cases listed; the counts cover all of them.
	maxProblemCases = 1000
)

var verdictEventRE = regexp.MustCompile(`^ev-[0-9]{1,18}$`)

// ProblemVerdict is what a seat says of one event.
type ProblemVerdict struct {
	Event   string `json:"event"`
	Verdict string `json:"verdict"`
	Outcome string `json:"outcome"`
	Ref     string `json:"ref,omitempty"`
}

func boundedText(s string, max int) bool {
	return len(s) <= max && utf8.ValidString(s) && !strings.ContainsAny(s, "\x00\r\n")
}

// CheckProblemVerdict says why v is not a verdict ("" when it is one).
func CheckProblemVerdict(v ProblemVerdict) string {
	switch {
	case !verdictEventRE.MatchString(v.Event):
		return "event is not an event id (ev-<n>)"
	case v.Verdict != problemloop.VerdictReal && v.Verdict != problemloop.VerdictFalse:
		return "verdict is real or false"
	case v.Outcome != problemloop.OutcomeHandled && v.Outcome != problemloop.OutcomeSourceFixed && v.Outcome != problemloop.OutcomeOpen:
		return "outcome is handled, source_fixed or open"
	case !boundedText(v.Ref, MaxVerdictRef):
		return "ref is one line of UTF-8 text of at most 200 bytes"
	}
	return ""
}

// ParseProblemVerdict reads a verdict message's body: the envelope, then its
// text. A body that is a message but not a verdict answers ok false.
func ParseProblemVerdict(body []byte) (summary string, v ProblemVerdict, problem string, ok bool) {
	var env struct {
		V       int    `json:"v"`
		Type    string `json:"type"`
		Summary string `json:"summary"`
		Text    string `json:"text"`
	}
	if json.Unmarshal(body, &env) != nil || env.Type != problemloop.VerdictType {
		return "", ProblemVerdict{}, "", false
	}
	if !boundedText(env.Summary, MaxVerdictSummary) {
		return "", ProblemVerdict{}, "format: summary is one line of UTF-8 text of at most 300 bytes", true
	}
	if env.V != 1 {
		return env.Summary, ProblemVerdict{}, "format: v is 1", true
	}
	dec := json.NewDecoder(strings.NewReader(env.Text))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return env.Summary, ProblemVerdict{}, "format: the text is not the verdict fields (" + err.Error() + ")", true
	}
	if dec.More() {
		return env.Summary, ProblemVerdict{}, "format: the text has more than the verdict", true
	}
	if p := CheckProblemVerdict(v); p != "" {
		return env.Summary, ProblemVerdict{}, "format: " + p, true
	}
	return env.Summary, v, "", true
}

// ProblemCase is one letter of a problem: what was sent, whether it was seen,
// and the latest valid verdict on its event.
type ProblemCase struct {
	Kind      string     `json:"kind"`
	Subject   string     `json:"subject"`
	Event     string     `json:"event"`
	Row       string     `json:"row"`
	Receiver  string     `json:"receiver"`
	Letter    time.Time  `json:"letter_at"`
	Acked     *time.Time `json:"acked_at"`
	Verdict   string     `json:"verdict"`
	Outcome   string     `json:"outcome"`
	Ref       string     `json:"ref"`
	VerdictBy string     `json:"verdict_by"`
	VerdictAt *time.Time `json:"verdict_at"`
	Closed    bool       `json:"closed"`
	// State is where the case stands: closed, judged (a verdict, not closed),
	// seen (acknowledged, no verdict) or sent.
	State string `json:"state"`
}

// InvalidVerdict is a verdict message that did not pass; it counts for nothing.
type InvalidVerdict struct {
	MessageID string    `json:"message_id"`
	Sender    string    `json:"sender"`
	At        time.Time `json:"at"`
	Summary   string    `json:"summary,omitempty"`
	Problem   string    `json:"problem"`
}

// ProblemCounts are computed from the cases: raised is the letters, seen the
// acknowledged ones, judged those with a verdict, closed a real problem
// handled or fixed at the source, or a false one fixed at the source, open the rest.
type ProblemCounts struct {
	Raised               int      `json:"raised"`
	Seen                 int      `json:"seen"`
	Judged               int      `json:"judged"`
	Real                 int      `json:"real"`
	False                int      `json:"false"`
	Closed               int      `json:"closed"`
	Open                 int      `json:"open"`
	AckMedianMinutes     *float64 `json:"ack_median_minutes"`
	VerdictMedianMinutes *float64 `json:"verdict_median_minutes"`
	// ClosedMedianMinutes is letter to the verdict that closed it, over the
	// closed cases; nil when none is closed (2026-11-19, the coordinator: the
	// loop showed time to verdict but not time to handled).
	ClosedMedianMinutes *float64 `json:"closed_median_minutes"`
}

// ProblemReport is what GET /api/v1/bus/problems answers.
type ProblemReport struct {
	Since   time.Time        `json:"since"`
	Counts  ProblemCounts    `json:"counts"`
	Cases   []ProblemCase    `json:"cases"`
	Invalid []InvalidVerdict `json:"invalid"`
}

// problemLetterRE reads a wake's body (subscribe.Body): type, subject, the
// row and the event.
var problemLetterRE = regexp.MustCompile(`^bus: (\S+) (.+?) \(change [^)]*\)\. Read .*? \[subscription ([a-z0-9-]{1,63}), event (ev-[0-9]{1,18})\]`)

// isDrill says a subject is a drill's (bus/drill): not a real event.
func isDrill(subject string) bool {
	return strings.HasPrefix(subject, "drill/") || strings.HasPrefix(subject, "drill-") || strings.HasSuffix(subject, "@drill")
}

// ProblemLoop reads the problem letters sent since `since` (those of the rows
// in kinds: row id to kind) and the verdicts given since. The latest valid
// verdict on an event wins and keeps who gave it and when; a verdict that
// does not parse is listed as invalid with its sender and never counted. A
// repeat-sharing alert letter is one letter, so one case; drills are left out.
func ProblemLoop(ctx context.Context, db eventlog.DB, since time.Time, kinds map[string]string) (ProblemReport, error) {
	out := ProblemReport{Since: since, Cases: []ProblemCase{}, Invalid: []InvalidVerdict{}}
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		out.Cases, out.Invalid = []ProblemCase{}, []InvalidVerdict{}
		verdicts := map[string]ProblemCase{}
		vrows, err := tx.Query(ctx, `SELECT m.message_id, m.from_actor_id, b.body, m.created_at
		  FROM agentrt.message m JOIN agentrt.artifact_blob b ON b.artifact_id = m.body_artifact_id
		  WHERE m.to_actor_id = $1 AND m.message_kind = 'note' AND m.created_at >= $2
		  ORDER BY m.created_at DESC, m.message_id DESC LIMIT $3`, StatusBoard, since, maxProblemRows)
		if err != nil {
			return err
		}
		defer vrows.Close()
		for vrows.Next() {
			var id, from string
			var body []byte
			var at time.Time
			if err := vrows.Scan(&id, &from, &body, &at); err != nil {
				return err
			}
			summary, v, problem, ok := ParseProblemVerdict(body)
			if !ok {
				continue
			}
			if problem != "" {
				out.Invalid = append(out.Invalid, InvalidVerdict{MessageID: id, Sender: from, At: at, Summary: summary, Problem: problem})
				continue
			}
			if _, have := verdicts[v.Event]; have {
				continue // newest first: the latest valid one is already here
			}
			t := at
			verdicts[v.Event] = ProblemCase{Verdict: v.Verdict, Outcome: v.Outcome, Ref: v.Ref, VerdictBy: from, VerdictAt: &t}
		}
		if err := vrows.Err(); err != nil {
			return err
		}
		vrows.Close()

		lrows, err := tx.Query(ctx, `SELECT m.to_actor_id, b.body, m.created_at, m.acked_at
		  FROM agentrt.message m JOIN agentrt.artifact_blob b ON b.artifact_id = m.body_artifact_id
		  WHERE m.from_actor_id = 'controller' AND m.created_at >= $1
		    AND substring(b.body from 1 for octet_length(convert_to($2, 'UTF8'))) = convert_to($2, 'UTF8')
		  ORDER BY m.created_at DESC, m.message_id DESC LIMIT $3`, since, WakePrefix, maxProblemRows)
		if err != nil {
			return err
		}
		defer lrows.Close()
		for lrows.Next() {
			var to string
			var body []byte
			var at time.Time
			var acked *time.Time
			if err := lrows.Scan(&to, &body, &at, &acked); err != nil {
				return err
			}
			if strings.HasPrefix(string(body), UndeliverablePrefix) {
				continue // a rerouted notice, not the letter itself
			}
			// Only a letter that asked for a verdict is a case: those sent before
			// the loop existed could not be answered and would stay open for good.
			if !strings.Contains(string(body), problemloop.VerdictType) {
				continue
			}
			m := problemLetterRE.FindStringSubmatch(string(body))
			if m == nil || isDrill(m[2]) {
				continue
			}
			kind := kinds[m[3]]
			if kind == "" {
				continue
			}
			c := ProblemCase{Kind: kind, Subject: m[2], Event: m[4], Row: m[3], Receiver: to, Letter: at, Acked: acked}
			if v, ok := verdicts[c.Event]; ok {
				c.Verdict, c.Outcome, c.Ref, c.VerdictBy, c.VerdictAt = v.Verdict, v.Outcome, v.Ref, v.VerdictBy, v.VerdictAt
			}
			out.Cases = append(out.Cases, c)
		}
		return lrows.Err()
	})
	if err != nil {
		return ProblemReport{}, err
	}
	sort.SliceStable(out.Invalid, func(i, j int) bool { return out.Invalid[i].At.After(out.Invalid[j].At) })
	out.Counts = countProblems(out.Cases)
	if len(out.Cases) > maxProblemCases {
		out.Cases = out.Cases[:maxProblemCases]
	}
	return out, nil
}

// countProblems fills each case's Closed and State and counts the cases.
func countProblems(cases []ProblemCase) ProblemCounts {
	var n ProblemCounts
	var acks, verdicts, closes []float64
	for i := range cases {
		c := &cases[i]
		n.Raised++
		if c.Acked != nil {
			n.Seen++
			acks = append(acks, c.Acked.Sub(c.Letter).Minutes())
		}
		if c.Verdict != "" {
			n.Judged++
			if c.VerdictAt != nil {
				verdicts = append(verdicts, c.VerdictAt.Sub(c.Letter).Minutes())
			}
		}
		switch c.Verdict {
		case problemloop.VerdictReal:
			n.Real++
		case problemloop.VerdictFalse:
			n.False++
		}
		// A real problem is closed when handled or fixed at its source; a false
		// one only when its source is fixed (2026-11-19: real+source_fixed was
		// counted open, though fixing the source is the better end).
		c.Closed = c.Verdict == problemloop.VerdictReal && (c.Outcome == problemloop.OutcomeHandled || c.Outcome == problemloop.OutcomeSourceFixed) ||
			c.Verdict == problemloop.VerdictFalse && c.Outcome == problemloop.OutcomeSourceFixed
		switch {
		case c.Closed:
			n.Closed++
			c.State = "closed"
			if c.VerdictAt != nil {
				closes = append(closes, c.VerdictAt.Sub(c.Letter).Minutes())
			}
		case c.Verdict != "":
			c.State = "judged"
		case c.Acked != nil:
			c.State = "seen"
		default:
			c.State = "sent"
		}
	}
	n.Open = n.Raised - n.Closed
	if len(acks) > 0 {
		m := median(acks)
		n.AckMedianMinutes = &m
	}
	if len(verdicts) > 0 {
		m := median(verdicts)
		n.VerdictMedianMinutes = &m
	}
	if len(closes) > 0 {
		m := median(closes)
		n.ClosedMedianMinutes = &m
	}
	return n
}
