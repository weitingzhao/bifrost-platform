package releasepolicy_test

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// Review 82's first reproduction: freeze A, lift A, a hand-written freeze with
// no id, then A's lifted object put back. The remembered freeze is the
// unidentified one, so A's signature does not open the gate.
func TestHandWrittenFreezeWithoutAnIDThenTheOldLiftIsRefused(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first, err := f.eng.Freeze(ctx, "agent@mac", "incident")
	if err != nil {
		t.Fatal(err)
	}
	text := releasepolicy.UnfreezePrefix(first.FreezeID, first.FrozenAt) + "at=2026-10-08T12:05:00Z by=owner\n"
	sig := f.key.Sign(t, []byte(text), releasepolicy.NamespaceUnfreeze)
	if st, err := f.eng.Unfreeze(ctx, text, sig); err != nil || st.Frozen {
		t.Fatalf("unfreeze A = %+v %v", st, err)
	}
	lifted, _, _, _ := f.cms.ConfigMap(ctx, releasepolicy.FreezeConfigMap)
	f.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{
		"frozen": "true", "frozen_at": "2026-10-08T12:00:00Z", "who": "owner", "reason": "hand written",
	})
	mid := f.eng.Status(ctx)
	if !mid.Frozen || mid.FreezeID != "" || !strings.Contains(mid.FreezeReason, "no identifier") {
		t.Fatalf("hand-written freeze status = %+v", mid)
	}
	f.cms.Set(releasepolicy.FreezeConfigMap, lifted)
	wantWait(t, f.decide(nil), "a later freeze was seen and its record is gone")
}

// An unidentified freeze has nothing a signature can name. The status says so
// and points at the break-glass; the unfreeze write does not happen.
func TestUnidentifiedFreezeCannotBeLifted(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	obj := map[string]string{
		"frozen": "true", "frozen_at": "2026-10-08T12:00:00Z", "who": "owner", "reason": "hand written",
	}
	f.cms.Set(releasepolicy.FreezeConfigMap, obj)
	st := f.eng.Status(ctx)
	if !st.Frozen || !strings.Contains(st.FreezeReason, "no identifier") || !strings.Contains(st.FreezeReason, "break-glass") {
		t.Fatalf("status = %+v", st)
	}
	text := releasepolicy.UnfreezePrefix("0123456789abcdef0123456789abcdef", st.FrozenAt) + "at=2026-10-08T12:05:00Z by=owner\n"
	_, err := f.eng.Unfreeze(ctx, text, f.key.Sign(t, []byte(text), releasepolicy.NamespaceUnfreeze))
	if !errors.Is(err, releasepolicy.ErrRefused) || !strings.Contains(err.Error(), "break-glass") {
		t.Fatalf("unfreeze = %v", err)
	}
	got, _, _, _ := f.cms.ConfigMap(ctx, releasepolicy.FreezeConfigMap)
	if !reflect.DeepEqual(got, obj) {
		t.Fatalf("object changed to %v", got)
	}
	wantWait(t, f.decide(nil), "no identifier")
}

