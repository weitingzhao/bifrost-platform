package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

// RunEvidenceArchive must read the committed run_evidence row of this exact
// UID, including its children's raw evidence. An in-memory observation is not
// an archive receipt. B5-e supplies the store implementation.
type RunEvidenceArchive interface {
	HasRunEvidence(context.Context, STGRunIdentity) (bool, error)
}

func finalizerPatch(obj *unstructured.Unstructured) ([]byte, error) {
	if obj.GetResourceVersion() == "" || obj.GetUID() == "" {
		return nil, ErrSTGUnknown
	}
	i := slices.Index(obj.GetFinalizers(), STGReleaseFinalizer)
	if i < 0 {
		return nil, nil
	}
	return json.Marshal([]map[string]any{
		{"op": "test", "path": "/metadata/uid", "value": string(obj.GetUID())},
		{"op": "test", "path": "/metadata/resourceVersion", "value": obj.GetResourceVersion()},
		{"op": "test", "path": fmt.Sprintf("/metadata/finalizers/%d", i), "value": STGReleaseFinalizer},
		{"op": "remove", "path": fmt.Sprintf("/metadata/finalizers/%d", i)},
	})
}

// RemoveArchivedFinalizer only releases retained evidence after a durable
// receipt. Conflict/error is returned to the caller for a later read/retry;
// it never replaces the whole finalizer list or removes another controller's.
func (a *STGReleaseAdapter) RemoveArchivedFinalizer(ctx context.Context, in NamedDeliverInput, uid types.UID) error {
	id, err := in.Identity()
	if err != nil {
		return err
	}
	if uid == "" || a.Archive == nil {
		return ErrSTGUnknown
	}
	ctx, cancel := stgContext(ctx)
	defer cancel()
	identity := STGRunIdentity{STGReleaseNamespace, id.Name, uid}
	archived, err := a.Archive.HasRunEvidence(ctx, identity)
	if err != nil {
		return err
	}
	if !archived {
		return fmt.Errorf("run evidence has not been archived")
	}
	dyn, err := a.dynamic()
	if err != nil {
		return err
	}
	res := dyn.Resource(pipelineRunGVR).Namespace(STGReleaseNamespace)
	obj, err := res.Get(ctx, id.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		// The archive was verified first. A prior successful removal may
		// have completed deletion while its response was lost.
		return nil
	}
	if err != nil {
		return err
	}
	if obj.GetUID() != uid {
		return ErrSTGCollision
	}
	if err = MatchNamedDeliverRun(obj, in, false); err != nil {
		return err
	}
	status := stgRunStatus(obj)
	if status != "Succeeded" && status != "Failed" {
		return fmt.Errorf("run is not terminal")
	}
	patch, err := finalizerPatch(obj)
	if err != nil || patch == nil {
		return err
	}
	_, err = res.Patch(ctx, id.Name, types.JSONPatchType, patch, metav1.PatchOptions{})
	return err
}
