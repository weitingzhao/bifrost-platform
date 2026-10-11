package releasepolicy_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
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
	installed, _, _ := f.cms.ConfigMap(ctx, releasepolicy.PolicyConfigMap)

	refuse := func(name, text, sig, want string) {
		t.Helper()
		_, err := f.eng.Install(ctx, text, sig)
		if !errors.Is(err, releasepolicy.ErrRefused) || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: err = %v, want refusal containing %q", name, err, want)
		}
		if got, _, _ := f.cms.ConfigMap(ctx, releasepolicy.PolicyConfigMap); got["policy.yaml"] != installed["policy.yaml"] {
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

	stale := "unfreeze frozen_at=2026-10-08T11:00:00Z at=2026-10-08T12:05:00Z by=owner\n"
	if _, err = f.eng.Unfreeze(ctx, stale, f.key.Sign(t, []byte(stale), releasepolicy.NamespaceUnfreeze)); !errors.Is(err, releasepolicy.ErrRefused) {
		t.Fatalf("unfreeze for another freeze: %v", err)
	}
	text := "unfreeze frozen_at=" + st.FrozenAt + " at=2026-10-08T12:05:00Z by=owner\n"
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

func (w *countingWriter) WriteConfigMap(ctx context.Context, name string, data map[string]string) error {
	w.n++
	return (rptest.Writer{CMs: w.cms}).WriteConfigMap(ctx, name, data)
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
		before, _, _ := f.cms.ConfigMap(ctx, releasepolicy.FreezeConfigMap)
		_, err := f.eng.Freeze(ctx, "agent@mac", "incident")
		if err == nil || !errors.Is(err, releasepolicy.ErrRefused) || err.Error() != "refused: "+freezeRefusedNoAnchor {
			t.Fatalf("freeze = %v, want refused: %s", err, freezeRefusedNoAnchor)
		}
		if w.n != 0 {
			t.Fatalf("writer saw %d writes", w.n)
		}
		after, _, _ := f.cms.ConfigMap(ctx, releasepolicy.FreezeConfigMap)
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
		if err != nil || !strings.Contains(string(got), `"frozen_at":"2026-10-08T12:00:00Z"`) {
			t.Fatalf("remembered freeze = %q (%v)", got, err)
		}
	})
}
