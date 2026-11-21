package controller_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/loopalert"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// TestTuningFailuresToAlert: a loop alerts after FailuresToAlert failures in
// a row until a pass reads a tuning rule; then after the rule's
// failures_to_alert.
func TestTuningFailuresToAlert(t *testing.T) {
	pool := setup(t)
	c := &controller.Controller{DB: pool, Logf: func(string, ...any) {}}
	fail := func(name string, n int) {
		t.Helper()
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		i := 0
		c.RunLoopForTest(ctx, name, time.Millisecond, func() error {
			if i == n {
				cancel()
				return errors.New("stopped")
			}
			i++
			return fmt.Errorf("the read failed (%d)", i)
		})
	}
	alerts := func() int {
		t.Helper()
		var n int
		must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'controller_loop_failing'`,
			loopalert.ControllerStream).Scan(&n))
		return n
	}
	if loopalert.FailuresToAlert <= 2 {
		t.Fatalf("FailuresToAlert is %d: the test lowers it to 2", loopalert.FailuresToAlert)
	}
	fail("agentrt-assess", 2)
	if n := alerts(); n != 0 {
		t.Fatalf("%d alerts after two failures with no tuning rule", n)
	}
	// The rule is recorded as agentrttest's rules are, row and text: what a
	// pass reads of it is the store's (the signing is the release policy's,
	// another domain's, and its own tests').
	text := "rule_id: tuning\nrule_version: 1\ndirection: initial\neffective_at: 2026-10-01T00:00:00Z\nexpires_at: 2099-01-01T00:00:00Z\nvalues: {failures_to_alert: 2}\n"
	r, err := rules.Parse([]byte(text))
	must(t, err)
	body, err := r.BodyJSON()
	must(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, signed_by, signature,
	    effective_at, expires_at) VALUES ($1, 1, $2, $3, 'initial', 'test', 'test', '2026-10-01T00:00:00Z', '2099-01-01T00:00:00Z')`,
		rules.KindTuning, string(body), text)
	must(t, err)
	c.ReadTuningForTest(ctx)
	fail("agentrt-landings", 2)
	if n := alerts(); n != 1 {
		t.Fatalf("%d alerts after two failures under failures_to_alert 2; want 1", n)
	}
}
