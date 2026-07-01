package cluster

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func TestBuildMatrixDatastoreSnapshot_PerEnv(t *testing.T) {
	pg := PostgresStatusResponse{
		Reachability: probe.ReachOK,
		LanAccess:    PostgresLanAccessView{Endpoint: "192.168.10.73:30432", Reach: probe.ReachOK},
		Databases: []PostgresDatabaseView{
			{Name: "bifrost_dev", Environment: "dev", Reach: probe.ReachOK, Detail: "ok"},
			{Name: "bifrost_stg", Environment: "stg", Reach: probe.ReachOK, Detail: "ok"},
			{Name: "bifrost_prod", Environment: "prod", Reach: probe.ReachOK, Detail: "ok"},
		},
	}
	redis := RedisStatusResponse{
		Reachability: probe.ReachOK,
		EnvEndpoints: []RedisEnvEndpointView{
			{Environment: "dev", LiveReach: probe.ReachOK, QueueReach: probe.ReachOK},
			{Environment: "stg", LiveReach: probe.ReachOK, QueueReach: probe.ReachOK},
			{Environment: "prod", LiveReach: probe.ReachOK, QueueReach: probe.ReachOK},
		},
		LanEndpoints: []RedisLanEndpointView{
			{Environment: "dev", Role: "live+queue", Endpoint: "192.168.10.73:30379", Available: true},
			{Environment: "prod", Role: "live", Endpoint: "192.168.10.73:30382", Available: true},
		},
	}

	snap := BuildMatrixDatastoreSnapshot(pg, redis)
	for _, env := range []string{"dev", "stg", "prod"} {
		row, ok := snap.ByEnv[env]
		if !ok {
			t.Fatalf("missing env %s", env)
		}
		if row.Postgres != probe.ReachOK {
			t.Fatalf("%s postgres: %s", env, row.Postgres)
		}
		if row.Redis != probe.ReachOK {
			t.Fatalf("%s redis: %s", env, row.Redis)
		}
		if row.PostgresLAN != "192.168.10.73:30432" {
			t.Fatalf("%s postgres LAN: %q", env, row.PostgresLAN)
		}
	}
	if snap.ByEnv["dev"].RedisLAN != "192.168.10.73:30379" {
		t.Fatalf("dev redis LAN: %q", snap.ByEnv["dev"].RedisLAN)
	}
}

func TestWorstRedisReach(t *testing.T) {
	if got := worstRedisReach(probe.ReachOK, probe.ReachFail); got != probe.ReachFail {
		t.Fatalf("got %s", got)
	}
}
