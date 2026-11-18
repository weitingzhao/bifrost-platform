//bifrost:owner plan

package store_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// 2026-11-17, store split step F2: the artifact package validates view artifacts through a hook; an unbound hook must refuse.
// This is the other half: a binary that links this package (platform-api
// does, and every writer of artifacts) has the check registered by its init,
// and it is the plan view's and the external work description's check, not
// a stand-in. The transaction is nil on purpose: each refusal comes before
// it is used.
func TestLinkingTheStoreRegistersTheViewArtifactCheck(t *testing.T) {
	t.Parallel()
	_, err := artifact.PutArtifact(ctx, nil, "controller", "other", "application/json", []byte(`{"kind":"`+planview.Kind+`"}`))
	if errors.Is(err, artifact.ErrNoCheck) || !errors.Is(err, eventlog.ErrInvalid) || !strings.Contains(err.Error(), "plan view artifact:") {
		t.Fatalf("a broken plan view: %v, want the plan view's refusal", err)
	}
	_, _, err = store.UploadArtifact(ctx, nil, wr("thread:check"), "other", "application/json", []byte(`{"kind":"agentrt.external_work/v1"}`))
	if errors.Is(err, artifact.ErrNoCheck) || !errors.Is(err, eventlog.ErrInvalid) {
		t.Fatalf("a broken external work description: %v, want the description's refusal", err)
	}
}
