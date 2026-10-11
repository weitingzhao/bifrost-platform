package notice_test

import (
	"fmt"
	"math/rand"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentthreads"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalnotify"
	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

// TestPushTextHidesWhatStrippingCannotReveal is the round-2 ratchet. Review 59's
// two reproductions are the explicit cases: on ac68be6 the Look title still
// contains "xbearer abcdefgh", and the Created title still contains the joined
// AWS prefix "AKIAAAAAAAAAAAAAA…". Cleaning after a clip, or stripping before
// the first redaction, is what let them through.
func TestPushTextHidesWhatStrippingCannotReveal(t *testing.T) {
	now := time.Date(2026, 10, 11, 4, 15, 0, 0, time.UTC)
	at := now.Add(-11 * time.Minute)

	lookTitle, lookBody := agentthreads.DefaultConfig().Message(agentthreads.Thread{
		Title: "x\tbearer abcdefgh", Thread: "abcd", Vendor: "claude", Host: "h",
		Event: "after_tool", At: at, TurnStartedAt: at,
	}, now)
	if strings.Contains(lookTitle, "abcdefgh") || strings.Contains(lookBody, "abcdefgh") {
		t.Fatalf("kind look\ninput %q\ntitle %q\nbody %q", "x\tbearer abcdefgh", lookTitle, lookBody)
	}
	if !strings.Contains(lookTitle, "[redacted]") {
		t.Fatalf("kind look\ninput %q\ntitle %q\nbody %q", "x\tbearer abcdefgh", lookTitle, lookBody)
	}

	aws := "AKIA" + strings.Repeat("A", 16)
	summary := strings.Repeat("x", 80) + " AKIA" + strings.Repeat("A", 8) + "\u200b" + strings.Repeat("A", 8)
	created := approvalnotify.Compose(approvalnotify.KindCreated, approvalnotify.Item{
		ID: "appr_x", Number: 7, Summary: summary, Tier: "B",
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}, now)
	if strings.Contains(created.Title, aws) || strings.Contains(created.Message, aws) ||
		strings.Contains(created.Title, "AKIAAAAAAAAAAAAA") || !strings.Contains(created.Title, "[redacted aws key]") {
		t.Fatalf("kind created\ninput %q\ntitle %q\nbody %q", summary, created.Title, created.Message)
	}

	const inputs = 200_000
	shards := runtime.GOMAXPROCS(0)
	if shards < 1 {
		shards = 1
	}
	base, extra := inputs/shards, inputs%shards
	for shard := 0; shard < shards; shard++ {
		shard := shard
		t.Run(fmt.Sprintf("shard-%d", shard), func(t *testing.T) {
			t.Parallel()
			count := base
			if shard < extra {
				count++
			}
			start := shard*base + shard
			if shard >= extra {
				start = shard*base + extra
			}
			rng := rand.New(rand.NewSource(306307 + int64(shard)))
			for n := 0; n < count; n++ {
				i := start + n
				text, secret := agentText(rng, i)
				orig, stripped, kept := hides(text, secret)
				checkPush(t, "bare", text, secret, orig, stripped, kept, notice.PushText(text, false), notice.PushText(text, true))
				checkKinds(t, now, i, text, secret, orig, stripped, kept)
			}
		})
	}
	t.Logf("inputs=%d seed=%d", inputs, 306307)
}

func checkKinds(t *testing.T, now time.Time, n int, text, secret string, orig, stripped, kept bool) {
	t.Helper()
	// One field carries the agent text. Filling every field on every input
	// repeats the same redaction and does not finish under the race detector
	// inside the default ten minutes. Across the corpus each field is the one
	// that carries it, and every kind still runs.
	it := approvalnotify.Item{
		ID: "appr_prop", Number: 7, Action: "gitops_sync_app", Tier: "C",
		CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
	}
	switch n % 8 {
	case 0:
		it.Env = text
	case 1:
		it.Summary = text
	case 2:
		it.KeyParams = map[string]string{"name": text}
	case 3:
		it.Requester = text
	case 4:
		it.Thread = text
	case 5:
		it.WorkID = text
	case 6:
		it.Reason = text
	default:
		it.Error = text
	}
	for _, kind := range []string{
		approvalnotify.KindCreated,
		approvalnotify.KindFailed,
		approvalnotify.KindUnknown,
		approvalnotify.KindNotExecuted,
		approvalnotify.KindWaiting,
		approvalnotify.KindExpiring,
	} {
		m := approvalnotify.Compose(kind, it, now)
		checkPush(t, kind, text, secret, orig, stripped, kept, m.Title, m.Message)
	}
	at := now.Add(-11 * time.Minute)
	th := agentthreads.Thread{Vendor: "claude", Host: "h", Event: "after_tool", At: at, TurnStartedAt: at}
	host := agentthreads.Host{Host: "h", At: at, Vendors: map[string]agentthreads.VendorReport{}}
	switch n % 7 {
	case 0:
		th.Title = text
	case 1:
		th.Title = ""
		th.Thread = text
	case 2:
		th.Vendor = text
	case 3:
		th.Host = text
		host.Host = text
	case 4:
		th.Work = text
	case 5:
		th.Event = agentthreads.Event(text)
	default:
		th.Tool = text
	}
	title, body := agentthreads.DefaultConfig().Message(th, now)
	checkPush(t, "look", text, secret, orig, stripped, kept, title, body)
	hostTitle, hostBody := agentthreads.DefaultConfig().HostMessage(host, []agentthreads.Thread{th}, now)
	checkPush(t, "look-host", text, secret, orig, stripped, kept, hostTitle, hostBody)
	q := questions.Question{Number: 7, ExpiresAt: now.Add(time.Hour)}
	switch n % 4 {
	case 0:
		q.Title = text
	case 1:
		q.Urgency = text
	case 2:
		q.Requester = text
	default:
		q.RequesterThread = text
	}
	title, body = questions.NoticeMessage(q, now)
	checkPush(t, "answer", text, secret, orig, stripped, kept, title, body)
}

// hides reports whether the oracle removed secret. A secret that was never a
// contiguous span was not hidden; stripping may be what joins it.
func hides(text, secret string) (orig, stripped, kept bool) {
	orig = strings.Contains(text, secret) && !strings.Contains(notice.OracleRedact(text), secret)
	noNL := notice.StripControls(text, false)
	withNL := notice.StripControls(text, true)
	stripped = strings.Contains(noNL, secret) && !strings.Contains(notice.OracleRedact(noNL), secret)
	kept = strings.Contains(withNL, secret) && !strings.Contains(notice.OracleRedact(withNL), secret)
	return orig, stripped, kept
}

func checkPush(t *testing.T, kind, input, secret string, orig, stripped, kept bool, title, body string) {
	t.Helper()
	if notice.PushText(title, false) != title || notice.PushText(body, true) != body ||
		(orig && (strings.Contains(title, secret) || strings.Contains(body, secret))) ||
		(stripped && strings.Contains(title, secret)) ||
		(kept && strings.Contains(body, secret)) {
		t.Fatalf("kind %s\ninput %q\ntitle %q\nbody %q", kind, input, title, body)
	}
}

func agentText(rng *rand.Rand, n int) (text, secret string) {
	keywords := []string{
		"password", "passwd", "pwd", "secret", "token",
		"apikey", "api_key", "api-key", "access_key", "private_key", "credential",
		"to\u212aen", "pa\u017fsword",
	}
	schemes := []string{"bearer", "basic", "Bearer", "Basic", "BeArEr", "Ba\u017fic"}
	seps := []string{"=", ":", " = ", "='", "=\""}
	ws := []string{" ", "\t", "\n", "  "}
	secrets := []string{
		"QQQQQQQQ",
		"QQQQ.QQQQ",
		"AKIAAAAAAAAAAAAAAAAA",
		"ASIABBBBBBBBBBBBBBBB",
		"ghp_" + strings.Repeat("a", 20),
		"xoxb-" + strings.Repeat("c", 10),
		"eyJQQQQQQQQ.RRRRRRRR.SSSSSSSS",
	}
	secret = secrets[rng.Intn(len(secrets))]
	switch rng.Intn(3) {
	case 0:
		text = keywords[rng.Intn(len(keywords))] + seps[rng.Intn(len(seps))] + secret
	case 1:
		text = schemes[rng.Intn(len(schemes))] + ws[rng.Intn(len(ws))] + secret
	default:
		text = "note " + keywords[rng.Intn(len(keywords))] + "=" + secret + " end"
	}
	removed := removedClass()
	text = spliceRune(text, indexInside(text, secret, rng), removed[rng.Intn(len(removed))])
	if kw := keywords[n%len(keywords)]; strings.Contains(text, kw) {
		at := utf8.RuneCountInString(text[:strings.Index(text, kw)]) + 1 + rng.Intn(max(1, utf8.RuneCountInString(kw)-1))
		text = spliceRune(text, at, removed[(n+3)%len(removed)])
	}
	if sp := strings.IndexByte(text, ' '); sp >= 0 {
		text = spliceRune(text, sp, removed[(n+7)%len(removed)])
	}
	limits := []int{60, 100, 140, 160}
	limit := limits[n%len(limits)]
	want := limit + []int{-8, -1, 0, 1, 8}[rng.Intn(5)]
	if want < 8 {
		want = 8
	}
	pad := want - utf8.RuneCountInString(text)
	if pad > 0 {
		side := rng.Intn(pad + 1)
		text = strings.Repeat("x", side) + text + strings.Repeat("y", pad-side)
	}
	text = spliceRune(text, limit, removed[n%len(removed)])
	return text, secret
}

func removedClass() []rune {
	out := []rune{'\n', '\r', '\t', 0x00, 0x01, 0x07, 0x0b, 0x0c, 0x1b, 0x1f, 0x7f, 0x80, 0x85, 0x9f}
	out = append(out,
		0x061c, 0x200e, 0x200f,
		0x202a, 0x202b, 0x202c, 0x202d, 0x202e,
		0x2066, 0x2067, 0x2068, 0x2069,
		0x180e, 0x200b, 0x200c, 0x200d, 0x2060, 0xfeff,
		0x2028, 0x2029,
	)
	return out
}

func indexInside(s, secret string, rng *rand.Rand) int {
	at := strings.Index(s, secret)
	if at < 0 {
		return utf8.RuneCountInString(s) / 2
	}
	width := utf8.RuneCountInString(secret)
	if width < 2 {
		return at
	}
	return utf8.RuneCountInString(s[:at]) + 1 + rng.Intn(width-1)
}

func spliceRune(s string, runeAt int, r rune) string {
	rs := []rune(s)
	if len(rs) == 0 {
		return string(r)
	}
	if runeAt < 0 {
		runeAt = 0
	}
	if runeAt > len(rs) {
		runeAt = len(rs)
	}
	rs = append(rs, 0)
	copy(rs[runeAt+1:], rs[runeAt:])
	rs[runeAt] = r
	return string(rs)
}
