package store_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// routingRule is routing:dev at version v (90 % quota stop), with a
// competence threshold when score is not negative, as a row a test inserts.
func routingRule(v int, score, samples int) string {
	direction := "initial"
	if v > 1 {
		direction = "loosen"
	}
	body := fmt.Sprintf(`{"direction":%q,"effective_at":"2026-01-01T00:00:00Z","expires_at":"2100-01-01T00:00:00Z",`, direction)
	text := fmt.Sprintf("rule_id: routing:dev\nrule_version: %d\ndirection: %s\neffective_at: 2026-01-01T00:00:00Z\nexpires_at: 2100-01-01T00:00:00Z\ntenant: dev\norder: [capability, quota]\nquota_stop_percent: 90\n", v, direction)
	if score >= 0 {
		body += fmt.Sprintf(`"min_samples":%d,"min_score_percent":%d,`, samples, score)
		text += fmt.Sprintf("min_score_percent: %d\nmin_samples: %d\n", score, samples)
	}
	body += fmt.Sprintf(`"order":["capability","quota"],"quota_stop_percent":90,"rule_id":"routing:dev","rule_version":%d,"tenant":"dev"}`, v)
	return fmt.Sprintf(`INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, signed_by, signature,
	  effective_at, expires_at) VALUES ('routing:dev', %d, '%s', E'%s', '%s', 'test', 'test', '2026-01-01T00:00:00Z', '2100-01-01T00:00:00Z')`,
		v, body, strings.ReplaceAll(text, "\n", `\n`), direction)
}

// reading records the Owner's reading of a weekly pool: used, resetting in left.
func (w *world) reading(t *testing.T, pool string, used float64, left time.Duration) {
	t.Helper()
	reset := time.Now().Add(left).UTC().Truncate(time.Second)
	_, err := store.RecordOwnerReading(ctx, w.rt, store.Writer{Actor: "owner"}, pool+fmt.Sprint(used),
		store.PoolReading{Pool: pool, UsedFraction: pv(used), ResetsAt: &reset})
	ok(t, err)
}

