package releasepolicy_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// The sentence Freeze returns when the anchor is empty. refused prefixes it
// with "refused: ", which is what the handler writes and the audit records.
const freezeRefusedNoAnchor = `freeze refused: platform-api has no trust anchor, so no signature could lift it. Generate the release owner key and compile its fingerprint in first (docs/RELEASE.md, "The release owner key").`

func TestInstallVerifiesBeforeWriting(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	installed, _, _, _ := f.cms.ConfigMap(ctx, releasepolicy.PolicyConfigMap)

	refuse := func(name, text, sig, want string) {
		t.Helper()
		_, err := f.eng.Install(ctx, text, sig)
		if !errors.Is(err, releasepolicy.ErrRefused) || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: err = %v, want refusal containing %q", name, err, want)
		}
		if got, _, _, _ := f.cms.ConfigMap(ctx, releasepolicy.PolicyConfigMap); got["policy.yaml"] != installed["policy.yaml"] {
			t.Fatalf("%s: the installed policy changed", name)
		}
	}
	newer := rptest.PolicyText("rp-20261008-1130", now.Add(-30*time.Minute), 90, pipeline)
	refuse("foreign key", newer, rptest.NewKey(t).Sign(t, []byte(newer), releasepolicy.NamespacePolicy), "not the compiled-in Owner key")
	refuse("unfreeze namespace", newer, f.key.Sign(t, []byte(newer), releasepolicy.NamespaceUnfreeze), "namespace")
	expired := rptest.PolicyText("rp-old", now.Add(-100*24*time.Hour), 90, pipeline)
	refuse("expired", expired, f.key.Sign(t, []byte(expired), releasepolicy.NamespacePolicy), "expired at")
	older := rptest.PolicyText("rp-20261008-1000", now.Add(-2*time.Hour), 90, pipeline, "wider")
	refuse("older than installed", older, f.key.Sign(t, []byte(older), releasepolicy.NamespacePolicy), "before the installed")

	st, err := f.eng.Install(ctx, newer, f.key.Sign(t, []byte(newer), releasepolicy.NamespacePolicy))
	if err != nil || !st.Valid || st.PolicyID != "rp-20261008-1130" {
		t.Fatalf("install = %+v, %v", st, err)
	}
	// The same policy again is a no-op, not a refusal.
	if _, err := f.eng.Install(ctx, newer, f.key.Sign(t, []byte(newer), releasepolicy.NamespacePolicy)); err != nil {
		t.Fatalf("reinstall: %v", err)
	}
}

func TestFreezeAndSignedUnfreeze(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.eng.Freeze(ctx, "", "x"); !errors.Is(err, releasepolicy.ErrRefused) {
		t.Fatalf("freeze without who: %v", err)
	}
	st, err := f.eng.Freeze(ctx, "agent@mac", "incident")
	if err != nil || !st.Frozen || st.FrozenAt != "2026-10-08T12:00:00Z" {
		t.Fatalf("freeze = %+v, %v", st, err)
	}
	wantWait(t, f.decide(nil), "releases are frozen")

	stale := "unfreeze freeze_id=999 frozen_at=2026-10-08T11:00:00Z at=2026-10-08T12:05:00Z by=owner\n"
	if _, err = f.eng.Unfreeze(ctx, stale, f.key.Sign(t, []byte(stale), releasepolicy.NamespaceUnfreeze)); !errors.Is(err, releasepolicy.ErrRefused) {
		t.Fatalf("unfreeze for another freeze: %v", err)
	}
	if st.FreezeID == "" {
		t.Fatal("freeze status has no freeze_id")
	}
	text := releasepolicy.UnfreezePrefix(st.FreezeID, st.FrozenAt) + "at=2026-10-08T12:05:00Z by=owner\n"
	if _, err = f.eng.Unfreeze(ctx, text, rptest.NewKey(t).Sign(t, []byte(text), releasepolicy.NamespaceUnfreeze)); !errors.Is(err, releasepolicy.ErrRefused) {
		t.Fatalf("unfreeze with a foreign key: %v", err)
	}
	if _, err = f.eng.Unfreeze(ctx, text, f.key.Sign(t, []byte(text), releasepolicy.NamespacePolicy)); !errors.Is(err, releasepolicy.ErrRefused) {
		t.Fatalf("unfreeze in the policy namespace: %v", err)
	}
	st, err = f.eng.Unfreeze(ctx, text, f.key.Sign(t, []byte(text), releasepolicy.NamespaceUnfreeze))
	if err != nil || st.Frozen {
		t.Fatalf("unfreeze = %+v, %v", st, err)
	}
	wantAuto(t, f.decide(nil))
	if _, err := f.eng.Unfreeze(ctx, text, f.key.Sign(t, []byte(text), releasepolicy.NamespaceUnfreeze)); !errors.Is(err, releasepolicy.ErrRefused) {
		t.Fatalf("unfreeze when not frozen: %v", err)
	}
}

