package releasepolicy_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

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