// choice is what the attempt_leased event of a claim records of its pick.
func (w *world) choice(t *testing.T, c *store.Claim) map[string]any {
	t.Helper()
	var body map[string]any
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json FROM agentrt.event WHERE event_type = 'attempt_leased' AND attempt_id = $1`,
		c.AttemptID).Scan(&body))
	ch, _ := body["choice"].(map[string]any)
	return ch
}

// W-31 2.4 slice ② ⑥: of the tools the route allows, the one whose pool is
// furthest behind its window goes first; claude comes first in the route.
func TestPickByPace(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.ready(t, store.Task{})
	w.reading(t, "claude:owner:weekly", 0.9, 24*time.Hour)  // pace ≈ +0.04
	w.reading(t, "codex:owner:weekly", 0.5, 6*24*time.Hour) // pace ≈ +0.36
	w.reading(t, "codex:owner:short", 0.1, 4*time.Hour)     // the fuller window counts
	c := w.claim(t, "h1", store.LeaseTTL, codex, claude)
	if c == nil || c.Tool.Executor != "exec:claude" {
		t.Fatalf("claim = %+v; want claude, behind its window", c)
	}
	ch := w.choice(t, c)
	if ch["by"] != "pace" || ch["candidate"] != "implementer/exec:claude/anthropic/*" || ch["options"] != float64(2) {
		t.Fatalf("choice = %v", ch)
	}
	if p, _ := ch["pace_permille"].(float64); p < 30 || p > 50 {
		t.Fatalf("pace_permille = %v", ch["pace_permille"])
	}
	w.ready(t, store.Task{})
	w.reading(t, "claude:owner:weekly", 0.99, 6*24*time.Hour) // pace ≈ +0.85, now ahead of codex
	if c = w.claim(t, "h2", store.LeaseTTL, claude, codex); c == nil || c.Tool.Executor != "exec:codex" {
		t.Fatalf("claim = %+v; want codex once claude is ahead", c)
	}
}

// ⑥: a pool used to the routing rule's quota stop is passed by even when it
// is behind its window, and a reading with no reset still counts for it.
func TestPickPassesAPoolAtTheQuotaStop(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.legacy(t, routingRule(1, -1, 0))
	w.ready(t, store.Task{})
	w.reading(t, "claude:owner:weekly", 0.95, time.Hour) // pace ≈ −0.04, but 95 % used
	w.reading(t, "codex:owner:weekly", 0.5, 6*24*time.Hour)
	if c := w.claim(t, "h1", store.LeaseTTL, claude); c != nil {
		t.Fatalf("claude at 95 %% took %+v", c)
	}
	c := w.claim(t, "h1", store.LeaseTTL, claude, codex)
	if c == nil || c.Tool.Executor != "exec:codex" {
		t.Fatalf("claim = %+v; want codex", c)
	}
	ch := w.choice(t, c)
	if fmt.Sprint(ch["passed_by_quota"]) != "[implementer/exec:claude/anthropic/*]" || ch["by"] != "route" {
		t.Fatalf("choice = %v", ch)
	}
	// The Owner reads codex at 92 % and gives no reset: it counts for the stop.
	w.ready(t, store.Task{})
	_, err := store.RecordOwnerReading(ctx, w.rt, store.Writer{Actor: "owner"}, "no-reset",
		store.PoolReading{Pool: "codex:owner:short", UsedFraction: pv(0.92)})
	ok(t, err)
	if c := w.claim(t, "h2", store.LeaseTTL, claude, codex); c != nil {
		t.Fatalf("both at the stop, %s took the task", c.Tool.Executor)
	}
}

// ⑦: with nothing read, the executor that took work longest ago goes first.
func TestPickRotates(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.ready(t, store.Task{})
	w.ready(t, store.Task{})
	first := w.claim(t, "h1", store.LeaseTTL, codex, claude)
	if first == nil || first.Tool.Executor != "exec:claude" || w.choice(t, first)["by"] != "route" {
		t.Fatalf("first = %+v", first)
	}
	second := w.claim(t, "h2", store.LeaseTTL, codex, claude)
	if second == nil || second.Tool.Executor != "exec:codex" || w.choice(t, second)["by"] != "rotation" {
		t.Fatalf("second = %+v (%v)", second, w.choice(t, second))
	}
}

// ⑤: without a threshold the route records nothing of competence; with one,
// a candidate below it over enough samples is excluded (with its sample), a
// new computation decides the route again, and the record replays.
func TestCompetenceThreshold(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	for i := 1; i <= 3; i++ {
		w.ready(t, store.Task{})
		w.conclude(t, fmt.Sprintf("h%d", i), codex, "rejected")
		w.ready(t, store.Task{})
		w.conclude(t, fmt.Sprintf("h%d", i+3), claude, "accepted")
	}
	_, err := store.ComputeScores(ctx, w.rt)
	ok(t, err)
	task := w.ready(t, store.Task{})
	body := w.lastRoute(t, task)
	if _, on := body["inputs"].(map[string]any)["competence"]; on || !strings.Contains(fmt.Sprint(body["candidates"]), "implementer/exec:codex/openai/*") {
		t.Fatalf("without a threshold: %v", body)
	}
	w.legacy(t, routingRule(1, 15, 3))
	n, err := store.Reroute(ctx, w.rt)
	ok(t, err)
	body = w.lastRoute(t, task)
	if n == 0 || strings.Contains(fmt.Sprint(body["candidates"]), "implementer/exec:codex/openai/*") ||
		!strings.Contains(fmt.Sprint(body["candidates"]), "implementer/exec:claude/anthropic/*") ||
		!strings.Contains(fmt.Sprint(body["candidates"]), "implementer/exec:cursor/cursor-mix/*") {
		t.Fatalf("with the threshold (%d rerouted): %v", n, body["candidates"])
	}
	var codexOut map[string]any
	for _, e := range body["excluded"].([]any) {
		if m := e.(map[string]any); m["candidate"] == "implementer/exec:codex/openai/*" {
			codexOut = m
		}
	}
	if codexOut["reason"] != "below the competence threshold" || fmt.Sprint(codexOut["sample"]) != "map[attempts:3 first_pass:0]" {
		t.Fatalf("codex excluded as %v", codexOut)
	}
	route, excluded, fp, err := store.ReplayRoute(body["inputs"].(map[string]any))
	ok(t, err)
	got, _ := json.Marshal([]any{route, excluded})
	want, _ := json.Marshal([]any{body["candidates"], body["excluded"]})
	if string(got) != string(want) || fp != body["fingerprint"] {
		t.Fatalf("replayed %s (%s), recorded %s (%v)", got, fp, want, body["fingerprint"])
	}
	// Nothing new: nothing decided again.
	if n, err = store.Reroute(ctx, w.rt); err != nil || n != 0 {
		t.Fatalf("a pass with nothing new rerouted %d (%v)", n, err)
	}
	// A claim checks it again: codex is not handed the task.
	if c := w.claim(t, "h7", store.LeaseTTL, codex); c != nil {
		t.Fatalf("codex below the threshold took %+v", c)
	}
	// Claude fails six more (3 first passes of 9 is below 15 %) once a
	// computation counts them: the route is decided again without it.
	for i := 9; i <= 14; i++ {
		ok(t, store.RegisterHost(ctx, w.rt, "owner", fmt.Sprintf("h%d", i), "dev"))
	}
	var left string
	for i := 0; i < 7; i++ {
		left = w.ready(t, store.Task{})
	}
	for i := 9; i <= 14; i++ {
		w.conclude(t, fmt.Sprintf("h%d", i), claude, "rejected")
	}
	_, err = store.ComputeScores(ctx, w.rt)
	ok(t, err)
	if n, err = store.Reroute(ctx, w.rt); err != nil || n == 0 {
		t.Fatalf("a new computation rerouted %d (%v)", n, err)
	}
	if body = w.lastRoute(t, left); strings.Contains(fmt.Sprint(body["candidates"]), "implementer/exec:claude/anthropic/*") {
		t.Fatalf("claude at 3 of 9 still routed: %v", body["candidates"])
	}
}

// Review 286 B5: a monthly reading whose reset is further off than 30 days
// still meets the quota stop (its pace counts as 0).
func TestALongMonthStillStops(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.legacy(t, routingRule(1, -1, 0))
	w.ready(t, store.Task{})
	w.reading(t, "codex:owner:monthly", 0.95, 31*24*time.Hour)
	if c := w.claim(t, "h1", store.LeaseTTL, codex); c != nil {
		t.Fatalf("codex at 95 %% of its month took %+v", c)
	}
	if c := w.claim(t, "h1", store.LeaseTTL, codex, claude); c == nil || c.Tool.Executor != "exec:claude" {
		t.Fatalf("claim = %+v; want claude", c)
	}
}

// measuredAgo dates the latest reading of pool back to ago.
func (w *world) measuredAgo(t *testing.T, pool string, ago time.Duration) {
	t.Helper()
	w.legacy(t, fmt.Sprintf(`UPDATE agentrt.vendor_pool SET measured_at = clock_timestamp() - make_interval(secs => %d)
	  WHERE vendor_pool_id = '%s'`, int(ago.Seconds()), pool))
}

// PROD 11-02: a pool is read again only by an attempt that ran on it, so a
// reading at the quota stop holds work back only while it is recent. Measured
// now or not quite three hours ago it holds; older, it counts as not read
// (pace 0, so claude, behind codex's pace, goes first), a claim goes through
// and its attempt reads the pool again. A cooldown holds however old the
// reading.
func TestAQuotaStopReadingHoldsOnlyWhileRecent(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.legacy(t, routingRule(1, -1, 0))
	w.ready(t, store.Task{})
	w.reading(t, "claude:owner:weekly", 0.99, 7*24*time.Hour-time.Hour) // 45 minutes after its reset, as on PROD
	w.reading(t, "codex:owner:weekly", 0.5, 6*24*time.Hour)             // pace ≈ +0.36
	if c := w.claim(t, "h1", store.LeaseTTL, claude); c != nil {
		t.Fatalf("claude at 99 %% measured now took %+v", c)
	}
	w.measuredAgo(t, "claude:owner:weekly", 3*time.Hour-10*time.Minute)
	if c := w.claim(t, "h1", store.LeaseTTL, claude); c != nil {
		t.Fatalf("claude at 99 %% measured 2 h 50 min ago took %+v", c)
	}
	w.measuredAgo(t, "claude:owner:weekly", 3*time.Hour+time.Minute)
	c := w.claim(t, "h1", store.LeaseTTL, codex, claude)
	if c == nil || c.Tool.Executor != "exec:claude" {
		t.Fatalf("claim = %+v; want claude, its reading older than three hours", c)
	}
	ch := w.choice(t, c)
	if _, read := ch["pace_permille"]; read || ch["by"] != "pace" || fmt.Sprint(ch["passed_by_quota"]) != "[]" {
		t.Fatalf("choice = %v; want claude picked as not read", ch)
	}

	// The Owner reads claude spent, resetting in an hour: it cools down, and
	// the cooldown holds however old the reading is.
	w.ready(t, store.Task{})
	w.reading(t, "claude:owner:weekly", 1, time.Hour)
	w.measuredAgo(t, "claude:owner:weekly", 4*time.Hour)
	if c := w.claim(t, "h2", store.LeaseTTL, claude); c != nil {
		t.Fatalf("claude cooling down took %+v", c)
	}
	if c := w.claim(t, "h2", store.LeaseTTL, claude, codex); c == nil || c.Tool.Executor != "exec:codex" {
		t.Fatalf("claim = %+v; want codex while claude cools down", c)
	}
}

// An event that names no fraction (an "allowed" seven_day without its
// utilization) dates nothing: the old 99 % keeps its own age and stops
// holding after three hours, instead of being stamped fresh by the run it
// let through (review of 61c28c4c).
func TestAReadingWithNoFractionKeepsTheOldReadingsAge(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.legacy(t, routingRule(1, -1, 0))
	w.ready(t, store.Task{})
	w.reading(t, "claude:owner:weekly", 0.99, 7*24*time.Hour-time.Hour)
	w.measuredAgo(t, "claude:owner:weekly", 3*time.Hour+time.Minute)
	f := w.started(t, "h1", claude)
	reset := time.Now().Add(6 * 24 * time.Hour)
	_, err := store.ComputedWith(ctx, w.rt, f, store.Ending{ExitCode: pv(0), EndHow: "exit", Conclusion: "done",
		Pools: []store.PoolReading{{Pool: "claude:owner:weekly", ResetsAt: &reset}}})
	ok(t, err)
	w.ready(t, store.Task{})
	if c := w.claim(t, "h2", store.LeaseTTL, claude); c == nil {
		t.Fatal("claude held again: an event with no fraction made the old 99 % look freshly measured")
	}
}
