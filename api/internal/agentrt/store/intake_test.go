package store_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// How far a client was served moves alone without an event, and only
// forward; anything else on its row needs an event, its acknowledgement the
// client's own intake_acked, never past what it was served (R2-A5, B3).
func TestIntakeServedMovesForwardOnly(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	exec := func(q string, args ...any) error { _, err := iw.rt.Exec(ctx, q, args...); return err }
	served := func() string {
		var s string
		ok(t, iw.rt.QueryRow(ctx, `SELECT COALESCE(served_txid || '.' || served_event, '') FROM agentrt.intake_client WHERE intake_client_id = $1`,
			iw.dots.Actor).Scan(&s))
		return s
	}
	mark := func(c store.EventCursor) error {
		return store.InTx(ctx, iw.rt, func(tx pgx.Tx) error { return store.MarkServed(ctx, tx, iw.dots.Actor, c) })
	}
	events := iw.count(t, `SELECT count(*) FROM agentrt.event`)
	ok(t, mark(store.EventCursor{TxID: 100, EventID: 5}))
	ok(t, mark(store.EventCursor{TxID: 90, EventID: 9})) // older: nothing moves, no error
	if served() != "100.5" || iw.count(t, `SELECT count(*) FROM agentrt.event`) != events {
		t.Fatalf("served %s, events %d → %d: the served pair moves alone, with no event", served(), events, iw.count(t, `SELECT count(*) FROM agentrt.event`))
	}
	ok(t, exec(`UPDATE agentrt.intake_client SET served_txid = 100, served_event = 7 WHERE intake_client_id = $1`, iw.dots.Actor))
	wantErr(t, exec(`UPDATE agentrt.intake_client SET served_txid = 99, served_event = 50 WHERE intake_client_id = $1`, iw.dots.Actor), "only moves forward")
	wantErr(t, exec(`UPDATE agentrt.intake_client SET served_txid = NULL, served_event = NULL WHERE intake_client_id = $1`, iw.dots.Actor), "only moves forward")
	wantErr(t, exec(`UPDATE agentrt.intake_client SET served_txid = 101, products = '{screening,finance}' WHERE intake_client_id = $1`, iw.dots.Actor),
		"needs SET LOCAL")
	wantErr(t, exec(`UPDATE agentrt.intake_client SET served_txid = 101, revoked_at = now() WHERE intake_client_id = $1`, iw.dots.Actor), "needs SET LOCAL")
	wantErr(t, exec(`UPDATE agentrt.intake_client SET acked_txid = 100, acked_event = 1, acked_at = now() WHERE intake_client_id = $1`, iw.dots.Actor),
		"needs SET LOCAL")
	// With an event that is not the client's own acknowledgement, the
	// served pair still only moves forward.
	ack := func(w store.Writer, s string) (map[string]any, error) {
		c, good := store.ParseEventCursor(s)
		if !good {
			t.Fatalf("cursor %s", s)
		}
		return store.AckIntake(ctx, iw.rt, w, c)
	}
	_, err := ack(iw.dots, "100.8")
	wantIs(t, err, store.ErrInvalid, "acknowledges no further")
	out, err := ack(iw.dots, "100.6")
	ok(t, err)
	if out["acked"] != "100.6" || out["moved"] != true {
		t.Fatalf("ack: %v", out)
	}
	out, err = ack(iw.dots, "100.2")
	ok(t, err)
	if out["acked"] != "100.6" || out["moved"] != false {
		t.Fatalf("an older ack: %v", out)
	}
	// Past the store: another client's event does not acknowledge for it, and
	// no acknowledgement goes past what was served (the table's check).
	ackSQL := func(actor, cursor string, txid, event int) error {
		return store.InTx(ctx, iw.rt, func(tx pgx.Tx) error {
			if _, err := store.Append(ctx, tx, store.Event{Stream: store.IntakeStream(iw.dots.Actor), Type: "intake_acked", Actor: actor,
				Body: map[string]any{"intake_client_id": iw.dots.Actor, "cursor": cursor}}); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE agentrt.intake_client SET acked_txid = $2, acked_event = $3, acked_at = now() WHERE intake_client_id = $1`,
				iw.dots.Actor, txid, event)
			return err
		})
	}
	wantErr(t, ackSQL(iw.dotsOwner.Actor, "100.7", 100, 7), "acknowledges its own updates")
	wantErr(t, ackSQL(iw.dots.Actor, "200.1", 200, 1), "intake_client_check")
	wantErr(t, ackSQL(iw.dots.Actor, "100.5", 100, 5), "only moves forward")
	ok(t, ackSQL(iw.dots.Actor, "100.7", 100, 7))
	// Settings are the Owner's, by intake_client_set; a revoked client stays
	// revoked.
	ok(t, store.RevokeIntakeClient(ctx, iw.rt, iw.owner, iw.dots.Actor))
	_, err = store.SetIntakeClient(ctx, iw.rt, iw.owner, store.IntakeClientSetup{ClientID: iw.dots.Actor, OnBehalfOf: "person:p2", Products: []string{"screening"}})
	wantErr(t, err, "stays so")
	wantErr(t, store.InTx(ctx, iw.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: store.IntakeStream(iw.dots.Actor), Type: "intake_client_set", Actor: "owner",
			Body: map[string]any{"change": "client", "intake_client_id": iw.dots.Actor}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.intake_client SET products = '{screening,finance}' WHERE intake_client_id = $1`, iw.dots.Actor)
		return err
	}), "is revoked and stays so")
	_, err = store.SetIntakeClient(ctx, iw.rt, iw.crew, store.IntakeClientSetup{ClientID: "agent:other", Products: []string{"screening"}})
	wantIs(t, err, store.ErrInvalid, "only the Owner")
}