// countingWriter records WriteConfigMap calls and forwards them.
type countingWriter struct {
	n   int
	cms *rptest.ConfigMaps
}

func (w *countingWriter) WriteConfigMap(ctx context.Context, name string, data map[string]string, resourceVersion string) error {
	w.n++
	return (rptest.Writer{CMs: w.cms}).WriteConfigMap(ctx, name, data, resourceVersion)
}

// TestFreezeRefusedWithoutTrustAnchor: an empty anchor refuses before any
// write and before the remembered newest freeze changes. With an anchor,
// freeze still writes and records that time.
func TestFreezeRefusedWithoutTrustAnchor(t *testing.T) {
	const seenAt = "2026-10-08T11:00:00Z"
	seed := []byte(`{"frozen_at":"` + seenAt + `"}`)

	newEng := func(t *testing.T, anchor string) (*fixture, *countingWriter, string) {
		t.Helper()
		f := newFixture(t)
		path := filepath.Join(t.TempDir(), "release-policy-freeze")
		if err := os.WriteFile(path, seed, 0o644); err != nil {
			t.Fatal(err)
		}
		w := &countingWriter{cms: f.cms}
		f.eng = releasepolicy.New(releasepolicy.Deps{
			ConfigMaps: f.cms, Writer: w, Git: f.facts, CI: f.facts, Window: f.facts, Deployed: f.facts,
			Anchor: anchor, Now: func() time.Time { return f.clock }, StatePath: path,
		})
		return f, w, path
	}

	t.Run("empty anchor", func(t *testing.T) {
		f, w, path := newEng(t, "")
		ctx := context.Background()
		before, _, _, _ := f.cms.ConfigMap(ctx, releasepolicy.FreezeConfigMap)
		_, err := f.eng.Freeze(ctx, "agent@mac", "incident")
		if err == nil || !errors.Is(err, releasepolicy.ErrRefused) || err.Error() != "refused: "+freezeRefusedNoAnchor {
			t.Fatalf("freeze = %v, want refused: %s", err, freezeRefusedNoAnchor)
		}
		if w.n != 0 {
			t.Fatalf("writer saw %d writes", w.n)
		}
		after, _, _, _ := f.cms.ConfigMap(ctx, releasepolicy.FreezeConfigMap)
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("freeze ConfigMap changed from %v to %v", before, after)
		}
		if got, err := os.ReadFile(path); err != nil || string(got) != string(seed) {
			t.Fatalf("remembered freeze = %q (%v), want %s", got, err, seed)
		}
		st := f.eng.Status(ctx)
		if !st.Frozen || !strings.Contains(st.FreezeReason, seenAt) {
			t.Fatalf("status after refusal = %+v, want the remembered %s still in force", st, seenAt)
		}

		var audited []string
		h := f.eng.HandleFreeze(func(_ *http.Request, action, target, result, detail string) {
			audited = append(audited, action+" result="+result+" target="+target+" detail="+detail)
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/release-policy/freeze", strings.NewReader(`{"who":"agent@mac","reason":"incident"}`))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("handler = %d %s", rec.Code, rec.Body.String())
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body["error"] != "refused: "+freezeRefusedNoAnchor {
			t.Fatalf("body = %q", body["error"])
		}
		if len(audited) != 1 || !strings.Contains(audited[0], "release_policy.freeze") || !strings.Contains(audited[0], "result=refused") || !strings.Contains(audited[0], freezeRefusedNoAnchor) {
			t.Fatalf("audit = %q", audited)
		}
		if w.n != 0 {
			t.Fatalf("handler wrote %d times", w.n)
		}
	})

	t.Run("with an anchor", func(t *testing.T) {
		f, w, path := newEng(t, "")
		f.eng = releasepolicy.New(releasepolicy.Deps{
			ConfigMaps: f.cms, Writer: w, Git: f.facts, CI: f.facts, Window: f.facts, Deployed: f.facts,
			Anchor: f.key.Fingerprint, Now: func() time.Time { return f.clock }, StatePath: path,
		})
		st, err := f.eng.Freeze(context.Background(), "agent@mac", "incident")
		if err != nil || !st.Frozen || st.FrozenAt != "2026-10-08T12:00:00Z" {
			t.Fatalf("freeze = %+v, %v", st, err)
		}
		if w.n != 1 {
			t.Fatalf("writer saw %d writes, want 1", w.n)
		}
		got, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(got), `"frozen_at":"2026-10-08T12:00:00Z"`) || !regexp.MustCompile(`"freeze_id":"[0-9a-f]{32}"`).Match(got) {
			t.Fatalf("remembered freeze = %q (%v)", got, err)
		}
	})
}