// Review 82's second reproduction, with the break-glass the docs describe:
// object and memory both reset, then a new freeze at the same frozen_at.
// A's signature names a different id, so it does not lift B.
func TestBreakGlassThenSameTimestampDoesNotReuseTheSignature(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "release-policy-freeze")
	f.eng = f.engineAt(path)
	first, err := f.eng.Freeze(ctx, "agent@mac", "incident")
	if err != nil {
		t.Fatal(err)
	}
	text := releasepolicy.UnfreezePrefix(first.FreezeID, first.FrozenAt) + "at=2026-10-08T12:05:00Z by=owner\n"
	sig := f.key.Sign(t, []byte(text), releasepolicy.NamespaceUnfreeze)
	if st, err := f.eng.Unfreeze(ctx, text, sig); err != nil || st.Frozen {
		t.Fatalf("unfreeze A = %+v %v", st, err)
	}
	f.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{"frozen": "false"})
	if err := os.WriteFile(path, []byte(`{"frozen_at":""}`), 0o644); err != nil {
		t.Fatal(err)
	}
	f.eng = f.engineAt(path)
	if st := f.eng.Status(ctx); st.Frozen {
		t.Fatalf("after break-glass = %+v", st)
	}
	second, err := f.eng.Freeze(ctx, "agent@mac", "incident")
	if err != nil {
		t.Fatal(err)
	}
	if second.FrozenAt != first.FrozenAt || second.FreezeID == first.FreezeID || !wellFormedHex(second.FreezeID) {
		t.Fatalf("A=%s@%s B=%s@%s", first.FreezeID, first.FrozenAt, second.FreezeID, second.FrozenAt)
	}
	before, _, _, _ := f.cms.ConfigMap(ctx, releasepolicy.FreezeConfigMap)
	_, err = f.eng.Unfreeze(ctx, text, sig)
	if !errors.Is(err, releasepolicy.ErrRefused) {
		t.Fatalf("A's signature lifted B: %v", err)
	}
	after, _, _, _ := f.cms.ConfigMap(ctx, releasepolicy.FreezeConfigMap)
	if !reflect.DeepEqual(before, after) || after["frozen"] != "true" || after["freeze_id"] != second.FreezeID {
		t.Fatalf("object after replay = %v", after)
	}
	wantWait(t, f.decide(nil), "releases are frozen")
}

// The object rolled back to never-frozen while an unlifted freeze is remembered.
func TestNeverFrozenRollbackWhileAnUnliftedFreezeIsRemembered(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.eng.Freeze(ctx, "agent@mac", "incident"); err != nil {
		t.Fatal(err)
	}
	f.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{"frozen": "false"})
	st := f.eng.Status(ctx)
	if !st.Frozen || !strings.Contains(st.FreezeReason, "was seen and its record is gone") {
		t.Fatalf("status = %+v", st)
	}
	wantWait(t, f.decide(nil), "was seen and its record is gone")
}

// A lifted shape whose signature does not verify, or whose text names a
// different id (including one that only prefixes the id in place), stays shut.
func TestLiftedObjectWithABadSignatureOrAnotherIDIsRefused(t *testing.T) {
	const id = "0123456789abcdef0123456789abcdef"
	const other = "fedcba9876543210fedcba9876543210"
	const frozenAt = "2026-10-08T11:30:00Z"
	text := releasepolicy.UnfreezePrefix(id, frozenAt) + "at=2026-10-08T11:45:00Z by=owner\n"

	t.Run("signature does not verify", func(t *testing.T) {
		f := newFixture(t)
		f.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{
			"frozen": "false", "frozen_at": frozenAt, "freeze_id": id,
			"unfreeze.txt": text, "unfreeze.sig": f.key.Sign(t, []byte(text+" "), releasepolicy.NamespaceUnfreeze),
		})
		wantWait(t, f.decide(nil), "unfreeze signature")
	})
	t.Run("names another id", func(t *testing.T) {
		f := newFixture(t)
		named := releasepolicy.UnfreezePrefix(other, frozenAt) + "at=2026-10-08T11:45:00Z by=owner\n"
		f.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{
			"frozen": "false", "frozen_at": frozenAt, "freeze_id": id,
			"unfreeze.txt": named, "unfreeze.sig": f.key.Sign(t, []byte(named), releasepolicy.NamespaceUnfreeze),
		})
		wantWait(t, f.decide(nil), "the unfreeze names "+other+", not "+id)
	})
	t.Run("a longer id is not the same token", func(t *testing.T) {
		f := newFixture(t)
		longer := id + "ab"
		named := releasepolicy.UnfreezePrefix(longer, frozenAt) + "at=2026-10-08T11:45:00Z by=owner\n"
		f.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{
			"frozen": "false", "frozen_at": frozenAt, "freeze_id": id,
			"unfreeze.txt": named, "unfreeze.sig": f.key.Sign(t, []byte(named), releasepolicy.NamespaceUnfreeze),
		})
		wantWait(t, f.decide(nil), "the unfreeze names "+longer+", not "+id)
	})
}

