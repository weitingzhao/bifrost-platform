package controller_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
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
		must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'policy_alert'`,
			store.ControllerStream).Scan(&n))
		return n
	}
	if store.FailuresToAlert <= 2 {
		t.Fatalf("FailuresToAlert is %d: the test lowers it to 2", store.FailuresToAlert)
	}
	fail("agentrt-assess", 2)
	if n := alerts(); n != 0 {
		t.Fatalf("%d alerts after two failures with no tuning rule", n)
	}
	key := rptest.NewKey(t)
	text := "rule_id: tuning\nrule_version: 1\ndirection: initial\neffective_at: 2026-10-01T00:00:00Z\nexpires_at: 2099-01-01T00:00:00Z\nvalues: {failures_to_alert: 2}\n"
	_, err := store.SignRule(ctx, pool, store.Writer{Actor: "owner", Token: "admin"}, rules.KindTuning, []byte(text),
		key.Sign(t, []byte(text), rules.Namespace), key.Fingerprint)
	must(t, err)
	c.ReadTuningForTest(ctx)
	fail("agentrt-landings", 2)
	if n := alerts(); n != 1 {
		t.Fatalf("%d alerts after two failures under failures_to_alert 2; want 1", n)
	}
}
