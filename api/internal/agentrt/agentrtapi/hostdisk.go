package agentrtapi

import (
	"context"
	"fmt"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// pageLimit bounds a page to the Owner's phone; it is sent apart from the
// claim, which never waits for it.
const pageLimit = 15 * time.Second

// pageDisk pages the Owner when a host's free disk went below the floor or
// is back above (store.RecordHostDisk said changed); the record is the
// platform's, a page that fails is logged.
func (a *API) pageDisk(host, changed string, d store.HostDisk) {
	if changed == "" || a.NotifyOwner == nil {
		return
	}
	floor := a.DiskFloor
	if floor <= 0 {
		floor = store.DefaultDiskFloor
	}
	free := fmt.Sprintf("free %.1f GiB on the host's root volume, %.1f GiB on agentd's root volume, the floor %.1f GiB",
		float64(d.Host)/(1<<30), float64(d.Root)/(1<<30), float64(floor)/(1<<30))
	title, body := "Host "+host+" is low on disk", "It takes no new work until it has room again: "+free+"."
	if changed == "above" {
		title, body = "Host "+host+" has room on disk again", "It takes work again: "+free+"."
	}
	safego.Go("agentrt-disk-page", func() {
		ctx, cancel := context.WithTimeout(context.Background(), pageLimit)
		defer cancel()
		if err := a.NotifyOwner(ctx, title, body, "disk:"+host+":"+changed); err != nil && a.Logf != nil {
			a.Logf("agentrt: the page that host %s's disk went %s the floor failed: %v", host, changed, err)
		}
	})
}
