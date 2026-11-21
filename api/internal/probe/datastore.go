package probe

import (
	"context"
	"fmt"

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
	return p.datastoreObservation(ctx, envID, cfgAddr, "postgres", func() (datastoreRow, bool) {
		row, ok := ds.envReach(envID)
		return datastoreRow{Reach: string(row.Postgres), Detail: row.PostgresDetail, LAN: row.PostgresLAN}, ok
	})
}

func (p *Prober) probeRedis(ctx context.Context, envID string, cfgAddr string, ds *DatastoreSnapshot) Target {
	return p.datastoreObservation(ctx, envID, cfgAddr, "redis", func() (datastoreRow, bool) {
		row, ok := ds.envReach(envID)
		return datastoreRow{Reach: string(row.Redis), Detail: row.RedisDetail, LAN: row.RedisLAN}, ok
	})
}

func (p *Prober) datastoreObservation(ctx context.Context, envID, cfgAddr, id string, read func() (datastoreRow, bool)) Target {
	observation := projectDatastore(ctx, envID, cfgAddr, id, read, func(ctx context.Context, id, category, addr string) datastoreReading {
		target := p.probeTCP(ctx, id, category, addr)
		return datastoreReading{Reach: string(target.Reachability), Detail: target.Detail, URL: target.URL}
	})
	return Target{ID: id, Category: "datastore", Reachability: Reachability(observation.Reach), Auth: AuthSkipped, AuthorizationLevel: "L0", Detail: observation.Detail, URL: observation.URL}
}

func postgresCfgAddr(env config.Environment) string {
	return fmt.Sprintf("%s:%d", env.Postgres.Host, env.Postgres.Port)
}

func redisCfgAddr(env config.Environment) string {
	return fmt.Sprintf("%s:%d", env.Redis.Host, env.Redis.Port)
}
