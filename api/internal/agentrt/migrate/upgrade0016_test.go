package migrate_test

import (
	"context"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func TestUpgrade0016KeepsEvents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.New(t)
	conn := db.Connect(t, db.Owner)
	files, err := migrate.Files()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = migrate.RunFiles(ctx, conn, "dev", files[:15]); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Seed(ctx, conn); err != nil {
		t.Fatal(err)
	}
	const query = `SELECT COALESCE(string_agg(to_jsonb(e)::text,E'\n' ORDER BY event_id),'') FROM agentrt.event e`
	var before, after string
	if err = conn.QueryRow(ctx, query).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if before == "" {
		t.Fatal("fixture has no events")
	}
	applied, err := migrate.RunFiles(ctx, conn, "dev", files[:16])
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || applied[0] != 16 {
		t.Fatalf("applied %v", applied)
	}
	if err = conn.QueryRow(ctx, query).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("upgrade changed events")
	}
	for _, name := range []string{"metric_reading", "metric_reading_window_idx", "metric_reading_label_idx", "event_ci_tree_idx"} {
		var exists bool
		if err = conn.QueryRow(ctx, `SELECT to_regclass('agentrt.' || $1) IS NOT NULL`, name).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("missing %s", name)
		}
	}
	var triggers int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM pg_trigger WHERE tgrelid='agentrt.metric_reading'::regclass AND NOT tgisinternal`).Scan(&triggers); err != nil {
		t.Fatal(err)
	}
	if triggers != 3 {
		t.Fatalf("got %d triggers", triggers)
	}
}
