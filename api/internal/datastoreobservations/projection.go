// Package datastoreobservations projects already-read health and ordered TCP fallbacks.
// It has no database, identity, authorization or environment configuration access.
package datastoreobservations

import (
	"context"
	"strings"
)

type Row struct{ Reach, Detail, LAN string }
type Observation struct{ Reach, Detail, URL string }

func Project(ctx context.Context, envID, cfgAddr, id string, read func() (Row, bool), observeTCP func(context.Context, string, string, string) Observation) Observation {
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
					return Observation{
						Reach:  "ok",
						Detail: detail + " · tcp_lan verified", URL: url,
					}
				}
				detail = detail + " · tcp_lan: " + tcp.Detail
			}
		}
		if reach == "ok" || reach == "degraded" {
			return Observation{
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