// The counter's retry loop is gone. One conflict refuses and leaves the
// object that won the race.
func TestFreezeDoesNotAllocateAgainAfterAConflict(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	w := &swapWriter{cms: f.cms, name: releasepolicy.FreezeConfigMap}
	f.eng = releasepolicy.New(releasepolicy.Deps{
		ConfigMaps: f.cms, Writer: w, Git: f.facts, CI: f.facts, Window: f.facts, Deployed: f.facts,
		Anchor: f.key.Fingerprint, Now: func() time.Time { return f.clock },
	})
	w.armed = true
	w.swap = map[string]string{
		"frozen": "true", "frozen_at": "2026-10-08T12:00:00Z",
		"freeze_id": "fedcba9876543210fedcba9876543210", "who": "other@host", "reason": "raced",
	}
	_, err := f.eng.Freeze(ctx, "agent@mac", "incident")
	if !errors.Is(err, releasepolicy.ErrRefused) || !strings.Contains(err.Error(), "the freeze changed while this freeze was being written") {
		t.Fatalf("conflict = %v", err)
	}
	got, _, _, _ := f.cms.ConfigMap(ctx, releasepolicy.FreezeConfigMap)
	if got["reason"] != "raced" || got["freeze_id"] != w.swap["freeze_id"] || got["unfreeze.txt"] != "" {
		t.Fatalf("object after conflict = %v", got)
	}
}

// gatePropertySequences is the number of seeded histories the property test
// runs. Each history is gatePropertyOps long. The gate may allow only when
// the last freeze since the previous full reset was lifted by a signature
// for its own id, or when no freeze has been set since that reset.
const (
	gatePropertySequences = 3000
	gatePropertyOps       = 12
	gatePropertySeed      = int64(31020261011)
)

type propFreeze struct {
	id     string
	lifted bool
}

type propSig struct {
	id, text, sig string
}

func TestGateAllowsOnlyTheFreezeThatWasLifted(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()
	rng := rand.New(rand.NewSource(gatePropertySeed))
	for n := 0; n < gatePropertySequences; n++ {
		runGateSequence(t, f, dir, rng, n)
	}
}

