package questions

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

func testSvc(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	s := New(filepath.Join(dir, "questions"), actuation.NewAuditLog(filepath.Join(dir, "audit.json")))
	s.post = acceptPost()
	return s
}

func acceptPost() func(context.Context, notice.Message) ([]notice.Delivery, error) {
	return func(context.Context, notice.Message) ([]notice.Delivery, error) {
		return []notice.Delivery{{Channel: "ntfy", Target: "phone", Result: notice.ResultAccepted}}, nil
	}
}

func ask(t *testing.T, s *Service, session, key string, extra map[string]any) Result {
	t.Helper()
	fields := map[string]any{
		"idempotency_key": key,
		"title":           "Where is the quota?",
		"urgency":         "Before tonight",
		"meanwhile":       "The thread waits",
	}
	for k, v := range extra {
		fields[k] = v
	}
	res := s.Create(context.Background(), session, "reporter", "reporter", mustJSON(t, fields))
	return res
}

func mustCreate(t *testing.T, s *Service, session, key string, extra map[string]any) map[string]any {
	t.Helper()
	res := ask(t, s, session, key, extra)
	if res.Code != http.StatusCreated {
		t.Fatalf("create %s = %d %v", key, res.Code, res.Body)
	}
	return res.Body.(map[string]any)
}

func answer(t *testing.T, s *Service, ref string, body map[string]any) Result {
	t.Helper()
	if body == nil {
		body = map[string]any{"option": "none", "text": "See settings.", "channel": "console"}
	}
	return s.Answer(context.Background(), "owner-console", "admin", "admin", ref, mustJSON(t, body))
}

