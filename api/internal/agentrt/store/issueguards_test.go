package store_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// guardStep is one event and the statements written under it.
type guardStep struct {
	ev    store.Event
	stmts []string
}

// ie is an event on an issue's stream naming the issue.
func ie(issue, typ, actor, mission string, body map[string]any) store.Event {
	body["issue_id"] = issue
	return store.Event{Stream: store.IssueStream(issue), Type: typ, Actor: actor, MissionID: mission, Body: body}
}

// run writes the steps in one transaction as the runtime role; an event the
// store itself refuses is the case's own mistake, not the guard's refusal.
func (iw *issueWorld) run(t *testing.T, steps ...guardStep) error {
	t.Helper()
	var appendErr error
	err := store.InTx(ctx, iw.rt, func(tx pgx.Tx) error {
		for _, s := range steps {
			if _, err := store.Append(ctx, tx, s.ev); err != nil {
				appendErr = err
				return err
			}
			for _, q := range s.stmts {
				if _, err := tx.Exec(ctx, q); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if appendErr != nil {
		t.Fatalf("the case's event does not append: %v", appendErr)
	}
	return err
}

func (iw *issueWorld) textOf(t *testing.T, issue, kind string) int64 {
	t.Helper()
	var id int64
	ok(t, iw.rt.QueryRow(ctx, `SELECT max(issue_text_id) FROM agentrt.issue_text WHERE issue_id = $1 AND text_kind = $2`, issue, kind).Scan(&id))
	return id
}

// B1 (review 522): every guard branch 0009 adds refuses its minimal
// counterexample, written past the store as the runtime role with the event
// type the table takes, violating only the branch's own condition. Each case
// runs in its own transaction and rolls back, so they share one fixture:
//
//	A  her issue, taken (mission mA), its trial asked and open
//	B  her issue, briefed twice: askB0 superseded, askB open
//	C  her issue, ready (small), designed, reviewed once (pass)
//	D  her issue, declined by the Owner (closed)
//	E  her issue, open, untaken
//	P  her agent's own proposal, briefed (askP open)
func TestIssueGuardMatrix(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	A, B, C, D, E := iw.submit(t, "a"), iw.submit(t, "b"), iw.submit(t, "c"), iw.submit(t, "d"), iw.submit(t, "e")
	iw.readySmall(t, A, "improvement")
	taken, err := iw.take(t, A, fitting)
	ok(t, err)
	mA := taken["mission_id"].(string)
	askT, err := iw.trial(t, A, 1, mA)
	ok(t, err)
	askB0 := iw.brief(t, B, "brief b0")
	askB := iw.brief(t, B, "brief b1")
	iw.readySmall(t, C, "improvement")
	ok(t, iw.design(t, C, "design c"))
	ok(t, iw.review(t, C, "通过", 1, "codex"))
	ok(t, iw.close(t, iw.owner, D, store.ConclusionDeclined, 0, ""))
	prop, err := store.SubmitIssue(ctx, iw.rt, iw.dots, iw.key(), store.IssueSubmission{Product: "screening", Kind: "proposal", Title: "p",
		OnBehalfOf: store.SelfIssue, Proposal: "an idea"})
	ok(t, err)
	P := prop["issue_id"].(string)
	askP := iw.brief(t, P, "brief p")
	bB, bC := iw.state(t, B).BriefSHA256, iw.state(t, C)
	textB, wordsB, briefC := iw.textOf(t, B, "brief"), iw.textOf(t, B, "words"), iw.textOf(t, C, "brief")
	notBrief := iw.report(t, "implement")
	zeros := strings.Repeat("0", 64)
	const crew, dots = "thread:crew-1", "agent:dots-p2"
	upd := func(issue, set string) string {
		return `UPDATE agentrt.issue SET ` + set + `, version = version + 1 WHERE issue_id = '` + issue + `'`
	}
	answerB := func(actor, answeredBy string) []string { // her response, then the answer to askB
		return []string{textSQL(9701, B, "response", "person:p2", "intake_local", "yes", actor),
			`UPDATE agentrt.issue_ask SET verdict = 'confirmed', words_text_id = 9701, answered_by_actor_id = '` + answeredBy +
				`', answer_channel = 'intake_local', answered_at = now() WHERE issue_ask_id = ` + itoa(askB)}
	}
	answered := func(ask int64, person, channel string) map[string]any {
		return map[string]any{"ask_id": ask, "text_ids": []any{int64(9701)}, "verdict": "confirmed", "channel": channel, "person": person}
	}
	newIssue := func(set string) string {
		return `INSERT INTO agentrt.issue (issue_id, product, kind, title, served_person, asks_person, state, waiting_on, created_by_actor_id` + set
	}
	for _, c := range []struct {
		name  string
		steps []guardStep
		want  string
	}{
		// guard_issue
		{"an agent records a classification", []guardStep{{ie(B, "issue_classified", dots, "", map[string]any{"check_id": 1, "risk_class": "small",
			"category": "fault", "brief_sha256": bB, "seat_report": iw.seat}), []string{upd(B, "waiting_on = 'crew'")}}},
			"does not write a issue_classified event"},
		{"an event on another issue's stream", []guardStep{{store.Event{Stream: store.IssueStream(A), Type: "issue_replied", Actor: crew,
			Body: map[string]any{"issue_id": B, "text_id": 1}}, []string{upd(B, "waiting_on = 'crew'")}}}, "is not about issue " + B},
		{"an issue created by another event", []guardStep{{ie("BI-990", "issue_added", crew, "", map[string]any{"channel": "thread",
			"text_ids": []any{int64(1)}}), []string{newIssue(`) VALUES ('BI-990', 'screening', 'complaint', 't', 'person:owner', 'person:owner',
			'clarifying', 'crew', '` + crew + `')`)}}}, "created by issue_submitted, not issue_added"},
		{"an issue created decided", []guardStep{{ie("BI-991", "issue_submitted", crew, "", map[string]any{"product": "screening",
			"kind": "complaint", "channel": "thread", "text_ids": []any{int64(1)}}), []string{newIssue(`, risk_class) VALUES ('BI-991', 'screening',
			'complaint', 't', 'person:owner', 'person:owner', 'clarifying', 'crew', '` + crew + `', 'small')`)}}}, "with nothing decided"},
		{"an issue created as another writer", []guardStep{{ie("BI-992", "issue_submitted", crew, "", map[string]any{"product": "screening",
			"kind": "complaint", "channel": "thread", "text_ids": []any{int64(1)}}), []string{newIssue(`) VALUES ('BI-992', 'screening', 'complaint',
			't', 'person:owner', 'person:owner', 'clarifying', 'crew', 'owner')`)}}}, "created by the writer of its event"},
		{"a title rewritten", []guardStep{{ie(B, "issue_state_changed", crew, "", map[string]any{"change": "resume"}),
			[]string{upd(B, "title = 'another'")}}}, "keeps its product, kind, title"},
		{"a class set by a reply", []guardStep{{ie(B, "issue_replied", crew, "", map[string]any{"text_id": 1}),
			[]string{upd(B, "risk_class = 'large'")}}}, "set by a classification or a merge"},
		{"a priority set by a reply", []guardStep{{ie(B, "issue_replied", crew, "", map[string]any{"text_id": 1}),
			[]string{upd(B, "priority = 'p1'")}}}, "set by issue_priority_set"},
		{"a brief that is her words", []guardStep{{ie(B, "issue_brief_drafted", crew, "", map[string]any{"text_id": wordsB,
			"brief_sha256": bB, "ask_id": 1}), []string{upd(B, "brief_text_id = "+itoa(wordsB))}}}, "changes only to a brief of its own"},
		{"a brief changed under live work", []guardStep{{ie(A, "issue_brief_drafted", crew, "", map[string]any{"text_id": 9702,
			"brief_sha256": zeros, "ask_id": 1}), []string{textSQL(9702, A, "brief", "", "thread", "new scope", crew),
			upd(A, "brief_text_id = 9702, brief_confirmed_ask = NULL")}}}, "while its work is live"},
		{"a design that is a brief", []guardStep{{ie(C, "issue_design_put", crew, "", map[string]any{"text_id": briefC,
			"design_sha256": bC.BriefSHA256}), []string{upd(C, "design_text_id = "+itoa(briefC))}}}, "changes only to a design of its own"},
		{"a confirmation by an open ask", []guardStep{{ie(B, "issue_answered", dots, "", answered(askB, "person:p2", "intake_local")),
			[]string{upd(B, "brief_confirmed_ask = "+itoa(askB))}}}, "an ask that did not confirm this brief"},
		{"a confirmation cleared by a reply", []guardStep{{ie(C, "issue_replied", crew, "", map[string]any{"text_id": 1}),
			[]string{upd(C, "brief_confirmed_ask = NULL")}}}, "cleared only by a new brief"},
		{"active by a reply", []guardStep{{ie(B, "issue_replied", crew, "", map[string]any{"text_id": 1}),
			[]string{upd(B, "state = 'active'")}}}, "becomes active only by take_issue"},
		{"taken and not active", []guardStep{{ie(C, "issue_taken", crew, "", map[string]any{"commitment": 1, "brief_sha256": bC.BriefSHA256,
			"rule_id": "intake-gates", "rule_version": 1, "risk_class": "small", "mission_state": "active", "live": 0, "limit": 1}),
			[]string{upd(C, "waiting_on = 'crew'")}}}, "a taken issue is active"},
		{"closed twice", []guardStep{{ie(D, "issue_state_changed", crew, "", map[string]any{"change": "close"}),
			[]string{upd(D, "conclusion = 'resolved'")}}}, "reopened before it is closed again"},
		{"merged by a state change", []guardStep{{ie(E, "issue_state_changed", crew, "", map[string]any{"change": "close"}),
			[]string{upd(E, "state = 'closed', conclusion = 'merged', duplicate_of = '"+B+"'")}}}, "merged by issue_merged"},
		{"merged into a closed issue", []guardStep{{ie(E, "issue_merged", crew, "", map[string]any{"into": D}),
			[]string{upd(E, "state = 'closed', conclusion = 'merged', duplicate_of = '"+D+"'")}}}, "merged into an open issue"},
		{"merged into a taken issue", []guardStep{{ie(E, "issue_merged", crew, "", map[string]any{"into": A}),
			[]string{upd(E, "state = 'closed', conclusion = 'merged', duplicate_of = '"+A+"'")}}}, "only issues no work was taken for"},
		// guard_issue_text
		{"a text by another writer", []guardStep{{ie(B, "issue_added", dots, "", map[string]any{"channel": "intake_local",
			"text_ids": []any{int64(9703)}}), []string{textSQL(9703, B, "reading", "", "intake_local", "r", crew)}}}, "written by the writer of its event"},
		{"a kind its event does not write", []guardStep{{ie(B, "issue_added", crew, "", map[string]any{"channel": "thread",
			"text_ids": []any{int64(9704)}}), []string{textSQL(9704, B, "brief", "", "thread", "r", crew)}}}, "writes no brief text"},
		{"a sha256 not of the body", []guardStep{{ie(B, "issue_added", crew, "", map[string]any{"channel": "thread",
			"text_ids": []any{int64(9705)}}), []string{`INSERT INTO agentrt.issue_text (issue_text_id, issue_id, text_kind, channel, body, sha256,
			created_by_actor_id) OVERRIDING SYSTEM VALUE VALUES (9705, '` + B + `', 'reading', 'thread', 'r', '` + zeros + `', '` + crew + `')`}}},
			"sha256 is its body's"},
		{"an agent on a thread's channel", []guardStep{{ie(B, "issue_added", dots, "", map[string]any{"channel": "thread",
			"text_ids": []any{int64(9706)}}), []string{textSQL(9706, B, "reading", "", "thread", "r", dots)}}}, "does not write reading on channel thread"},
		{"a thread's own words for her", []guardStep{{ie(B, "issue_added", crew, "", map[string]any{"channel": "thread",
			"text_ids": []any{int64(9707)}}), []string{textSQL(9707, B, "words", "person:p2", "thread", "w", crew)}}},
			"does not write words on channel thread for person:p2"},
		// guard_issue_ask
		{"a confirmation asked by issue_asked", []guardStep{{ie(B, "issue_asked", crew, "", map[string]any{"text_id": textB, "ask_id": 9708,
			"purpose": "confirm_brief"}), []string{fmt.Sprintf(`INSERT INTO agentrt.issue_ask (issue_ask_id, issue_id, person, purpose, bound_text_id,
			bound_sha256, brief_sha256) OVERRIDING SYSTEM VALUE VALUES (9708, '%s', 'person:p2', 'confirm_brief', %d, '%s', '%s')`, B, textB, bB, bB)}}},
			"every other ask by issue_asked"},
		{"an ask of another person", []guardStep{{ie(B, "issue_asked", crew, "", map[string]any{"text_id": textB, "ask_id": 9709,
			"purpose": "needs_info"}), []string{fmt.Sprintf(`INSERT INTO agentrt.issue_ask (issue_ask_id, issue_id, person, purpose, bound_text_id,
			bound_sha256, brief_sha256) OVERRIDING SYSTEM VALUE VALUES (9709, '%s', 'person:owner', 'needs_info', %d, '%s', '%s')`, B, textB, bB, bB)}}},
			"asks person:p2, not person:owner"},
		{"an ask born superseded", []guardStep{{ie(B, "issue_asked", crew, "", map[string]any{"text_id": textB, "ask_id": 9710,
			"purpose": "needs_info"}), []string{fmt.Sprintf(`INSERT INTO agentrt.issue_ask (issue_ask_id, issue_id, person, purpose, bound_text_id,
			bound_sha256, brief_sha256, superseded_at) OVERRIDING SYSTEM VALUE VALUES (9710, '%s', 'person:p2', 'needs_info', %d, '%s', '%s', now())`,
			B, textB, bB, bB)}}}, "a new ask is open"},
		{"an ask of another brief", []guardStep{{ie(B, "issue_asked", crew, "", map[string]any{"text_id": textB, "ask_id": 9711,
			"purpose": "check_answer"}), []string{fmt.Sprintf(`INSERT INTO agentrt.issue_ask (issue_ask_id, issue_id, person, purpose, bound_text_id,
			bound_sha256, brief_sha256) OVERRIDING SYSTEM VALUE VALUES (9711, '%s', 'person:p2', 'check_answer', %d, '%s', '%s')`, B, textB, bB, zeros)}}},
			"asked for its current brief"},
		{"what is asked rewritten", []guardStep{{ie(B, "issue_state_changed", crew, "", map[string]any{"change": "supersede",
			"ask_ids": []any{askB}}), []string{`UPDATE agentrt.issue_ask SET purpose = 'needs_info' WHERE issue_ask_id = ` + itoa(askB)}}},
			"never changes"},
		{"superseded twice", []guardStep{{ie(B, "issue_state_changed", crew, "", map[string]any{"change": "supersede",
			"ask_ids": []any{askB0}}), []string{`UPDATE agentrt.issue_ask SET superseded_at = now() WHERE issue_ask_id = ` + itoa(askB0)}}},
			"superseded once"},
		{"superseded by an answer", []guardStep{{ie(B, "issue_answered", dots, "", answered(askB, "person:p2", "intake_local")),
			[]string{`UPDATE agentrt.issue_ask SET superseded_at = now() WHERE issue_ask_id = ` + itoa(askB)}}}, "superseded once"},
		{"answered by a state change", []guardStep{
			{ie(B, "issue_answered", dots, "", answered(askB, "person:p2", "intake_local")), answerB(dots, dots)[:1]},
			{ie(B, "issue_state_changed", crew, "", map[string]any{"change": "resume", "ask_ids": []any{askB}}), answerB(dots, dots)[1:]}},
			"answered once, while open, by issue_answered"},
		{"answered as someone else", []guardStep{{ie(B, "issue_answered", dots, "", answered(askB, "person:p2", "intake_local")),
			answerB(dots, "agent:dots-owner")}}, "does not answer for person:p2"},
		{"a proposal confirmed by a thread", []guardStep{{ie(P, "issue_answered", crew, "", answered(askP, store.OwnerPerson, "thread")),
			[]string{textSQL(9701, P, "response", store.OwnerPerson, "thread", "yes", crew),
				`UPDATE agentrt.issue_ask SET verdict = 'confirmed', words_text_id = 9701, answered_by_actor_id = '` + crew +
					`', answer_channel = 'thread', answered_at = now() WHERE issue_ask_id = ` + itoa(askP)}}}, "by the Owner himself"},
		{"an answer that is not her response", []guardStep{{ie(B, "issue_answered", dots, "", answered(askB, "person:p2", "intake_local")),
			[]string{`UPDATE agentrt.issue_ask SET verdict = 'confirmed', words_text_id = ` + itoa(wordsB) + `, answered_by_actor_id = '` + dots +
				`', answer_channel = 'intake_local', answered_at = now() WHERE issue_ask_id = ` + itoa(askB)}}}, "the person's response"},
		{"withdrawn after the take", []guardStep{{ie(A, "issue_answered", dots, mA, map[string]any{"ask_id": askT, "text_ids": []any{int64(9701)},
			"verdict": "withdrawn", "channel": "intake_local", "person": "person:p2"}), []string{textSQL(9701, A, "response", "person:p2",
			"intake_local", "never mind", dots), `UPDATE agentrt.issue_ask SET verdict = 'withdrawn', words_text_id = 9701, answered_by_actor_id = '` +
			dots + `', answer_channel = 'intake_local', answered_at = now() WHERE issue_ask_id = ` + itoa(askT)}}}, "withdrawn only before it is taken"},
		// guard_issue_work
		{"work of an earlier brief", []guardStep{{ie(C, "issue_taken", crew, "M-1", map[string]any{"commitment": 1, "brief_sha256": zeros,
			"rule_id": "intake-gates", "rule_version": 1, "risk_class": "small", "mission_state": "active", "live": 0, "limit": 1}),
			[]string{`INSERT INTO agentrt.issue_work (issue_id, commitment, mission_id, brief_sha256) VALUES ('` + C + `', 1, 'M-1', '` + zeros + `')`}}},
			"taken for its current brief"},
		{"work of a mission not made for it", []guardStep{{ie(C, "issue_taken", crew, "M-1", map[string]any{"commitment": 1,
			"brief_sha256": bC.BriefSHA256, "rule_id": "intake-gates", "rule_version": 1, "risk_class": "small", "mission_state": "active",
			"live": 0, "limit": 1}), []string{`INSERT INTO agentrt.issue_work (issue_id, commitment, mission_id, brief_sha256) VALUES ('` + C +
			`', 1, 'M-1', '` + bC.BriefSHA256 + `')`}}}, "a live mission made for it"},
		{"work born ended", []guardStep{{ie(C, "issue_taken", crew, "M-1", map[string]any{"commitment": 1, "brief_sha256": bC.BriefSHA256,
			"rule_id": "intake-gates", "rule_version": 1, "risk_class": "small", "mission_state": "active", "live": 0, "limit": 1}),
			[]string{`INSERT INTO agentrt.issue_work (issue_id, commitment, mission_id, brief_sha256, ended_at) VALUES ('` + C + `', 1, 'M-1', '` +
				bC.BriefSHA256 + `', now())`}}}, "taken by issue_taken, live"},
		{"work renumbered", []guardStep{{ie(A, "issue_taken", crew, mA, map[string]any{"commitment": 2, "brief_sha256": zeros,
			"rule_id": "intake-gates", "rule_version": 1, "risk_class": "small", "mission_state": "active", "live": 0, "limit": 1}),
			[]string{`UPDATE agentrt.issue_work SET commitment = 2 WHERE mission_id = '` + mA + `'`}}}, "changes nothing but its end"},
		{"work ended while its mission lives", []guardStep{{store.Event{Stream: store.MissionStream(mA), Type: "mission_state_changed",
			Actor: "controller", MissionID: mA, Body: map[string]any{"from": "active", "to": "done"}},
			[]string{`UPDATE agentrt.issue_work SET ended_at = now() WHERE mission_id = '` + mA + `'`}}}, "ends only when its mission"},
		// guard_issue_check
		{"evidence of another kind", []guardStep{{ie(C, "issue_classified", crew, "", map[string]any{"check_id": 9712, "risk_class": "small",
			"category": "fault", "brief_sha256": bC.BriefSHA256, "seat_report": iw.seat}), []string{`INSERT INTO agentrt.issue_check (issue_check_id,
			issue_id, check_kind, brief_sha256, result, created_by_actor_id) OVERRIDING SYSTEM VALUE VALUES (9712, '` + C + `', 'priority', '` +
			bC.BriefSHA256 + `', 'p1', 'owner')`}}}, "records no priority evidence"},
		{"evidence by another writer", []guardStep{{ie(C, "issue_classified", crew, "", map[string]any{"check_id": 9713, "risk_class": "small",
			"category": "fault", "brief_sha256": bC.BriefSHA256, "seat_report": iw.seat}), []string{`INSERT INTO agentrt.issue_check (issue_check_id,
			issue_id, check_kind, brief_sha256, result, category, seat_artifact_id, created_by_actor_id) OVERRIDING SYSTEM VALUE VALUES (9713, '` + C +
			`', 'classification', '` + bC.BriefSHA256 + `', 'small', 'fault', '` + iw.seat + `', 'owner')`}}}, "recorded by the writer of its event"},
		{"evidence of an earlier brief", []guardStep{{ie(C, "issue_classified", crew, "", map[string]any{"check_id": 9714, "risk_class": "small",
			"category": "fault", "brief_sha256": zeros, "seat_report": iw.seat}), []string{`INSERT INTO agentrt.issue_check (issue_check_id,
			issue_id, check_kind, brief_sha256, result, category, seat_artifact_id, created_by_actor_id) OVERRIDING SYSTEM VALUE VALUES (9714, '` + C +
			`', 'classification', '` + zeros + `', 'small', 'fault', '` + iw.seat + `', '` + crew + `')`}}}, "current brief and design"},
		{"evidence of an earlier design", []guardStep{{ie(C, "issue_reviewed", crew, "", map[string]any{"check_id": 9715, "check_kind": "review",
			"result": "pass", "brief_sha256": bC.BriefSHA256, "text_id": 9716}), []string{textSQL(9716, C, "review", "", "thread", "通过", crew),
			`INSERT INTO agentrt.issue_check (issue_check_id, issue_id, check_kind, brief_sha256, design_sha256, result, report_text_id, reviewer_tool,
			author_tool, review_round, created_by_actor_id) OVERRIDING SYSTEM VALUE VALUES (9715, '` + C + `', 'review', '` + bC.BriefSHA256 + `', '` +
				zeros + `', 'pass', 9716, 'codex', 'claude', 2, '` + crew + `')`}}}, "current brief and design"},
		{"a review whose report is a brief", []guardStep{{ie(C, "issue_reviewed", crew, "", map[string]any{"check_id": 9717, "check_kind": "review",
			"result": "pass", "brief_sha256": bC.BriefSHA256}), []string{`INSERT INTO agentrt.issue_check (issue_check_id, issue_id, check_kind,
			brief_sha256, design_sha256, result, report_text_id, reviewer_tool, author_tool, review_round, created_by_actor_id) OVERRIDING SYSTEM VALUE
			VALUES (9717, '` + C + `', 'review', '` + bC.BriefSHA256 + `', '` + bC.DesignSHA256 + `', 'pass', ` + itoa(briefC) + `, 'codex', 'claude', 2, '` +
			crew + `')`}}}, "names a text of another kind"},
		{"a review skipping a round", []guardStep{{ie(C, "issue_reviewed", crew, "", map[string]any{"check_id": 9718, "check_kind": "review",
			"result": "pass", "brief_sha256": bC.BriefSHA256, "text_id": 9719}), []string{textSQL(9719, C, "review", "", "thread", "通过", crew),
			`INSERT INTO agentrt.issue_check (issue_check_id, issue_id, check_kind, brief_sha256, design_sha256, result, report_text_id, reviewer_tool,
			author_tool, review_round, created_by_actor_id) OVERRIDING SYSTEM VALUE VALUES (9718, '` + C + `', 'review', '` + bC.BriefSHA256 + `', '` +
				bC.DesignSHA256 + `', 'pass', 9719, 'codex', 'claude', 3, '` + crew + `')`}}}, "is round 2, not 3"},
		{"a seat's answer that is no brief task's", []guardStep{{ie(C, "issue_classified", crew, "", map[string]any{"check_id": 9720,
			"risk_class": "small", "category": "fault", "brief_sha256": bC.BriefSHA256, "seat_report": notBrief}), []string{`INSERT INTO agentrt.issue_check
			(issue_check_id, issue_id, check_kind, brief_sha256, result, category, seat_artifact_id, created_by_actor_id) OVERRIDING SYSTEM VALUE
			VALUES (9720, '` + C + `', 'classification', '` + bC.BriefSHA256 + `', 'small', 'fault', '` + notBrief + `', '` + crew + `')`}}},
			"report of a brief task"},
		{"a seat's check of an unanswered ask", []guardStep{{ie(B, "issue_reviewed", crew, "", map[string]any{"check_id": 9721,
			"check_kind": "close_check", "result": "supports", "brief_sha256": bB}), []string{`INSERT INTO agentrt.issue_check (issue_check_id, issue_id,
			check_kind, brief_sha256, result, seat_artifact_id, ask_id, created_by_actor_id) OVERRIDING SYSTEM VALUE VALUES (9721, '` + B +
			`', 'close_check', '` + bB + `', 'supports', '` + iw.seat + `', ` + itoa(askB) + `, '` + crew + `')`}}}, "checks an answer that was given"},
		// guard_person, guard_intake_client
		{"a person by a thread", []guardStep{{store.Event{Stream: store.PeopleStream, Type: "intake_client_set", Actor: crew,
			Body: map[string]any{"change": "person", "person_id": "person:x"}}, []string{`INSERT INTO agentrt.person (person_id, display_name)
			VALUES ('person:x', 'x')`}}}, "does not write a intake_client_set event"},
		{"a person's creation moved", []guardStep{{store.Event{Stream: store.PeopleStream, Type: "intake_client_set", Actor: "owner",
			Body: map[string]any{"change": "person", "person_id": "person:p2"}}, []string{`UPDATE agentrt.person SET created_at = created_at -
			interval '1 day' WHERE person_id = 'person:p2'`}}}, "keeps when it was created"},
		{"products that are no keys", []guardStep{{store.Event{Stream: store.IntakeStream(dots), Type: "intake_client_set", Actor: "owner",
			Body: map[string]any{"change": "client", "intake_client_id": dots}}, []string{`UPDATE agentrt.intake_client SET products =
			'{Screening}' WHERE intake_client_id = '` + dots + `'`}}}, "are product keys"},
		{"a client set up by an acknowledgement", []guardStep{{store.Event{Stream: store.IntakeStream("agent:x"), Type: "intake_acked",
			Actor: dots, Body: map[string]any{"intake_client_id": "agent:x", "cursor": "1.1"}}, []string{`INSERT INTO agentrt.intake_client
			(intake_client_id, token_name, products) VALUES ('agent:x', 'intake:x', '{screening}')`}}}, "the Owner sets up an intake client"},
		{"a client of no agent", []guardStep{{store.Event{Stream: store.IntakeStream("agent:ghost"), Type: "intake_client_set", Actor: "owner",
			Body: map[string]any{"change": "client", "intake_client_id": "agent:ghost"}}, []string{`INSERT INTO agentrt.intake_client
			(intake_client_id, token_name, products) VALUES ('agent:ghost', 'intake:ghost', '{screening}')`}}}, "is an agent actor"},
		{"a client born served", []guardStep{
			{store.Event{Stream: "system", Type: "actor_registered", Actor: "owner", Body: map[string]any{"actors": []string{"agent:new"}}},
				[]string{`INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ('agent:new', 'agent', 'new')`}},
			{store.Event{Stream: store.IntakeStream("agent:new"), Type: "intake_client_set", Actor: "owner",
				Body: map[string]any{"change": "client", "intake_client_id": "agent:new"}}, []string{`INSERT INTO agentrt.intake_client
				(intake_client_id, token_name, products, served_txid, served_event) VALUES ('agent:new', 'intake:new', '{screening}', 1, 1)`}}},
			"has been served nothing"},
		{"a token renamed", []guardStep{{store.Event{Stream: store.IntakeStream(dots), Type: "intake_client_set", Actor: "owner",
			Body: map[string]any{"change": "client", "intake_client_id": dots}}, []string{`UPDATE agentrt.intake_client SET token_name =
			'intake:other' WHERE intake_client_id = '` + dots + `'`}}}, "keeps its token name"},
		{"settings by an acknowledgement", []guardStep{{store.Event{Stream: store.IntakeStream(dots), Type: "intake_acked", Actor: dots,
			Body: map[string]any{"intake_client_id": dots, "cursor": "1.1"}}, []string{`UPDATE agentrt.intake_client SET daily_adds = 5
			WHERE intake_client_id = '` + dots + `'`}}}, "changes an intake client's settings"},
	} {
		t.Run(c.name, func(t *testing.T) {
			wantErr(t, iw.run(t, c.steps...), c.want)
		})
	}

	// The import writes none of the seven tables.
	legacy := func(stmt string) []guardStep {
		return []guardStep{{store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller", Body: map[string]any{"source": "test"}},
			[]string{stmt}}}
	}
	for tbl, stmt := range map[string]string{
		"person":        `UPDATE agentrt.person SET display_name = 'x' WHERE person_id = 'person:p2'`,
		"intake_client": `UPDATE agentrt.intake_client SET daily_adds = 5 WHERE intake_client_id = '` + dots + `'`,
		"issue":         upd(E, "waiting_on = 'crew'"),
		"issue_text":    textSQL(9722, E, "note", "", "thread", "n", crew),
		"issue_ask":     `UPDATE agentrt.issue_ask SET superseded_at = now() WHERE issue_ask_id = ` + itoa(askB),
		"issue_work":    `UPDATE agentrt.issue_work SET ended_at = now() WHERE mission_id = '` + mA + `'`,
		"issue_check": `INSERT INTO agentrt.issue_check (issue_check_id, issue_id, check_kind, brief_sha256, result, created_by_actor_id)
		  OVERRIDING SYSTEM VALUE VALUES (9723, '` + C + `', 'priority', '` + bC.BriefSHA256 + `', 'p1', 'owner')`,
	} {
		t.Run("legacy writes no "+tbl, func(t *testing.T) {
			wantErr(t, iw.run(t, legacy(stmt)...), "a legacy_imported event does not write agentrt."+tbl)
		})
	}
}