// swapWriter replaces one object on the next write, then applies the caller's
// conditional write. The version the caller read no longer matches.
type swapWriter struct {
	cms   *rptest.ConfigMaps
	name  string
	swap  map[string]string
	armed bool
}

func (w *swapWriter) WriteConfigMap(ctx context.Context, name string, data map[string]string, resourceVersion string) error {
	if w.armed && name == w.name {
		w.armed = false
		w.cms.Set(name, w.swap)
	}
	return (rptest.Writer{CMs: w.cms}).WriteConfigMap(ctx, name, data, resourceVersion)
}

func TestSameSecondUnfreezeDoesNotLiftTheNextFreeze(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first, err := f.eng.Freeze(ctx, "agent@mac", "incident")
	if err != nil || !first.Frozen || first.FreezeID == "" {
		t.Fatalf("freeze A = %+v %v", first, err)
	}
	textA := releasepolicy.UnfreezePrefix(first.FreezeID, first.FrozenAt) + "at=2026-10-08T12:05:00Z by=owner\n"
	sigA := f.key.Sign(t, []byte(textA), releasepolicy.NamespaceUnfreeze)
	if st, err := f.eng.Unfreeze(ctx, textA, sigA); err != nil || st.Frozen {
		t.Fatalf("unfreeze A = %+v %v", st, err)
	}
	second, err := f.eng.Freeze(ctx, "agent@mac", "incident")
	if err != nil || !second.Frozen {
		t.Fatalf("freeze B = %+v %v", second, err)
	}
	if second.FrozenAt != first.FrozenAt {
		t.Fatalf("clock moved: %s vs %s", first.FrozenAt, second.FrozenAt)
	}
	if !wellFormedHex(first.FreezeID) || !wellFormedHex(second.FreezeID) || first.FreezeID == second.FreezeID {
		t.Fatalf("freeze ids = %s, %s; each must be its own 128-bit hex value", first.FreezeID, second.FreezeID)
	}
	before, _, _, _ := f.cms.ConfigMap(ctx, releasepolicy.FreezeConfigMap)
	if _, err := f.eng.Unfreeze(ctx, textA, sigA); !errors.Is(err, releasepolicy.ErrRefused) {
		t.Fatalf("replay of A's signature = %v", err)
	}
	after, _, _, _ := f.cms.ConfigMap(ctx, releasepolicy.FreezeConfigMap)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("replay wrote: before %v after %v", before, after)
	}
	if after["frozen"] != "true" || after["freeze_id"] != second.FreezeID || after["unfreeze.txt"] != "" {
		t.Fatalf("B is not still in place: %v", after)
	}
	wantWait(t, f.decide(nil), "releases are frozen")
	assertTektonStillFrozen(t, after)

	// The same timestamp on A's signed unfreeze is not enough once B was seen.
	f.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{
		"frozen": "false", "frozen_at": first.FrozenAt, "freeze_id": first.FreezeID,
		"unfreeze.txt": textA, "unfreeze.sig": sigA,
	})
	wantWait(t, f.decide(nil), "a later freeze")
}

func TestUnfreezeTextThatNamesOnlyFrozenAtIsRefused(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	st, err := f.eng.Freeze(ctx, "agent@mac", "incident")
	if err != nil {
		t.Fatal(err)
	}
	old := "unfreeze frozen_at=" + st.FrozenAt + " at=2026-10-08T12:05:00Z by=owner\n"
	before, _, _, _ := f.cms.ConfigMap(ctx, releasepolicy.FreezeConfigMap)
	_, err = f.eng.Unfreeze(ctx, old, f.key.Sign(t, []byte(old), releasepolicy.NamespaceUnfreeze))
	if !errors.Is(err, releasepolicy.ErrRefused) || !strings.Contains(err.Error(), "freeze_id="+st.FreezeID) {
		t.Fatalf("old form = %v", err)
	}
	after, _, _, _ := f.cms.ConfigMap(ctx, releasepolicy.FreezeConfigMap)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("old form wrote %v", after)
	}
	wantWait(t, f.decide(nil), "releases are frozen")
}

