package migrate_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func TestFilesAreNumberedWithoutGaps(t *testing.T) {
	files, err := migrate.Files()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 3 {
		t.Fatalf("found %d migrations", len(files))
	}
}

// T19: a second run changes nothing; two migrators started together apply
// each file once; an applied file edited by one byte stops the run.
func TestRunIsIdempotentSerialAndRefusesEditedFiles(t *testing.T) {
	db := agentrttest.New(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	results := make([][]int, 2)
	errs := make([]error, 2)
	for i := range 2 {
		c := db.Connect(t, db.Owner)
		wg.Add(1)
		go func(i int, c *pgx.Conn) {
			defer wg.Done()
			results[i], errs[i] = migrate.Run(ctx, c, "dev")
		}(i, c)
	}
	wg.Wait()
	files, _ := migrate.Files()
	total := 0
	for i := range 2 {
		if errs[i] != nil {
			t.Fatalf("migrator %d: %v", i, errs[i])
		}
		total += len(results[i])
	}
	if total != len(files) {
		t.Fatalf("the two migrators applied %d files together, want %d (each once)", total, len(files))
	}

	again, err := migrate.Run(ctx, db.Connect(t, db.Owner), "dev")
	if err != nil || len(again) != 0 {
		t.Fatalf("second run applied %v, err %v", again, err)
	}

	edited := append([]migrate.File(nil), files...)
	edited[0].SQL += " "
	edited[0].SHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := migrate.RunFiles(ctx, db.Connect(t, db.Owner), "dev", edited); !errors.Is(err, migrate.ErrEdited) {
		t.Fatalf("edited file: err = %v, want ErrEdited", err)
	}
}

func TestUnknownEnvironmentIsRefused(t *testing.T) {
	if _, err := migrate.RuntimeRole("qa"); err == nil {
		t.Fatal("qa accepted")
	}
}

// 0005 on a database that already has events (review 166 B2): the events
// stay as they were, the vocabulary gains attempt_cancelled and keeps every
// type it had.
func TestUpgradeTo0005KeepsEvents(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.New(t)
	conn := db.Connect(t, db.Owner)
	files, err := migrate.Files()
	if err != nil || len(files) < 5 || files[4].ID != 5 {
		t.Fatalf("files = %d %v", len(files), err)
	}
	if _, err = migrate.RunFiles(ctx, conn, "dev", files[:4]); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Seed(ctx, conn); err != nil {
		t.Fatal(err)
	}
	var before, after int
	var sumBefore, sumAfter string
	count := `SELECT count(*), COALESCE(string_agg(sha256, ',' ORDER BY event_stream_id, seq), '') FROM agentrt.event`
	if err = conn.QueryRow(ctx, count).Scan(&before, &sumBefore); err != nil || before == 0 {
		t.Fatalf("events before = %d %v", before, err)
	}
	var defBefore string
	if err = conn.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'event_type_closed'`).Scan(&defBefore); err != nil {
		t.Fatal(err)
	}
	applied, err := migrate.RunFiles(ctx, conn, "dev", files)
	if err != nil || len(applied) != len(files)-4 || applied[0] != 5 {
		t.Fatalf("applied %v %v", applied, err)
	}
	if err = conn.QueryRow(ctx, count).Scan(&after, &sumAfter); err != nil || after != before || sumAfter != sumBefore {
		t.Fatalf("events after = %d (was %d) %v", after, before, err)
	}
	var def string
	if err = conn.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'event_type_closed'`).Scan(&def); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(def, "'attempt_cancelled'") || strings.Contains(defBefore, "'attempt_cancelled'") {
		t.Fatalf("constraint before %q, after %q", defBefore, def)
	}
	for _, typ := range strings.Split(strings.TrimSpace(defBefore[strings.Index(defBefore, "ARRAY[")+6:strings.LastIndex(defBefore, "]")]), ",") {
		if !strings.Contains(def, strings.TrimSpace(typ)) {
			t.Fatalf("0005 dropped %s", typ)
		}
	}
}
