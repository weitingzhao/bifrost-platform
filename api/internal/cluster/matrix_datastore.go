package cluster

import (
	"context"
	"fmt"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// DatastoreSnapshot builds matrix datastore reachability from cluster status APIs.
// platform-api on Mac cannot resolve *.svc.cluster.local — matrix uses this instead of raw TCP.
func (s *Service) DatastoreSnapshot(ctx context.Context) probe.DatastoreSnapshot {
	pg := s.PostgresStatus(ctx)
	redis := s.RedisStatus(ctx)
	return BuildMatrixDatastoreSnapshot(pg, redis)
}

// BuildMatrixDatastoreSnapshot maps cluster/postgres + cluster/redis into per-env matrix signals.
func BuildMatrixDatastoreSnapshot(pg PostgresStatusResponse, redis RedisStatusResponse) probe.DatastoreSnapshot {
	snap := probe.DatastoreSnapshot{ByEnv: map[string]probe.DatastoreEnvReach{}}

	for _, db := range pg.Databases {
		row := snap.ByEnv[db.Environment]
		row.Postgres = db.Reach
		row.PostgresDetail = fmt.Sprintf("database %s (%s)", db.Name, db.Detail)
		snap.ByEnv[db.Environment] = row
	}

	if pg.Reachability == probe.ReachFail {
		for env, row := range snap.ByEnv {
			row.Postgres = probe.ReachFail
			row.PostgresDetail = pg.Summary
			snap.ByEnv[env] = row
		}
	} else if pg.Reachability == probe.ReachDegraded {
		for env, row := range snap.ByEnv {
			if row.Postgres == probe.ReachOK {
				row.Postgres = probe.ReachDegraded
				row.PostgresDetail = pg.Summary
				snap.ByEnv[env] = row
			}
		}
	}

	if pg.LanAccess.Endpoint != "" {
		for env, row := range snap.ByEnv {
			row.PostgresLAN = pg.LanAccess.Endpoint
			snap.ByEnv[env] = row
		}
	}

	for _, ep := range redis.EnvEndpoints {
		row := snap.ByEnv[ep.Environment]
		row.Redis = worstRedisReach(ep.LiveReach, ep.QueueReach)
		row.RedisDetail = fmt.Sprintf("live=%s queue=%s · %s", ep.LiveReach, ep.QueueReach, ep.Detail)
		snap.ByEnv[ep.Environment] = row
	}

	if redis.Reachability == probe.ReachFail {
		for env, row := range snap.ByEnv {
			row.Redis = probe.ReachFail
			row.RedisDetail = redis.Summary
			snap.ByEnv[env] = row
		}
	}

	redisLANByEnv := map[string]string{}
	for _, lan := range redis.LanEndpoints {
		if !lan.Available || lan.Endpoint == "" {
			continue
		}
		switch lan.Role {
		case "live", "live+queue":
			if _, exists := redisLANByEnv[lan.Environment]; !exists {
				redisLANByEnv[lan.Environment] = lan.Endpoint
			}
		}
	}
	for env, endpoint := range redisLANByEnv {
		row := snap.ByEnv[env]
		row.RedisLAN = endpoint
		snap.ByEnv[env] = row
	}

	for _, env := range []string{"dev", "stg", "prod"} {
		if _, ok := snap.ByEnv[env]; !ok {
			snap.ByEnv[env] = probe.DatastoreEnvReach{}
		}
	}

	return snap
}

func worstRedisReach(live, queue probe.Reachability) probe.Reachability {
	if live == probe.ReachFail || queue == probe.ReachFail {
		return probe.ReachFail
	}
	if live == probe.ReachDegraded || queue == probe.ReachDegraded {
		return probe.ReachDegraded
	}
	if live == probe.ReachUnknown || queue == probe.ReachUnknown {
		return probe.ReachUnknown
	}
	return probe.ReachOK
}