func runGateSequence(t *testing.T, f *fixture, dir string, rng *rand.Rand, n int) {
	t.Helper()
	f.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{"frozen": "false"})
	w := &propWorld{t: t, f: f}
	for i := range w.paths {
		w.paths[i] = filepath.Join(dir, string(rune('a'+i)))
		_ = os.Remove(w.paths[i])
	}
	w.boot()
	var (
		history []propFreeze
		sigs    []propSig
		snaps   = []map[string]string{{"frozen": "false"}}
		trace   []string
	)
	for step := 0; step < gatePropertyOps; step++ {
		switch rng.Intn(7) {
		case 0:
			st, err := w.eng[0].Freeze(context.Background(), "agent@mac", "incident")
			if err != nil {
				trace = append(trace, "freeze refused")
				break
			}
			history = append(history, propFreeze{id: st.FreezeID})
			trace = append(trace, "freeze "+st.FreezeID)
		case 1:
			id, text, sig, ok := w.signInPlace()
			if !ok {
				trace = append(trace, "unfreeze skipped")
				break
			}
			_, err := w.eng[0].Unfreeze(context.Background(), text, sig)
			sigs = append(sigs, propSig{id: id, text: text, sig: sig})
			if err == nil && len(history) > 0 && history[len(history)-1].id == id {
				history[len(history)-1].lifted = true
				trace = append(trace, "unfreeze "+id)
			} else if err == nil {
				trace = append(trace, "unfreeze wrote "+id)
			} else {
				trace = append(trace, "unfreeze refused "+id)
			}
		case 2:
			if len(sigs) == 0 {
				trace = append(trace, "replay skipped")
				break
			}
			s := sigs[rng.Intn(len(sigs))]
			_, err := w.eng[0].Unfreeze(context.Background(), s.text, s.sig)
			if err == nil && len(history) > 0 && history[len(history)-1].id == s.id {
				history[len(history)-1].lifted = true
				trace = append(trace, "replay lifted "+s.id)
			} else if err == nil {
				trace = append(trace, "replay wrote "+s.id)
			} else {
				trace = append(trace, "replay refused "+s.id)
			}
		case 3:
			w.f.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{
				"frozen": "true", "frozen_at": "2026-10-08T12:00:00Z", "who": "owner", "reason": "hand written",
			})
			history = append(history, propFreeze{})
			trace = append(trace, "hand-written freeze")
		case 4:
			if len(snaps) < 2 {
				trace = append(trace, "rollback skipped")
				break
			}
			snap := snaps[rng.Intn(len(snaps)-1)]
			w.f.cms.Set(releasepolicy.FreezeConfigMap, cloneData(snap))
			trace = append(trace, "rollback")
		case 5:
			w.f.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{"frozen": "false"})
			for _, path := range w.paths {
				if err := os.WriteFile(path, []byte(`{"frozen_at":""}`), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			w.boot()
			history = nil
			snaps = []map[string]string{{"frozen": "false"}}
			trace = append(trace, "break-glass")
		default:
			w.boot()
			trace = append(trace, "restart")
		}
		if trace[len(trace)-1] != "break-glass" {
			data, _, found, err := w.f.cms.ConfigMap(context.Background(), releasepolicy.FreezeConfigMap)
			if err != nil || !found {
				t.Fatalf("seq %d step %d object: found=%v err=%v", n, step, found, err)
			}
			snaps = append(snaps, data)
		}
		w.check(n, history, trace)
	}
}

type propWorld struct {
	t     *testing.T
	f     *fixture
	paths [2]string
	eng   [2]*releasepolicy.Engine
}

func (w *propWorld) boot() {
	for i := range w.eng {
		w.eng[i] = w.f.engineAt(w.paths[i])
	}
}

func (f *fixture) engineAt(path string) *releasepolicy.Engine {
	return releasepolicy.New(releasepolicy.Deps{
		ConfigMaps: f.cms, Writer: rptest.Writer{CMs: f.cms}, Git: f.facts, CI: f.facts, Window: f.facts, Deployed: f.facts,
		Anchor: f.key.Fingerprint, Now: func() time.Time { return f.clock }, StatePath: path,
	})
}

func (w *propWorld) signInPlace() (string, string, string, bool) {
	st := w.eng[0].Status(context.Background())
	if !st.Frozen || !wellFormedHex(st.FreezeID) || st.FrozenAt == "" {
		return "", "", "", false
	}
	text := releasepolicy.UnfreezePrefix(st.FreezeID, st.FrozenAt) + "at=2026-10-08T12:05:00Z by=owner\n"
	return st.FreezeID, text, w.f.key.Sign(w.t, []byte(text), releasepolicy.NamespaceUnfreeze), true
}

func (w *propWorld) check(n int, history []propFreeze, trace []string) {
	w.t.Helper()
	ok := len(history) == 0
	if !ok {
		last := history[len(history)-1]
		ok = last.id != "" && last.lifted
	}
	for i, eng := range w.eng {
		st := eng.Status(context.Background())
		if !st.Frozen && !ok {
			w.t.Fatalf("sequence %d engine %d allowed a release\ntrace:\n%s\nstatus: %+v\nhistory: %+v",
				n, i, strings.Join(trace, "\n"), st, history)
		}
		d := eng.Decide(context.Background(), "start_pipeline_run", actions.TierC, map[string]any{
			"name": pipeline, "revision": "main", "who": "agent@mac",
		})
		if d.Auto && !ok {
			w.t.Fatalf("sequence %d engine %d auto-approved\ntrace:\n%s\nreasons: %q", n, i, strings.Join(trace, "\n"), d.Reasons)
		}
	}
}

func cloneData(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