func TestUnfreezeWriteRefusesWhenTheFreezeChanged(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	w := &swapWriter{cms: f.cms}
	f.eng = releasepolicy.New(releasepolicy.Deps{
		ConfigMaps: f.cms, Writer: w, Git: f.facts, CI: f.facts, Window: f.facts, Deployed: f.facts,
		Anchor: f.key.Fingerprint, Now: func() time.Time { return f.clock },
	})
	st, err := f.eng.Freeze(ctx, "agent@mac", "incident")
	if err != nil {
		t.Fatal(err)
	}
	text := releasepolicy.UnfreezePrefix(st.FreezeID, st.FrozenAt) + "at=2026-10-08T12:05:00Z by=owner\n"
	sig := f.key.Sign(t, []byte(text), releasepolicy.NamespaceUnfreeze)
	w.name = releasepolicy.FreezeConfigMap
	w.armed = true
	w.swap = map[string]string{
		"frozen": "true", "frozen_at": st.FrozenAt, "freeze_id": "99",
		"who": "other@host", "reason": "a later freeze",
	}
	_, err = f.eng.Unfreeze(ctx, text, sig)
	const want = "refused: the freeze changed while the unfreeze was being checked; read the status and sign again"
	if !errors.Is(err, releasepolicy.ErrRefused) || err.Error() != want {
		t.Fatalf("conflict = %v", err)
	}
	got, _, _, _ := f.cms.ConfigMap(ctx, releasepolicy.FreezeConfigMap)
	if got["freeze_id"] != "99" || got["frozen"] != "true" || got["reason"] != "a later freeze" || got["unfreeze.txt"] != "" {
		t.Fatalf("object after refused unfreeze = %v", got)
	}
	assertTektonStillFrozen(t, got)
}

func TestInstallDoesNotOverwriteAPolicyItDidNotCheck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	w := &swapWriter{cms: f.cms, name: releasepolicy.PolicyConfigMap}
	f.eng = releasepolicy.New(releasepolicy.Deps{
		ConfigMaps: f.cms, Writer: w, Git: f.facts, CI: f.facts, Window: f.facts, Deployed: f.facts,
		Anchor: f.key.Fingerprint, Now: func() time.Time { return f.clock },
	})
	newer := rptest.PolicyText("rp-20261008-1130", now.Add(-30*time.Minute), 90, pipeline)
	w.armed = true
	w.swap = map[string]string{"policy.yaml": "someone else's policy\n", "policy.sig": "other"}
	_, err := f.eng.Install(ctx, newer, f.key.Sign(t, []byte(newer), releasepolicy.NamespacePolicy))
	if !errors.Is(err, releasepolicy.ErrRefused) || !strings.Contains(err.Error(), "the installed policy changed while this policy was being checked") {
		t.Fatalf("install race = %v", err)
	}
	got, _, _, _ := f.cms.ConfigMap(ctx, releasepolicy.PolicyConfigMap)
	if got["policy.yaml"] != "someone else's policy\n" || got["policy.sig"] != "other" {
		t.Fatalf("install wrote over the policy it did not check: %v", got)
	}
}

// assertTektonStillFrozen runs freeze_decision.py when the sibling checkout's
// schema already lists freeze_id. A checkout that is missing, or a schema
// from before this change, does not fail the test: the object itself is
// still required to be the frozen shape.
func assertTektonStillFrozen(t *testing.T, data map[string]string) {
	t.Helper()
	if data["frozen"] != "true" || data["freeze_id"] == "" || data["unfreeze.txt"] != "" {
		t.Fatalf("not the frozen shape: %v", data)
	}
	dir := siblingReleaseDir()
	if dir == "" {
		t.Log("sibling bifrost-trade-infra checkout is absent; freeze_decision.py was not run")
		return
	}
	schema, err := os.ReadFile(filepath.Join(dir, "freeze_schema.json"))
	if err != nil || !strings.Contains(string(schema), `"freeze_id"`) {
		t.Log("sibling freeze schema does not list freeze_id; freeze_decision.py was not run")
		return
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("python3", "-c", `
import json, sys
sys.path.insert(0, sys.argv[1])
import freeze_decision as fd
allow, line = fd.decide(json.loads(sys.argv[2]))
sys.stdout.write(("allow\n" if allow else "frozen\n") + line)
`, dir, string(raw))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("freeze_decision: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "拒绝：发布已冻结") {
		t.Fatalf("tekton decision = %q, want the frozen refusal", out)
	}
}

func wellFormedHex(id string) bool {
	if len(id) < 32 || len(id)%2 != 0 {
		return false
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func siblingReleaseDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	dir := wd
	for i := 0; i < 8; i++ {
		cand := filepath.Join(dir, "bifrost-trade-infra", "scripts", "release")
		if st, err := os.Stat(filepath.Join(cand, "freeze_decision.py")); err == nil && !st.IsDir() {
			return cand
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
