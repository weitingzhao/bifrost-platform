package approvalfacts

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/readslot"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

// ByNumber reads an approval's fact by its number, a bounded number of reads
// in flight. A record that cannot be read now is ok=false: the controller
// records nothing for it and a fact seen before stays seen.
func ByNumber(lookup func(ref string) (approvals.Approval, bool, error)) func(context.Context, int64) (seenfacts.ApprovalFact, bool) {
	reads := readslot.New(2)
	return func(ctx context.Context, number int64) (seenfacts.ApprovalFact, bool) {
		a, ok, err := readslot.Within(ctx, reads, func() (approvals.Approval, bool, error) { return lookup(strconv.FormatInt(number, 10)) })
		if err != nil || !ok || a.Status == "" {
			return seenfacts.ApprovalFact{}, false
		}
		return Fact(a)
	}
}

// Backfill lists the facts of the approvals the runtime may record, for durable
// backfill, a bounded number of reads in flight.
func Backfill(observations func() ([]approvals.Approval, error)) func(context.Context) ([]seenfacts.ApprovalFact, error) {
	reads := readslot.New(2)
	return func(ctx context.Context) ([]seenfacts.ApprovalFact, error) {
		items, ok, err := readslot.Within(ctx, reads, func() ([]approvals.Approval, bool, error) {
			items, err := observations()
			return items, true, err
		})
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("approval observations unavailable")
		}
		now := time.Now()
		var out []seenfacts.ApprovalFact
		for _, a := range items {
			if f, ok := Observation(a, now); ok {
				out = append(out, f)
			}
		}
		return out, nil
	}
}
