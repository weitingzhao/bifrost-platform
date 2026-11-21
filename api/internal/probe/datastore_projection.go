package probe

// The datastore projection (folded in from datastoreobservations 2026-11-20,
// unchanged but for unexported names) projects already-read health and ordered
// TCP fallbacks. It has no database, identity, authorization or environment
// configuration access.

import (
	"context"
	"strings"
)

type datastoreRow struct{ Reach, Detail, LAN string }
type datastoreReading struct{ Reach, Detail, URL string }

func projectDatastore(ctx context.Context, envID, cfgAddr, id string, read func() (datastoreRow, bool), observeTCP func(context.Context, string, string, string) datastoreReading) datastoreReading {
	const category = "datastore"
	if row, ok := read(); ok && row.Reach != "" {
		reach := row.Reach
		detail := "cluster_api: " + row.Detail
		url := "cluster://data/" + id + "/" + envID
		if row.LAN != "" {
			url = "tcp://" + row.LAN
			if reach != "ok" {
				tcp := observeTCP(ctx, id, category, row.LAN)
				if tcp.Reach == "ok" {
					return datastoreReading{
						Reach:  "ok",
						Detail: detail + " · tcp_lan verified", URL: url,
					}
				}
				detail = detail + " · tcp_lan: " + tcp.Detail
			}
		}
		if reach == "ok" || reach == "degraded" {
			return datastoreReading{
				Reach:  reach,
				Detail: detail, URL: url,
			}
		}
	}

	if row, ok := read(); ok && row.LAN != "" {
		tcp := observeTCP(ctx, id, category, row.LAN)
		if tcp.Reach == "ok" {
			tcp.Detail = "tcp_lan: " + tcp.Detail
			return tcp
		}
	}

	tcp := observeTCP(ctx, id, category, cfgAddr)
	if strings.Contains(cfgAddr, ".svc.cluster.local") {
		tcp.Detail = "tcp_config (in-cluster DNS — use cluster_api from Mac host): " + tcp.Detail
	} else {
		tcp.Detail = "tcp_config: " + tcp.Detail
	}
	return tcp
}
