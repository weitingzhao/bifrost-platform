package fullci_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/fullci"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
)

// Origin: 2026-11-17, the full CI's decisions moved out of the controller
// (LANE-BIZ-ALIGN): the zone of the day's time is now bound by the
// controller (planpace.BriefZone), and time.LoadLocation("") is UTC, so an
// unbound zone would start the day's runs at another hour. Unbound, a pass
// starts nothing and says so. The controller's own tests
// (controller/fullci_test.go) are the bound case.
func TestPassWithoutAZoneStartsNothing(t *testing.T) {
	ensured := 0
	f := &fullci.FullCI{
		Runs: func(context.Context, time.Time) ([]landingci.FullRun, error) { return nil, nil },
		Ensure: func(context.Context, string, time.Time) (string, error) {
			ensured++
			return "run", nil
		},
		Main: func(context.Context) (string, error) { return "1111111111111111111111111111111111111111", nil },
	}
	noon := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	n, err := f.Pass(context.Background(), fullci.Host{Now: func() time.Time { return noon }})
	if n != 0 || err == nil || ensured != 0 {
		t.Fatalf("unbound zone: %d started, err %v, %d ensured", n, err, ensured)
	}
	// The positive control: the same pass with a zone starts main's run (its
	// note then fails on a ledger that refuses every transaction).
	n, err = f.Pass(context.Background(), fullci.Host{DB: noLedger{}, Now: func() time.Time { return noon }, Zone: "UTC"})
	if n != 1 || ensured != 1 || err == nil {
		t.Fatalf("bound zone: %d started, err %v, %d ensured", n, err, ensured)
	}
}

// noLedger refuses every transaction.
type noLedger struct{}

func (noLedger) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return nil, errors.New("no ledger here")
}

// Origin: 2026-11-24, platform 0e98b939 reached STG at 20:07 and its PROD
// release waited on a full run started by hand: the day's runs alone (07:30)
// left a commit STG took in the day without the full pass PROD is judged on.
// STG's commit now runs in full when it changes, at any hour, once.
func TestAnSTGChangeRunsInFullAtAnyHourOnce(t *testing.T) {
	const a, b = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	var ensured []string
	f := &fullci.FullCI{
		Runs: func(context.Context, time.Time) ([]landingci.FullRun, error) { return nil, nil },
		Ensure: func(_ context.Context, rev string, _ time.Time) (string, error) {
			ensured = append(ensured, rev)
			return "ci-full-" + rev[:12], nil
		},
		Main: func(context.Context) (string, error) { return "1111111111111111111111111111111111111111", nil },
	}
	stg := a
	early := time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC) // before 07:30
	host := fullci.Host{DB: noLedger{}, Zone: "UTC", Now: func() time.Time { return early },
		STG: func(context.Context) (string, bool) { return stg, true }}
	_, _ = f.Pass(context.Background(), host)
	_, _ = f.Pass(context.Background(), host) // unchanged: not again
	stg = b
	_, _ = f.Pass(context.Background(), host)
	if len(ensured) != 2 || ensured[0] != a || ensured[1] != b {
		t.Fatalf("STG changes before the day's time: ensured %v, want [a b]", ensured)
	}
	// The positive control for the day's runs: past 07:30 main's head starts.
	late := time.Date(2026, 11, 24, 8, 0, 0, 0, time.UTC)
	host.Now = func() time.Time { return late }
	_, _ = f.Pass(context.Background(), host)
	if len(ensured) != 3 || ensured[2] != "1111111111111111111111111111111111111111" {
		t.Fatalf("the day's run: ensured %v", ensured)
	}
}