func TestAuthorizationAndAnswerIsNotApproval(t *testing.T) {
	h, svc := newHTTP(t)
	res := call(t, h, http.MethodPost, "/api/v1/questions", createBodyJSON(t, "0123456789abcdef"), "reporter-test-token", "thread-a")
	if res.Code != http.StatusCreated {
		t.Fatalf("reporter create = %d %s", res.Code, res.Body)
	}
	var created struct {
		ID     string `json:"id"`
		Number int    `json:"number"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	before := readRaw(t, svc)
	for _, token := range []string{"reporter-test-token", "operator-test-token"} {
		got := call(t, h, http.MethodPost, "/api/v1/questions/"+created.ID+"/answer", `{"option":"none","text":"no","channel":"console"}`, token, "owner-console")
		if got.Code != http.StatusUnauthorized {
			t.Fatalf("%s answer = %d %s", token, got.Code, got.Body)
		}
	}
	if after := readRaw(t, svc); after != before {
		t.Fatal("a non-admin answer changed the document")
	}
	ok := call(t, h, http.MethodPost, "/api/v1/questions/"+created.ID+"/answer", `{"option":"none","text":"See settings.","channel":"console"}`, "admin-test-token", "owner-console")
	if ok.Code != http.StatusOK || !contains(ok.Body.String(), `"by":"admin"`) || !contains(ok.Body.String(), `"approval":false`) {
		t.Fatalf("admin answer = %d %s", ok.Code, ok.Body)
	}
	wd := call(t, h, http.MethodPost, "/api/v1/questions", createBodyJSON(t, "abcdef0123456789"), "reporter-test-token", "thread-b")
	if wd.Code != http.StatusCreated {
		t.Fatal(wd.Body.String())
	}
	var second struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(wd.Body.Bytes(), &second)
	withdraw := call(t, h, http.MethodPost, "/api/v1/questions/"+second.ID+"/withdraw", `{"reason":"asked in thread"}`, "reporter-test-token", "thread-b")
	if withdraw.Code != http.StatusOK {
		t.Fatalf("withdraw = %d %s", withdraw.Code, withdraw.Body)
	}
	again := call(t, h, http.MethodPost, "/api/v1/questions/"+second.ID+"/answer", `{"option":"none","text":"late","channel":"console"}`, "admin-test-token", "owner-console")
	if again.Code != http.StatusConflict || !contains(again.Body.String(), `"error":"not open"`) {
		t.Fatalf("answer after withdraw = %d %s", again.Code, again.Body)
	}
	got := call(t, h, http.MethodGet, "/api/v1/questions/"+second.ID, "", "viewer-test-token", "thread-b")
	if contains(got.Body.String(), `"answer"`) {
		t.Fatalf("withdrawn record has an answer: %s", got.Body)
	}
}

func TestExpiryDoesNotSelectRecommended(t *testing.T) {
	s := testSvc(t)
	start := time.Date(2026, 10, 11, 6, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return start })
	created := mustCreate(t, s, "thread-a", "0123456789abcdef", map[string]any{
		"options":     []any{map[string]any{"label": "Ship"}, map[string]any{"label": "Wait"}},
		"recommended": 1,
		"ttl_hours":   1,
	})
	id := created["id"].(string)
	s.SetClock(func() time.Time { return start.Add(time.Hour) })
	res := answer(t, s, id, map[string]any{"option": "a", "channel": "console"})
	if res.Code != http.StatusConflict || res.Body.(map[string]any)["error"] != "expired" {
		t.Fatalf("answer at expiry = %d %v", res.Code, res.Body)
	}
	got := s.Get(context.Background(), "thread-a", "reporter", "reporter", id)
	q := got.Body.(Question)
	if q.Status != StatusExpired || q.Answer != nil {
		t.Fatalf("record = %+v", q)
	}
	if !auditHas(t, s, "question.expire", id) {
		t.Fatal("expiry was not audited")
	}
}

func TestTextCleaningAndUnknownFields(t *testing.T) {
	s := testSvc(t)
	over := ask(t, s, "thread-a", "0123456789abcdef", map[string]any{"title": repeat('a', 299)})
	if over.Code != http.StatusBadRequest || !contains(over.Body.(map[string]any)["error"].(string), "title") {
		t.Fatalf("long title = %d %v", over.Code, over.Body)
	}
	raw := mustJSON(t, map[string]any{
		"idempotency_key": "0123456789abcdef",
		"title":           "Hello",
		"body":            "a\u202eb\u200bc\x07<script>",
		"urgency":         "now",
		"meanwhile":       "wait",
		"options":         []any{map[string]any{"label": "a"}, map[string]any{"label": "b"}, map[string]any{"label": "c"}, map[string]any{"label": "d"}, map[string]any{"label": "e"}},
	})
	five := s.Create(context.Background(), "thread-a", "reporter", "reporter", raw)
	if five.Code != http.StatusBadRequest || !contains(five.Body.(map[string]any)["error"].(string), "options") {
		t.Fatalf("five options = %d %v", five.Code, five.Body)
	}
	unknown := s.Create(context.Background(), "thread-a", "reporter", "reporter", []byte(`{"idempotency_key":"0123456789abcdef","title":"Hello","urgency":"now","meanwhile":"wait","action":"ship"}`))
	if unknown.Code != http.StatusBadRequest || unknown.Body.(map[string]any)["error"] != "unknown field action" {
		t.Fatalf("unknown = %d %v", unknown.Code, unknown.Body)
	}
	if err := ValidSession(repeat('a', 129)); err == nil {
		t.Fatal("129-byte session was accepted")
	}
	if err := ValidSession("has space"); err == nil {
		t.Fatal("session with a space was accepted")
	}
	created := mustCreate(t, s, "thread-a", "0123456789abcdef", map[string]any{
		"body": "a\u202eb\u200bc\x07<script>",
	})
	q := s.Get(context.Background(), "thread-a", "reporter", "reporter", created["id"].(string)).Body.(Question)
	if q.Body != "abc<script>" {
		t.Fatalf("body = %q", q.Body)
	}
}

func TestFingerprintsIdempotencyAndDedup(t *testing.T) {
	s := testSvc(t)
	var posts int
	s.post = func(context.Context, notice.Message) ([]notice.Delivery, error) {
		posts++
		return []notice.Delivery{{Channel: "ntfy", Target: "phone", Result: notice.ResultAccepted}}, nil
	}
	base := map[string]any{"recommended": 1, "options": twoOptions(), "work_id": "W-65"}
	first := mustCreate(t, s, "thread-a", "0123456789abcdef", base)
	if posts != 1 {
		t.Fatalf("posts = %d, want 1", posts)
	}
	changed := ask(t, s, "thread-a", "0123456789abcdef", map[string]any{"recommended": 2, "options": twoOptions(), "work_id": "W-65"})
	if changed.Code != http.StatusConflict || changed.Body.(map[string]any)["error"] != "idempotency key reused with different content" {
		t.Fatalf("recommended change = %d %v", changed.Code, changed.Body)
	}
	replay := ask(t, s, "thread-a", "0123456789abcdef", map[string]any{
		"title": " \u200bWhere is the quota? ", "recommended": 1, "options": twoOptions(), "work_id": "W-65",
	})
	if replay.Code != http.StatusOK || replay.Body.(map[string]any)["replayed"] != true || posts != 1 {
		t.Fatalf("replay = %d %v posts %d", replay.Code, replay.Body, posts)
	}
	dup := ask(t, s, "thread-a", "abcdef0123456789", map[string]any{"recommended": 1, "options": twoOptions(), "work_id": "W-65"})
	if dup.Code != http.StatusConflict || dup.Body.(map[string]any)["error"] != "duplicate of "+numberLabel(1) {
		t.Fatalf("duplicate = %d %v", dup.Code, dup.Body)
	}
	other := mustCreate(t, s, "thread-a", "abcdef0123456789", map[string]any{"recommended": 1, "options": twoOptions(), "work_id": "W-66"})
	if other["id"] == first["id"] {
		t.Fatal("different work_id reused the id")
	}
	if posts != 2 {
		t.Fatalf("posts after second question = %d", posts)
	}
}

func TestSameKeyAfterTerminalCreatesANewQuestion(t *testing.T) {
	s := testSvc(t)
	start := time.Date(2026, 10, 11, 6, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return start })
	for _, tc := range []string{"withdraw", "expire", "ack"} {
		t.Run(tc, func(t *testing.T) {
			key := "key-for-" + tc + "-1"
			if len(key) < 16 {
				key = "0123456789abcdef"
			}
			created := mustCreate(t, s, "thread-a", key, map[string]any{"title": tc, "ttl_hours": 1})
			id := created["id"].(string)
			switch tc {
			case "withdraw":
				res := s.Withdraw(context.Background(), "thread-a", "reporter", "reporter", id, mustJSON(t, map[string]any{"reason": "done"}))
				if res.Code != http.StatusOK {
					t.Fatal(res.Body)
				}
			case "expire":
				s.SetClock(func() time.Time { return start.Add(2 * time.Hour) })
				s.Sweep()
				s.SetClock(func() time.Time { return start })
			case "ack":
				if res := answer(t, s, id, nil); res.Code != http.StatusOK {
					t.Fatal(res.Body)
				}
				ack := s.Ack(context.Background(), "thread-a", "reporter", "reporter", id, nil)
				if ack.Code != http.StatusOK {
					t.Fatal(ack.Body)
				}
			}
			again := mustCreate(t, s, "thread-a", key, map[string]any{"title": tc, "ttl_hours": 1})
			if again["id"] == id {
				t.Fatal("terminal question was reused")
			}
		})
	}
}

func TestRequesterOpenQuotaAndUnreadList(t *testing.T) {
	s := testSvc(t)
	for i := 1; i <= 3; i++ {
		mustCreate(t, s, "thread-a", keyN(i), map[string]any{"title": "open " + itoa(i)})
	}
	fourth := ask(t, s, "thread-a", keyN(4), map[string]any{"title": "open 4"})
	if fourth.Code != http.StatusTooManyRequests || fourth.Body.(map[string]any)["error"] != "read and ack Q1, Q2, Q3 first" {
		t.Fatalf("fourth = %d %v", fourth.Code, fourth.Body)
	}
}

func TestUnreadQuotaListsNumbers(t *testing.T) {
	s := testSvc(t)
	start := time.Date(2026, 10, 11, 6, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return start })
	var ids []string
	for i := 1; i <= 6; i++ {
		created := mustCreate(t, s, "thread-a", keyN(i), map[string]any{"title": "q" + itoa(i)})
		ids = append(ids, created["id"].(string))
		if res := answer(t, s, created["id"].(string), map[string]any{"text": "n" + itoa(i), "channel": "console"}); res.Code != http.StatusOK {
			t.Fatal(res.Body)
		}
	}
	s.SetClock(func() time.Time { return start.Add(2 * time.Hour) })
	got := ask(t, s, "thread-a", keyN(7), map[string]any{"title": "seventh"})
	if got.Code != http.StatusTooManyRequests || got.Body.(map[string]any)["error"] != "read and ack Q1, Q2, Q3, Q4, Q5, Q6 first" {
		t.Fatalf("seventh = %d %v", got.Code, got.Body)
	}
	_ = ids
}

func TestGlobalProtectedQuota(t *testing.T) {
	s := testSvc(t)
	start := time.Date(2026, 10, 11, 6, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return start })
	n := 0
	for pass := 0; pass < 2; pass++ {
		if pass == 1 {
			s.SetClock(func() time.Time { return start.Add(2 * time.Hour) })
		}
		per := 3
		if pass == 1 {
			per = 1
		}
		for r := 0; r < 10; r++ {
			session := "req-" + itoa(r)
			for j := 0; j < per; j++ {
				n++
				created := mustCreate(t, s, session, keyN(n), map[string]any{"title": "g" + itoa(n)})
				if res := answer(t, s, created["id"].(string), map[string]any{"text": "a", "channel": "console"}); res.Code != http.StatusOK {
					t.Fatalf("answer %d = %v", n, res.Body)
				}
			}
		}
	}
	got := ask(t, s, "req-0", keyN(n+1), map[string]any{"title": "one more"})
	if got.Code != http.StatusTooManyRequests || got.Body.(map[string]any)["error"] != "store full: 40 questions are open or unread" {
		t.Fatalf("41st = %d %v", got.Code, got.Body)
	}
}

func TestHourlyQuotaSurvivesPrune(t *testing.T) {
	s := testSvc(t)
	s.store.keepOverride = true
	s.store.keep = 0
	n := 0
	withdrawn := func(session string) {
		t.Helper()
		n++
		created := mustCreate(t, s, session, keyN(n), map[string]any{"title": "h" + itoa(n)})
		res := s.Withdraw(context.Background(), session, "reporter", "reporter", created["id"].(string), mustJSON(t, map[string]any{"reason": "done"}))
		if res.Code != http.StatusOK {
			t.Fatalf("withdraw = %v", res.Body)
		}
	}
	for j := 0; j < 6; j++ {
		withdrawn("hour-0")
	}
	if len(loadDoc(t, s).Questions) != 0 {
		t.Fatal("pruned questions were still stored")
	}
	got := ask(t, s, "hour-0", keyN(n+1), map[string]any{"title": "still"})
	if got.Code != http.StatusTooManyRequests || got.Body.(map[string]any)["error"] != "hourly quota: 6 questions from this requester in the last hour" {
		t.Fatalf("seventh from the same requester = %d %v", got.Code, got.Body)
	}
	for r := 1; r < 5; r++ {
		for j := 0; j < 6; j++ {
			withdrawn("hour-" + itoa(r))
		}
	}
	if len(loadDoc(t, s).Questions) != 0 {
		t.Fatal("records came back after the quota window filled")
	}
	got = ask(t, s, "hour-new", keyN(n+2), map[string]any{"title": "global"})
	if got.Code != http.StatusTooManyRequests || got.Body.(map[string]any)["error"] != "hourly quota: 30 questions created in the last hour" {
		t.Fatalf("31st = %d %v", got.Code, got.Body)
	}
}

func TestInboxAckAndRetention(t *testing.T) {
	s := testSvc(t)
	start := time.Date(2026, 10, 11, 6, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return start })
	created := mustCreate(t, s, "thread-a", "0123456789abcdef", nil)
	id := created["id"].(string)
	if res := answer(t, s, id, nil); res.Code != http.StatusOK {
		t.Fatal(res.Body)
	}
	for i := 0; i < 2; i++ {
		list := s.List(context.Background(), "thread-a", "reporter", "reporter", "inbox", true)
		rows := list.Body.(map[string]any)["questions"].([]Question)
		if len(rows) != 1 || rows[0].ID != id {
			t.Fatalf("inbox pass %d = %+v", i, rows)
		}
	}
	first := s.Ack(context.Background(), "thread-a", "reporter", "reporter", id, nil)
	if first.Code != http.StatusOK {
		t.Fatal(first.Body)
	}
	at := first.Body.(Question).Answer.Read.At
	second := s.Ack(context.Background(), "thread-a", "other", "reporter", id, []byte(`{}`))
	if second.Code != http.StatusOK || !second.Body.(Question).Answer.Read.At.Equal(at) {
		t.Fatalf("repeat ack = %d %v", second.Code, second.Body)
	}
	list := s.List(context.Background(), "thread-a", "reporter", "reporter", "", true)
	rows := list.Body.(map[string]any)["questions"].([]Question)
	if len(rows) != 0 {
		t.Fatalf("inbox after ack = %+v", rows)
	}

	s2 := testSvc(t)
	s2.SetClock(func() time.Time { return start })
	kept := mustCreate(t, s2, "thread-a", "0123456789abcdef", nil)
	if res := answer(t, s2, kept["id"].(string), nil); res.Code != http.StatusOK {
		t.Fatal(res.Body)
	}
	err := s2.store.update(func(doc *document) error {
		doc.asOf = start
		for i := 0; i < 301; i++ {
			doc.Questions = append(doc.Questions, Question{
				ID: "q_" + pad(i), Number: 1000 + i, Status: StatusWithdrawn,
				Requester: "other", RequesterPrincipal: "reporter", Title: "old",
				Urgency: "u", Meanwhile: "m", AllowText: true,
				CreatedAt:  start.Add(time.Duration(i) * time.Minute),
				ExpiresAt:  start.Add(24 * time.Hour),
				Withdrawal: &Withdrawal{At: start.Add(time.Duration(i+1) * time.Minute), Reason: "x", Session: "other", Principal: "reporter"},
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasID(t, s2, kept["id"].(string)) {
		t.Fatal("unread question was pruned before 7 days")
	}
	later := start.Add(unreadKeep + time.Hour)
	s2.SetClock(func() time.Time { return later })
	fresh := mustCreate(t, s2, "thread-b", "abcdef0123456789", map[string]any{"title": "newer"})
	_ = s2.Withdraw(context.Background(), "thread-b", "reporter", "reporter", fresh["id"].(string), mustJSON(t, map[string]any{"reason": "prune"}))
	if hasID(t, s2, kept["id"].(string)) {
		t.Fatal("unread question was kept after 7 days")
	}
}

func TestImpersonatedAckRecordsRealPrincipal(t *testing.T) {
	h, svc := newHTTP(t)
	res := call(t, h, http.MethodPost, "/api/v1/questions", createBodyJSON(t, "0123456789abcdef"), "reporter-test-token", "thread-a")
	if res.Code != http.StatusCreated {
		t.Fatal(res.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &created)
	ans := call(t, h, http.MethodPost, "/api/v1/questions/"+created.ID+"/answer", `{"text":"yes","channel":"console"}`, "admin-test-token", "owner-console")
	if ans.Code != http.StatusOK {
		t.Fatal(ans.Body.String())
	}
	ack := call(t, h, http.MethodPost, "/api/v1/questions/"+created.ID+"/ack", `{}`, "operator-test-token", "thread-a")
	if ack.Code != http.StatusOK || !contains(ack.Body.String(), `"principal":"operator"`) {
		t.Fatalf("ack = %d %s", ack.Code, ack.Body)
	}
	if !auditHas(t, svc, "question.ack", created.ID) {
		t.Fatal("ack was not audited")
	}
	raw := readAudit(t, svc)
	if !contains(raw, `"actor": "operator"`) && !contains(raw, `"actor":"operator"`) {
		t.Fatalf("audit missing operator principal: %s", raw)
	}
	if !contains(raw, "session=thread-a") {
		t.Fatalf("audit missing session: %s", raw)
	}
}

func TestAnswerRaces(t *testing.T) {
	s := testSvc(t)
	created := mustCreate(t, s, "thread-a", "0123456789abcdef", nil)
	id := created["id"].(string)
	var wg sync.WaitGroup
	results := make(chan int, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		results <- answer(t, s, id, map[string]any{"text": "one", "channel": "console"}).Code
	}()
	go func() {
		defer wg.Done()
		results <- s.Withdraw(context.Background(), "thread-a", "reporter", "reporter", id, mustJSON(t, map[string]any{"reason": "stop"})).Code
	}()
	wg.Wait()
	close(results)
	ok, conflict := 0, 0
	for code := range results {
		switch code {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			conflict++
		default:
			t.Fatalf("code %d", code)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("ok=%d conflict=%d", ok, conflict)
	}

	s2 := testSvc(t)
	created = mustCreate(t, s2, "thread-a", "0123456789abcdef", nil)
	id = created["id"].(string)
	codes := make(chan int, 2)
	var wg2 sync.WaitGroup
	wg2.Add(2)
	for _, text := range []string{"left", "right"} {
		text := text
		go func() {
			defer wg2.Done()
			codes <- answer(t, s2, id, map[string]any{"text": text, "channel": "console"}).Code
		}()
	}
	wg2.Wait()
	close(codes)
	ok, conflict = 0, 0
	for code := range codes {
		if code == http.StatusOK {
			ok++
		} else if code == http.StatusConflict {
			conflict++
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("two answers ok=%d conflict=%d", ok, conflict)
	}
	replay := answer(t, s2, id, nil)
	// The stored text is whichever won. Resend that text.
	q := s2.Get(context.Background(), "thread-a", "reporter", "reporter", id).Body.(Question)
	at := q.Answer.At
	again := answer(t, s2, id, map[string]any{"text": q.Answer.Text, "channel": "console"})
	if again.Code != http.StatusOK || again.Body.(map[string]any)["replayed"] != true {
		t.Fatalf("replay = %d %v", again.Code, again.Body)
	}
	q2 := s2.Get(context.Background(), "thread-a", "reporter", "reporter", id).Body.(Question)
	if !q2.Answer.At.Equal(at) {
		t.Fatal("replay rewrote answer.at")
	}
	_ = replay
}

func TestAnswerWaitsAcrossDeadline(t *testing.T) {
	s := testSvc(t)
	start := time.Date(2026, 10, 11, 6, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return start })
	created := mustCreate(t, s, "thread-a", "0123456789abcdef", map[string]any{"ttl_hours": 1})
	id := created["id"].(string)
	held := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = s.store.update(func(doc *document) error {
			close(held)
			<-release
			return errNoChange
		})
	}()
	<-held
	s.SetClock(func() time.Time { return start.Add(time.Hour) })
	close(release)
	res := answer(t, s, id, map[string]any{"text": "late", "channel": "console"})
	if res.Code != http.StatusConflict || res.Body.(map[string]any)["error"] != "expired" {
		t.Fatalf("answer = %d %v", res.Code, res.Body)
	}
	q := s.Get(context.Background(), "thread-a", "reporter", "reporter", id).Body.(Question)
	if q.Status != StatusExpired {
		t.Fatalf("status = %s", q.Status)
	}
}

func twoOptions() []any {
	return []any{map[string]any{"label": "Ship"}, map[string]any{"label": "Wait"}}
}

func keyN(n int) string {
	return "key-" + pad(n) + "-0123456789"
}

func pad(n int) string {
	s := itoa(n)
	for len(s) < 4 {
		s = "0" + s
	}
	return s
}

func fill(limit int) string {
	n := limit - 2
	if n < 0 {
		n = 0
	}
	return repeat('a', n)
}

func loadDoc(t *testing.T, s *Service) document {
	t.Helper()
	raw := readRaw(t, s)
	doc, err := parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return *doc
}

func readRaw(t *testing.T, s *Service) string {
	t.Helper()
	b, err := os.ReadFile(s.store.path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func hasID(t *testing.T, s *Service, id string) bool {
	t.Helper()
	res := s.Get(context.Background(), "thread-a", "reporter", "reporter", id)
	return res.Code == http.StatusOK
}

func auditHas(t *testing.T, s *Service, action, id string) bool {
	t.Helper()
	raw := readAudit(t, s)
	return contains(raw, action) && contains(raw, id)
}

func readAudit(t *testing.T, s *Service) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(filepath.Dir(s.store.path), "audit.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
