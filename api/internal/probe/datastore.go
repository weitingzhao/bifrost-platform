package probe

import (
	"context"
	"fmt"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

// DatastoreEnvReach carries per-environment datastore health from cluster APIs.
type DatastoreEnvReach struct {
	Postgres       Reachability
	PostgresDetail string
	Redis          Reachability
	RedisDetail    string
	PostgresLAN    string // host:port TCP fallback
	RedisLAN       string // host:port TCP fallback (live instance)
}

// DatastoreSnapshot is built once per matrix request from cluster/postgres + cluster/redis.
type DatastoreSnapshot struct {
	ByEnv map[string]DatastoreEnvReach
}

func (ds *DatastoreSnapshot) envReach(envID string) (DatastoreEnvReach, bool) {
	if ds == nil || ds.ByEnv == nil {
		return DatastoreEnvReach{}, false
	}
	row, ok := ds.ByEnv[envID]
	return row, ok
}

func (p *Prober) probePostgres(ctx context.Context, envID string, cfgAddr string, ds *DatastoreSnapshot) Target {
	const id = "postgres"
	const category = "datastore"

	if row, ok := ds.envReach(envID); ok && row.Postgres != "" {
		reach := row.Postgres
		detail := "cluster_api: " + row.PostgresDetail
		url := "cluster://data/postgres/" + envID
		if row.PostgresLAN != "" {
			url = "tcp://" + row.PostgresLAN
			if reach != ReachOK {
				tcp := p.probeTCP(ctx, id, category, row.PostgresLAN)
				if tcp.Reachability == ReachOK {
					return Target{
						ID: id, Category: category, Reachability: ReachOK,
						Auth: AuthSkipped, AuthorizationLevel: "L0",
						Detail: detail + " · tcp_lan verified", URL: url,
					}
				}
				detail = detail + " · tcp_lan: " + tcp.Detail
			}
		}
		if reach == ReachOK || reach == ReachDegraded {
			return Target{
				ID: id, Category: category, Reachability: reach,
				Auth: AuthSkipped, AuthorizationLevel: "L0",
				Detail: detail, URL: url,
			}
		}
	}

	if row, ok := ds.envReach(envID); ok && row.PostgresLAN != "" {
		tcp := p.probeTCP(ctx, id, category, row.PostgresLAN)
		if tcp.Reachability == ReachOK {
			tcp.Detail = "tcp_lan: " + tcp.Detail
			return tcp
		}
	}

	tcp := p.probeTCP(ctx, id, category, cfgAddr)
	if strings.Contains(cfgAddr, ".svc.cluster.local") {
		tcp.Detail = "tcp_config (in-cluster DNS — use cluster_api from Mac host): " + tcp.Detail
	} else {
		tcp.Detail = "tcp_config: " + tcp.Detail
	}
	return tcp
}

func (p *Prober) probeRedis(ctx context.Context, envID string, cfgAddr string, ds *DatastoreSnapshot) Target {
	const id = "redis"
	const category = "datastore"

	if row, ok := ds.envReach(envID); ok && row.Redis != "" {
		reach := row.Redis
		detail := "cluster_api: " + row.RedisDetail
		url := "cluster://data/redis/" + envID
		if row.RedisLAN != "" {
			url = "tcp://" + row.RedisLAN
			if reach != ReachOK {
				tcp := p.probeTCP(ctx, id, category, row.RedisLAN)
				if tcp.Reachability == ReachOK {
					return Target{
						ID: id, Category: category, Reachability: ReachOK,
						Auth: AuthSkipped, AuthorizationLevel: "L0",
						Detail: detail + " · tcp_lan verified", URL: url,
					}
				}
				detail = detail + " · tcp_lan: " + tcp.Detail
			}
		}
		if reach == ReachOK || reach == ReachDegraded {
			return Target{
				ID: id, Category: category, Reachability: reach,
				Auth: AuthSkipped, AuthorizationLevel: "L0",
				Detail: detail, URL: url,
			}
		}
	}

	if row, ok := ds.envReach(envID); ok && row.RedisLAN != "" {
		tcp := p.probeTCP(ctx, id, category, row.RedisLAN)
		if tcp.Reachability == ReachOK {
			tcp.Detail = "tcp_lan: " + tcp.Detail
			return tcp
		}
	}

	tcp := p.probeTCP(ctx, id, category, cfgAddr)
	if strings.Contains(cfgAddr, ".svc.cluster.local") {
		tcp.Detail = "tcp_config (in-cluster DNS — use cluster_api from Mac host): " + tcp.Detail
	} else {
		tcp.Detail = "tcp_config: " + tcp.Detail
	}
	return tcp
}

func postgresCfgAddr(env config.Environment) string {
	return fmt.Sprintf("%s:%d", env.Postgres.Host, env.Postgres.Port)
}

func redisCfgAddr(env config.Environment) string {
	return fmt.Sprintf("%s:%d", env.Redis.Host, env.Redis.Port)
}