// An external agent brings issues only for the person it is bound to, of
// its products, within its daily limits, while its token is bound and not
// revoked; a replay of its key passes the same checks first (§2.1, §5.2).
func TestIntakeClientScope(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	submit := func(w store.Writer, key string, s store.IssueSubmission) (map[string]any, error) {
		return store.SubmitIssue(ctx, iw.rt, w, key, s)
	}
	hers := store.IssueSubmission{Product: "screening", Kind: "complaint", Title: "t", OnBehalfOf: "person:p2",
		Words: []store.IssueWords{{Text: "her words"}}}
	forHim := hers
	forHim.OnBehalfOf = store.OwnerPerson
	_, err := submit(iw.dots, iw.key(), forHim)
	wantIs(t, err, store.ErrInvalid, "speaks for person:p2 only")
	other := hers
	other.Product = "finance"
	_, err = submit(iw.dots, iw.key(), other)
	wantIs(t, err, store.ErrInvalid, "brings issues of [screening], not finance")
	noWords := hers
	noWords.Words = nil
	_, err = submit(iw.dots, iw.key(), noWords)
	wantIs(t, err, store.ErrInvalid, "carries her own words")
	selfWithWords := hers
	selfWithWords.OnBehalfOf = store.SelfIssue
	_, err = submit(iw.dots, iw.key(), selfWithWords)
	wantIs(t, err, store.ErrInvalid, "carries no person's words")
	// Past the store: an issue for another person is refused by the database.
	wantErr(t, store.InTx(ctx, iw.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: store.IssueStream("BI-77"), Type: "issue_submitted", Actor: iw.dots.Actor,
			Body: map[string]any{"issue_id": "BI-77", "product": "screening", "kind": "complaint", "channel": "intake_local", "text_ids": []any{int64(1)}}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO agentrt.issue (issue_id, product, kind, title, served_person, asks_person, state, waiting_on, created_by_actor_id)
		  VALUES ('BI-77', 'screening', 'complaint', 't', 'person:owner', 'person:owner', 'clarifying', 'crew', $1)`, iw.dots.Actor)
		return err
	}), "submits only for the person it is bound to")
	// Daily limits: two submissions, one own proposal.
	two, one := 2, 1
	_, err = store.SetIntakeClient(ctx, iw.rt, iw.owner, store.IntakeClientSetup{ClientID: iw.dots.Actor, OnBehalfOf: "person:p2",
		Products: []string{"screening"}, Submits: &two, Proposals: &one})
	ok(t, err)
	firstKey := iw.key()
	first, err := submit(iw.dots, firstKey, hers)
	ok(t, err)
	idea := store.IssueSubmission{Product: "screening", Kind: "proposal", Title: "idea", OnBehalfOf: store.SelfIssue, Proposal: "an idea"}
	_, err = submit(iw.dots, iw.key(), idea)
	ok(t, err)
	_, err = submit(iw.dots, iw.key(), idea)
	wantIs(t, err, store.ErrQuota, "limit")
	_, err = submit(iw.dots, iw.key(), hers)
	wantIs(t, err, store.ErrQuota, "limit")
	// A replay of the first is its first answer, under the limit too.
	again, err := submit(iw.dots, firstKey, hers)
	ok(t, err)
	if again["issue_id"] != first["issue_id"] {
		t.Fatalf("replay: %v, first %v", again, first)
	}
	// Another token naming the agent is not it.
	_, err = submit(store.Writer{Actor: iw.dots.Actor, Token: "intake:someone"}, iw.key(), hers)
	if !errors.Is(err, store.ErrNotYours) {
		t.Fatalf("another token: %v", err)
	}
	// An agent is not the crew.
	_, err = store.DraftBrief(ctx, iw.rt, iw.dots, iw.key(), store.BriefDraft{IssueID: first["issue_id"].(string), Text: "b"})
	wantIs(t, err, store.ErrInvalid, "is not the crew")
	// Revoked: at once, writes and replays alike.
	ok(t, store.RevokeIntakeClient(ctx, iw.rt, iw.owner, iw.dots.Actor))
	_, err = submit(iw.dots, firstKey, hers)
	if !errors.Is(err, store.ErrRevoked) {
		t.Fatalf("a replay after revocation: %v", err)
	}
	_, err = store.AddToIssue(ctx, iw.rt, iw.dots, iw.key(), store.IssueAddition{IssueID: first["issue_id"].(string), Reading: "more"})
	if !errors.Is(err, store.ErrRevoked) {
		t.Fatalf("an addition after revocation: %v", err)
	}
}

// What a person said, and every other text of an issue, never reaches the
// generic artifact and event reads (§5.3): no event holds it, no artifact
// is made of it, and the same body uploaded on its own stays an artifact of
// its own, linked to nothing of the issue, not even through the issue's
// mission.
func TestIssueTextNeverInGenericReads(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	marker := "MARK-" + strings.ToLower(store.NewTaskID()[2:])
	words := "the list is empty again " + marker
	uploaded, _, err := store.UploadArtifact(ctx, iw.rt, iw.crew, "other", "text/plain", []byte(words))
	ok(t, err)
	out, err := store.SubmitIssue(ctx, iw.rt, iw.dots, iw.key(), store.IssueSubmission{Product: "screening", Kind: "complaint",
		Title: "empty list", OnBehalfOf: "person:p2", Words: []store.IssueWords{{Text: words, Where: "screen " + marker}},
		Reading: "reading " + marker, Proposal: "proposal " + marker})
	ok(t, err)
	id := out["issue_id"].(string)
	ask := iw.brief(t, id, "brief "+marker)
	_, err = store.AnswerAsk(ctx, iw.rt, iw.dots, iw.key(), store.AskAnswer{IssueID: id, ExpectedVersion: iw.state(t, id).Version, AskID: ask,
		Words: "yes " + marker, Verdict: "confirmed", Note: "note " + marker})
	ok(t, err)
	_, err = store.ClassifyIssue(ctx, iw.rt, iw.crew, iw.key(), store.IssueClassification{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
		RiskClass: "small", Category: "fault", SeatReport: iw.seat, Reason: "reason " + marker})
	ok(t, err)
	ok(t, iw.prioritize(t, id, "p1"))
	taken, err := store.TakeIssue(ctx, iw.rt, iw.crew, iw.key(), store.IssueTake{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
		Commitment: "commitment " + marker, Mission: fitting}, nil)
	ok(t, err)
	m := taken["mission_id"].(string)
	trial, err := iw.trial(t, id, 1, m)
	ok(t, err)
	_, err = iw.answer(t, iw.dots, id, trial, "confirmed")
	ok(t, err)
	_, err = store.Reply(ctx, iw.rt, iw.crew, iw.key(), store.IssueReply{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
		Purpose: store.ReplyPlain, Text: "reply " + marker})
	ok(t, err)

	if n := iw.count(t, `SELECT count(*) FROM agentrt.issue_text WHERE issue_id = $1 AND position($2 in body || COALESCE(said_where, '')) > 0`,
		id, marker); n < 9 {
		t.Fatalf("only %d texts hold the marker", n)
	}
	if n := iw.count(t, `SELECT count(*) FROM agentrt.event WHERE position($1 in body_json::text) > 0`, marker); n != 0 {
		t.Fatalf("%d events hold an issue's text", n)
	}
	var holders []string
	rows, err := iw.rt.Query(ctx, `SELECT artifact_id FROM agentrt.artifact_blob WHERE position(convert_to($1, 'UTF8') in body) > 0`, marker)
	ok(t, err)
	holders, err = pgx.CollectRows(rows, pgx.RowTo[string])
	ok(t, err)
	if len(holders) != 1 || holders[0] != uploaded {
		t.Fatalf("artifacts holding the marker: %v; want only the one uploaded on its own (%s)", holders, uploaded)
	}
	// The issue's texts are no artifacts, by their own sha256 either.
	if n := iw.count(t, `SELECT count(*) FROM agentrt.issue_text t JOIN agentrt.artifact a ON a.artifact_id = 'sha256:' || t.sha256
	  WHERE t.issue_id = $1 AND t.text_kind <> 'words'`, id); n != 0 {
		t.Fatalf("%d of the issue's texts are artifacts", n)
	}
	// The issue's mission names no artifact holding her words.
	metas, _, err := store.SearchArtifacts(ctx, iw.rt, store.ArtifactQuery{Mission: m, Limit: 100})
	ok(t, err)
	for _, a := range metas {
		if a.ID == uploaded {
			t.Fatalf("the issue's mission lists the artifact of the same words: %v", metas)
		}
	}
	// Every issue event names texts by text_id, never by a key artifact
	// search follows.
	rows, err = iw.rt.Query(ctx, `SELECT body_json FROM agentrt.event WHERE event_stream_id = $1`, store.IssueStream(id))
	ok(t, err)
	bodies, err := pgx.CollectRows(rows, pgx.RowTo[[]byte])
	ok(t, err)
	for _, b := range bodies {
		var body map[string]any
		ok(t, json.Unmarshal(b, &body))
		for _, k := range []string{"artifact_id", "text_artifact_id", "artifacts", "evidence"} {
			if _, has := body[k]; has {
				t.Fatalf("an issue event names %s: %s", k, b)
			}
		}
	}
}

// The issue tables keep their rows: no deletion or truncation, for the
// runtime and the owner alike; texts and evidence are never rewritten;
// agent_reader is granted nothing on them; the one-time import writes none
// of them (§4.4).
func TestIssueTablesHoldTheirRows(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	id := iw.submit(t, "keep me")
	owner := iw.db.Connect(t, iw.db.Owner)
	reader := iw.db.Connect(t, iw.db.Reader)
	run := func(c *pgx.Conn, q string) error { _, err := c.Exec(ctx, q); return err }
	for _, c := range []*pgx.Conn{iw.rt, owner} {
		wantErr(t, run(c, `DELETE FROM agentrt.issue WHERE issue_id = '`+id+`'`), "")
		wantErr(t, run(c, `DELETE FROM agentrt.issue_text`), "")
		wantErr(t, run(c, `UPDATE agentrt.issue_text SET body = 'x'`), "")
		wantErr(t, run(c, `TRUNCATE agentrt.issue_check`), "")
	}
	wantErr(t, run(owner, `DELETE FROM agentrt.issue WHERE issue_id = '`+id+`'`), "end by state")
	wantErr(t, run(owner, `UPDATE agentrt.issue_text SET body = 'x'`), "append only")
	wantErr(t, run(iw.rt, `DELETE FROM agentrt.person`), "")
	for _, tbl := range []string{"issue", "issue_text", "issue_ask", "issue_check", "issue_work", "person", "intake_client"} {
		wantErr(t, run(reader, `SELECT 1 FROM agentrt.`+tbl+` LIMIT 1`), "permission denied")
	}
	wantErr(t, store.InTx(ctx, iw.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO agentrt.person (person_id, display_name) VALUES ('person:x', 'x')`)
		return err
	}), "a legacy_imported event does not write agentrt.person")
	// The old tables still take the import.
	iw.legacy(t, `UPDATE agentrt.mission SET title = 'imported' WHERE mission_id = 'M-2'`)
}

