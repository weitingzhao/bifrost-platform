package homelayout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/yaml.v3"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Origin: 2026-11-18, the coordinator chose a versioned store for the Owner's
// city layout: a phone and a desktop must not silently overwrite each other,
// and only a layout (not any JSON) goes in.

// A layout is stored as any artifact; the runtime's body checks are its own.
func init() { artifact.RegisterCheck(func(string, []byte) error { return nil }) }

var owner = actor.Writer{Actor: "owner", Token: "owner"}

func sample(w, parts int) string {
	data := make([]string, w*w)
	for i := range data {
		data[i] = "0"
	}
	objs := make([]string, parts)
	for i := range objs {
		objs[i] = fmt.Sprintf(`{"type":"dorm","x":%d,"y":0,"width":64,"height":64}`, i*64)
	}
	return fmt.Sprintf(`{"type":"map","orientation":"isometric","width":%d,"height":%d,"tileheight":32,"layers":[{"type":"tilelayer","name":"roads","data":[%s]},{"type":"objectgroup","name":"parts","objects":[%s]}]}`,
		w, w, strings.Join(data, ","), strings.Join(objs, ","))
}

func TestCheckTakesOnlyALayout(t *testing.T) {
	if err := Check([]byte(sample(16, 2))); err != nil {
		t.Fatal(err)
	}
	bad := map[string]string{
		"not json":      `{`,
		"another map":   strings.Replace(sample(4, 0), "isometric", "orthogonal", 1),
		"no land":       sample(0, 0),
		"too much land": sample(97, 0),
		"short roads":   strings.Replace(sample(4, 0), `"data":[0,`, `"data":[`, 1),
		"no layers":     `{"type":"map","orientation":"isometric","width":1,"height":1,"tileheight":32,"layers":[]}`,
		"odd footprint": strings.Replace(sample(4, 1), `"width":64,"height":64`, `"width":96,"height":96`, 1),
		"off the land":  sample(1, 1),
		"too big":       sample(16, 0) + strings.Repeat(" ", MaxBytes),
	}
	for name, raw := range bad {
		if Check([]byte(raw)) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func open(t *testing.T) *pgxpool.Pool {
	t.Helper()
	db := agentrttest.Migrated(t)
	pool, err := pgxpool.New(context.Background(), db.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := actor.Seed(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestSaveVersionsAndRefusesAStaleBase(t *testing.T) {
	ctx, pool := context.Background(), open(t)
	if s, err := Latest(ctx, pool); err != nil || s.Version != 0 || s.Layout != nil {
		t.Fatalf("before any save: %+v %v", s, err)
	}
	a, err := Save(ctx, pool, owner, 0, []byte(sample(16, 1)))
	if err != nil || a.Version != 1 {
		t.Fatalf("first save: %+v %v", a, err)
	}
	first, err := Save(ctx, pool, owner, 1, []byte(sample(24, 2)))
	if err != nil || first.Version != 2 {
		t.Fatalf("second save: %+v %v", first, err)
	}
	stale, err := Save(ctx, pool, owner, 1, []byte(sample(16, 0)))
	if !errors.Is(err, ErrStale) || stale.Version != 2 {
		t.Fatalf("a save from an old version: %+v %v", stale, err)
	}
	got, err := Latest(ctx, pool)
	if err != nil || got.Version != 2 || got.SavedBy != "owner" || got.SavedAt.IsZero() {
		t.Fatalf("latest: %+v %v", got, err)
	}
	var m struct{ Width int }
	if err := json.Unmarshal(got.Layout, &m); err != nil || m.Width != 24 {
		t.Fatalf("the layout read back: %s %v", got.Layout, err)
	}
}

func TestTwoDevicesFromOneVersionOneWins(t *testing.T) {
	ctx, pool := context.Background(), open(t)
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func() { defer wg.Done(); _, errs[i] = Save(ctx, pool, owner, 0, []byte(sample(16, 0))) }()
	}
	wg.Wait()
	won := 0
	for _, err := range errs {
		switch {
		case err == nil:
			won++
		case !errors.Is(err, ErrStale):
			t.Errorf("unexpected: %v", err)
		}
	}
	if won != 1 {
		t.Fatalf("%d saves won", won)
	}
}

// Origin: the same day: the stream is the history, so every save stays readable and the chain holds.
func TestEverySaveStaysOnTheStream(t *testing.T) {
	ctx, pool := context.Background(), open(t)
	for i := range 3 {
		if _, err := Save(ctx, pool, owner, i, []byte(sample(16+i, 0))); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := eventlog.VerifyStream(ctx, pool, Stream); err != nil || n != 3 {
		t.Fatalf("%d events: %v", n, err)
	}
}

// Origin: 2026-11-18: one owning domain per event type (bus/registry/ledger/types.yaml).
func TestTheEventTypeIsRegisteredUnderOwner(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "bus", "registry", "ledger", "types.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Types []struct{ Type, Domain string } `yaml:"types"`
	}
	if err = yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, lt := range doc.Types {
		if lt.Type == EventType && lt.Domain == "owner" {
			return
		}
	}
	t.Fatalf("%s is not registered under owner", EventType)
}
