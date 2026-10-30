package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

// Serial: isolated question service environment.
// The production question service persists the authenticated principal's name.
// No notification leaves the process; no listener or database is used.
func TestStgCardsOwnerPrincipalRoundTrip(t *testing.T) {
	t.Setenv("APPROVAL_NOTIFY_URL", "")
	t.Setenv("OPS_VIEWER_ENV", "prod") // isolated service fixture, as questions.TestMain
	for _, principal := range []string{"owner", "admin"} {
		t.Run(principal, func(t *testing.T) {
			h := stgExhausted(t)
			h.block("budget_exhausted", stgTestMember())
			tr := stgTriggers(h.state())[0]
			c := stgTestCard(tr)
			svc := questions.New(filepath.Join(t.TempDir(), "questions"), nil)
			svc.SetClock(func() time.Time { return h.at })
			req := LandingCardRequest(c.Repo, tr, stgCardTitle(tr.Kind))
			var opts []map[string]string
			for _, label := range req["options"].([]any) {
				opts = append(opts, map[string]string{"label": label.(string)})
			}
			req["options"], req["idempotency_key"] = opts, strings.Repeat("a", 32)
			raw, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			made := svc.Create(ctx, "agentrt", "agentrt", "operator", raw)
			if made.Code != http.StatusCreated {
				t.Fatalf("create: %+v", made)
			}
			c.QuestionID = made.Body.(map[string]any)["id"].(string)
			answered := svc.Answer(ctx, "owner-console", principal, "admin", c.QuestionID, []byte(`{"option":"a","channel":"console"}`))
			if answered.Code != http.StatusOK {
				t.Fatalf("answer: %+v", answered)
			}
			read := svc.Get(ctx, "agentrt", "agentrt", "operator", c.QuestionID)
			if read.Code != http.StatusOK {
				t.Fatalf("get: %+v", read)
			}
			q := read.Body.(questions.Question)
			if q.Answer == nil || q.Answer.Approval {
				t.Fatal("answer missing or became approval")
			}
			fact := Question{ID: q.ID, Status: q.Status, Option: q.Answer.Option, Channel: q.Answer.Channel, AnsweredBy: q.Answer.By, OwnerAnswer: true, AnsweredAt: q.Answer.At.Format(time.RFC3339Nano)}
			outcome, records := stgCardDecision(h.state(), c, fact, q.Answer.At)
			if outcome["outcome"] != "applied" || len(records) != 2 {
				t.Fatalf("valid Console admin answer persisted by=%q but decision=%v/%v records=%d; want applied/release_granted and two ledger records", fact.AnsweredBy, outcome["outcome"], outcome["why"], len(records))
			}
		})
	}
}

type stgCardFenceRows struct {
	pgx.Rows
	values [][]any
	i      int
}

func (r *stgCardFenceRows) Next() bool { r.i++; return r.i <= len(r.values) }
func (*stgCardFenceRows) Close()       {}
func (*stgCardFenceRows) Err() error   { return nil }
func (r *stgCardFenceRows) Scan(dest ...any) error {
	return grantIdentityAssign(dest, r.values[r.i-1]...)
}

// Only the SQL transport is doubled. A tightening commit is scheduled exactly
// after SettleLandingCard's first transaction releases its policy snapshot.
type stgCardFenceTx struct {
	stgGateReadTx
	outcome           map[string]any
	mode              string
	switchAfterRead   bool
	switchAfterLookup bool
	commits           int
}

func (tx *stgCardFenceTx) setRule(mode string) {
	tx.mode = mode
	tx.text = fmt.Sprintf("rule_id: landing:dev\nrule_version: 1\ndirection: initial\neffective_at: %s\nexpires_at: %s\ntenant: dev\nrepos:\n  - repo: bifrost-platform\n    allow: [\"console/src/components/**/*.tsx\"]\n    deny: []\n    stg_release: %s\nlimits: {max_files: 10, max_changed_lines: 300, max_landings_per_day: 5, max_in_flight_per_repo: 1}\n", tx.at.Add(-time.Hour).Format(time.RFC3339), tx.at.Add(time.Hour).Format(time.RFC3339), mode)
	var err error
	tx.rule, err = rules.Parse([]byte(tx.text))
	if err != nil {
		tx.t.Fatal(err)
	}
	tx.body, err = tx.rule.BodyJSON()
	if err != nil {
		tx.t.Fatal(err)
	}
}
func (tx *stgCardFenceTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	if !strings.Contains(sql, "pg_advisory_xact_lock_shared") {
		tx.t.Fatalf("unexpected write: %s", sql)
	}
	return pgconn.CommandTag{}, nil
}
func (tx *stgCardFenceTx) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }
func (tx *stgCardFenceTx) Commit(context.Context) error {
	tx.commits++
	if tx.switchAfterRead && tx.commits == 1 || tx.switchAfterLookup && tx.commits == 2 {
		tx.setRule("none")
	}
	return nil
}
func (*stgCardFenceTx) Rollback(context.Context) error { return nil }
func (tx *stgCardFenceTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	if tx.outcome == nil && strings.Contains(sql, "FROM agentrt.event") {
		return &stgCardFenceRows{}, nil // open question, resolved ledger trigger
	}
	if !strings.Contains(sql, "SELECT body_json->'landing_answer'") || len(args) != 2 || args[0] != StgReleaseStream {
		tx.t.Fatalf("unexpected SQL: %s %v", sql, args)
	}
	return &stgCardFenceRows{values: [][]any{{map[string]any(nil), tx.outcome, "controller", ""}}}, nil
}