// The cross-stream cursor (§4.3): an event whose transaction took a smaller
// id and committed later is not skipped; reading on lists each event once,
// in commit order by transaction.
func TestEventsAfterKeepsLateCommits(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	pool := iw.db.Pool(t, iw.db.Runtime)
	readAll := func(from store.EventCursor) ([]int64, store.EventCursor) {
		var ids []int64
		cur := from
		for {
			var doc []byte
			var next store.EventCursor
			ok(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
				var err error
				doc, next, err = store.EventsAfter(ctx, tx, cur, 2, store.EventFilter{})
				return err
			}))
			var page []struct {
				EventID int64 `json:"event_id"`
			}
			ok(t, json.Unmarshal(doc, &page))
			for _, e := range page {
				ids = append(ids, e.EventID)
			}
			if len(page) < 2 {
				return ids, next
			}
			cur = next
		}
	}
	_, cur := readAll(store.EventCursor{})
	note := func(tx pgx.Tx, stream string) int64 {
		ev, err := store.Append(ctx, tx, store.Event{Stream: stream, Type: "note", Actor: "controller", Body: map[string]any{"text": stream}})
		ok(t, err)
		return ev.EventID
	}
	late, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	ok(t, err)
	defer func() { _ = late.Rollback(ctx) }()
	small := note(late, "issue:BI-90")
	var big int64
	ok(t, store.InTx(ctx, pool, func(tx pgx.Tx) error { big = note(tx, "issue:BI-91"); return nil }))
	if big <= small {
		t.Fatalf("ids %d, %d", small, big)
	}
	seen, mid := readAll(cur)
	for _, id := range seen {
		if id == big || id == small {
			t.Fatalf("read %v while the older transaction ran", seen)
		}
	}
	ok(t, late.Commit(ctx))
	// A snapshot's xmin is the cluster's: another database's transaction may
	// hold the horizon a moment longer, so read on until both show.
	var got []int64
	cur = mid
	for deadline := time.Now().Add(20 * time.Second); len(got) < 2 && time.Now().Before(deadline); {
		ids, next := readAll(cur)
		for _, id := range ids {
			if id == small || id == big {
				got = append(got, id)
			}
		}
		cur = next
		if len(got) < 2 {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if len(got) != 2 || got[0] != small || got[1] != big {
		t.Fatalf("read on: %v; want [%d %d]", got, small, big)
	}
}
