package main

import (
	"context"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
)

// The subcommand refuses a database or user that is not the environment's,
// and is idempotent on the right one.
func TestMigrateAndSeedChecksWhereItRuns(t *testing.T) {
	db := agentrttest.New(t)
	ctx := context.Background()
	conn := db.Connect(t, db.Owner)
	if _, _, err := migrateAndSeed(ctx, conn, "dev", "bifrost_platform_dev"); err == nil || !strings.Contains(err.Error(), "needs bifrost_platform_dev") {
		t.Fatalf("wrong database accepted: %v", err)
	}
	if _, _, err := migrateAndSeed(ctx, conn, "stg", db.Name); err == nil || !strings.Contains(err.Error(), "agentrt_owner_stg") {
		t.Fatalf("wrong user accepted: %v", err)
	}
	applied, seeded, err := migrateAndSeed(ctx, conn, "dev", db.Name)
	if err != nil || len(applied) == 0 || !seeded {
		t.Fatalf("first run: applied %v seeded %v err %v", applied, seeded, err)
	}
	applied, seeded, err = migrateAndSeed(ctx, conn, "dev", db.Name)
	if err != nil || len(applied) != 0 || seeded {
		t.Fatalf("second run: applied %v seeded %v err %v", applied, seeded, err)
	}
}