// Serial: event clock; resolved-trigger control and tightening at the last
// read commit before Withdraw. Only SQL transport and the question writer
// are doubled; production policy, trigger reads and settlement run intact.
func TestStgCardsNoneFenceBeforeWithdraw(t *testing.T) {
	h := stgReady(t, stgTestMember())
	h.block("ci_red", stgTestMember())
	c := stgTestCard(stgTriggers(h.state())[0])
	defer FreezeEventClock(h.at)()
	for _, tc := range []struct {
		name, mode string
		change     bool
		want       int
	}{
		{"enabled_control", "bifrost-deliver-platform", false, 1},
		{"none_control", "none", false, 0},
		{"none_after_lookup", "bifrost-deliver-platform", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &stgCardFenceTx{stgGateReadTx: stgGateReadTx{t: t, at: h.at}, switchAfterLookup: tc.change}
			tx.setRule(tc.mode)
			calls := 0
			stop := errors.New("stop after observing Withdraw")
			_, err := SettleLandingCard(context.Background(), tx, CardQuestions{
				Lookup: func(context.Context, string) (Question, bool, error) {
					return Question{ID: c.QuestionID, Status: "open"}, true, nil
				},
				Withdraw: func(context.Context, string, string) (string, error) { calls++; return "", stop },
			}, c, h.at)
			if err != nil && !errors.Is(err, stop) {
				t.Fatal(err)
			}
			if calls != tc.want {
				t.Fatalf("policy=%s Withdraw calls=%d; want %d", tx.mode, calls, tc.want)
			}
		})
	}
}

// Serial: FreezeEventClock. Positive and disabled controls use the same entry.
func TestStgCardsNoneFenceBeforeAck(t *testing.T) {
	h := stgReady(t, stgTestMember())
	h.block("ci_red", stgTestMember())
	c := stgTestCard(stgTriggers(h.state())[0])
	body, _ := stgCardDecision(h.state(), c, stgAnswer(c, "a"), h.at)
	defer FreezeEventClock(h.at)()
	for _, tc := range []struct {
		name, mode string
		change     bool
		want       int
	}{
		{"enabled_control", "bifrost-deliver-platform", false, 1},
		{"none_control", "none", false, 0},
		{"none_after_policy_read", "bifrost-deliver-platform", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &stgCardFenceTx{stgGateReadTx: stgGateReadTx{t: t, at: h.at}, outcome: body, switchAfterRead: tc.change}
			tx.setRule(tc.mode)
			acks := 0
			stop := errors.New("probe stops after observing Ack invocation")
			_, err := SettleLandingCard(context.Background(), tx, CardQuestions{Ack: func(context.Context, string) (bool, error) {
				acks++
				return false, stop
			}}, c, h.at)
			if err != nil && !errors.Is(err, stop) {
				t.Fatal(err)
			}
			if acks != tc.want {
				t.Fatalf("policy=%s Ack calls=%d; want %d (none must not invoke the question writer)", tx.mode, acks, tc.want)
			}
		})
	}
}

func TestStgCardsOwnerReceiptTrustAndLegacy(t *testing.T) {
	t.Parallel()
	for _, letter := range []string{"a", "b"} {
		for _, principal := range []string{"owner", "admin", "renamed-administrator"} {
			t.Run(principal+letter, func(t *testing.T) {
				h := stgExhausted(t)
				h.block("budget_exhausted", stgTestMember())
				c := stgTestCard(stgTriggers(h.state())[0])
				q := stgAnswer(c, letter)
				q.AnsweredBy = principal
				body, records := stgCardDecision(h.state(), c, q, h.at)
				if body["outcome"] != "applied" || body["by"] != principal || body["owner_answer"] != true || len(records) != 2 || badCardOutcome(body) != "" {
					t.Fatal(body, records, badCardOutcome(body))
				}
				h.raw(map[string]any{"text": "answer", "landing_card_outcome": body})
				for _, r := range records {
					h.add(map[string]any(r))
				}
				if len(h.state().Grants) != 1 {
					t.Fatal("authorized receipt did not replay")
				}
				legacy := stgClone(body)
				delete(legacy, "owner_answer")
				legacy["by"] = "owner"
				if badCardOutcome(legacy) != "" {
					t.Fatal("legacy Owner receipt no longer valid", badCardOutcome(legacy))
				}
				legacy["by"] = "admin"
				if badCardOutcome(legacy) == "" {
					t.Fatal("unmarked arbitrary principal became Owner")
				}
				for _, proof := range []any{false, "true", nil} {
					bad := stgClone(body)
					bad["by"] = "owner"
					bad["owner_answer"] = proof
					if badCardOutcome(bad) == "" {
						t.Fatal("untrusted applied receipt", proof)
					}
				}
			})
		}
	}
	h := stgExhausted(t)
	h.block("budget_exhausted", stgTestMember())
	c := stgTestCard(stgTriggers(h.state())[0])
	for _, by := range []string{"owner", "admin", "agent"} {
		q := stgAnswer(c, "a")
		q.AnsweredBy = by
		q.OwnerAnswer = false
		body, records := stgCardDecision(h.state(), c, q, h.at)
		if body["outcome"] != "refused" || body["why"] != "actor" || len(records) != 0 || badCardOutcome(body) != "" {
			t.Fatal("name authorized answer", body, records)
		}
	}
	for _, channel := range []string{"rule", "mcp"} {
		q := stgAnswer(c, "a")
		q.Channel = channel
		body, records := stgCardDecision(h.state(), c, q, h.at)
		if body["outcome"] != "refused" || body["why"] != "channel" || len(records) != 0 || badCardOutcome(body) != "" {
			t.Fatal("wrong source authorized answer", body, records)
		}
	}
}
