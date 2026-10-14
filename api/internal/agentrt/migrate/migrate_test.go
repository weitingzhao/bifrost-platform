package migrate_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
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
