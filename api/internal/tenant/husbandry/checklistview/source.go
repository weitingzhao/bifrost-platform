package checklistview

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/checklist"
	husbandry "github.com/weitingzhao/bifrost-platform/api/internal/tenant/husbandry"
)

// Signals maps Market/Flex/Research husbandry lanes to checklist items.
// Market due/draining is ok (not Operate); missed/degraded fails. Flex
// source=none or degraded fails. Batch missed/degraded via research_olap fails.
func Signals(snap husbandry.Snapshot) []checklist.ItemSignal {
	projected := Project(snap)
	out := make([]checklist.ItemSignal, 0, len(projected))
	for _, s := range projected {
		out = append(out, checklist.ItemSignal{ItemID: s.ItemID, Signal: s.Signal, Detail: s.Detail, Env: s.Env})
	}
	return out
}

// Snapshotter is the husbandry service as this adapter reads it.
type Snapshotter interface {
	Snapshot(ctx context.Context) husbandry.Snapshot
}

type source struct{ s Snapshotter }

func (a source) Signals(ctx context.Context) []checklist.ItemSignal {
	return Signals(a.s.Snapshot(ctx))
}

// Source is the tenant's husbandry lanes as the checklist's SignalSource:
// the tenant depends on the checklist's shape, not the other way round.
func Source(s Snapshotter) checklist.SignalSource { return source{s} }
