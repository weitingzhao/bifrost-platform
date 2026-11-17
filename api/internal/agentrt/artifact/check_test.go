package artifact

import (
	"context"
	"errors"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
)

// 2026-11-17, store split step F2: the artifact package validates view artifacts through a hook; an unbound hook must refuse.
// Both ways in are held, and then the positive control: a registered check
// is the one called, with the kind and the body, and its refusal is the
// answer. The transaction and the database are nil on purpose: the check
// comes before either is used. Not parallel: it swaps the package's check.
func TestNothingIsStoredWithoutARegisteredCheck(t *testing.T) {
	saved := bodyCheck.Swap(nil)
	defer bodyCheck.Store(saved)
	ctx := context.Background()
	view := []byte(`{"kind":"agentrt.plan_view/v1"}`)
	if id, err := PutArtifact(ctx, nil, "controller", "other", "application/json", view); !errors.Is(err, ErrNoCheck) || id != "" {
		t.Fatalf("PutArtifact with no check registered: %q, %v", id, err)
	}
	if id, created, err := UploadArtifact(ctx, nil, actor.Writer{Actor: "thread:a", Token: "t"}, "other", "application/json", view); !errors.Is(err, ErrNoCheck) || id != "" || created {
		t.Fatalf("UploadArtifact with no check registered: %q, %v, %v", id, created, err)
	}
	// A body that is no view artifact is refused too: without the check the
	// package cannot tell.
	if _, err := PutArtifact(ctx, nil, "controller", "report", "text/plain", []byte("a report")); !errors.Is(err, ErrNoCheck) {
		t.Fatalf("a report with no check registered: %v", err)
	}

	refused := errors.New("not a valid view")
	var kind, body string
	RegisterCheck(func(k string, b []byte) error {
		kind, body = k, string(b)
		return refused
	})
	if _, err := PutArtifact(ctx, nil, "controller", "other", "application/json", view); !errors.Is(err, refused) || kind != "other" || body != string(view) {
		t.Fatalf("PutArtifact with a check registered: %v (kind %q, body %q)", err, kind, body)
	}
	kind, body = "", ""
	if _, _, err := UploadArtifact(ctx, nil, actor.Writer{Actor: "thread:a", Token: "t"}, "other", "application/json", view); !errors.Is(err, refused) || kind != "other" || body != string(view) {
		t.Fatalf("UploadArtifact with a check registered: %v (kind %q, body %q)", err, kind, body)
	}
}
