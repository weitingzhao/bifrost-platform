package delivery

import (
	"context"
	"fmt"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// PrepareNamedDeliverRun is pure. It shares StartPipelineRun's parameter,
// service-account, workspace and declaration checks. No gate or Create occurs.
func PrepareNamedDeliverRun(in NamedDeliverInput, pipe *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	id, err := in.Identity()
	if err != nil {
		return nil, err
	}
	if pipe == nil || pipe.GetName() != STGReleasePipeline || pipe.GetNamespace() != STGReleaseNamespace {
		return nil, fmt.Errorf("wrong STG Pipeline")
	}
	if err = platformRunInputs(in.P, id.Params); err != nil {
		return nil, err
	}
	spec, _, err := buildPipelineRunSpec(STGReleasePipeline, in.P, "", id.Params, pipe)
	if err != nil {
		return nil, err
	}
	obj, err := unstructuredFrom(map[string]any{"apiVersion": "tekton.dev/v1", "kind": "PipelineRun", "spec": spec})
	if err != nil {
		return nil, err
	}
	obj.SetName(id.Name)
	obj.SetNamespace(STGReleaseNamespace)
	obj.SetLabels(id.Labels)
	obj.SetAnnotations(id.Annotations)
	obj.SetFinalizers([]string{STGReleaseFinalizer})
	if err = validateRunRequirements(pipe, obj); err != nil {
		return nil, err
	}
	// Pin the default task identity before admission rather than inheriting
	// a mutable cluster-wide default SA. Explicit overrides are not replaced.
	if template, ok := obj.Object["spec"].(map[string]any)["taskRunTemplate"].(map[string]any); ok {
		if _, present := template["serviceAccountName"]; !present {
			template["serviceAccountName"] = "default"
		}
	}
	// The shared constructor can follow mutable Pipeline declarations. B5 v1
	// only creates its frozen execution contract, so recovery never has to
	// reconstruct historical expectations from today's Pipeline.
	// Admission defaults are only allowed on readback, not in declarations:
	// an explicitly requested timeout would change the frozen v1 request.
	if !matchesSTGExecution(obj, false) {
		return nil, fmt.Errorf("STG release execution differs from the frozen v1 contract")
	}
	params, err := namedStrings(obj, "spec", "params")
	if err != nil {
		return nil, err
	}
	for k, v := range id.Params {
		if params[k] != v {
			return nil, fmt.Errorf("frozen param %s was dropped", k)
		}
	}
	return obj, nil
}

// ReadNamedDeliverRun is the recovery read. It preserves Kubernetes NotFound
// versus Forbidden/timeout; B5-c decides whether a 404 proves unsent using its
// binding, deadline and finalizer-probe facts. This method never creates.
func (a *STGReleaseAdapter) ReadNamedDeliverRun(ctx context.Context, in NamedDeliverInput) (*unstructured.Unstructured, error) {
	id, err := in.Identity()
	if err != nil {
		return nil, err
	}
	ctx, cancel := stgContext(ctx)
	defer cancel()
	dyn, err := a.dynamic()
	if err != nil {
		return nil, err
	}
	obj, err := dyn.Resource(pipelineRunGVR).Namespace(STGReleaseNamespace).Get(ctx, id.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if err = MatchNamedDeliverRun(obj, in, true); err != nil {
		return nil, err
	}
	return obj, nil
}

// CreateNamedDeliverRun never retries a Create. A lost response/409 is read
// back under the same name and matched before returning a UID. It returns the
// same actuation audit envelope as StartPipelineRun; B5-c persists its intent
// before calling and records the result. Authorization/permit belongs to B5-c.
func (a *STGReleaseAdapter) CreateNamedDeliverRun(ctx context.Context, in NamedDeliverInput) (resp cluster.ActuationResponse, obj *unstructured.Unstructured, err error) {
	resp = cluster.ActuationResponse{Action: "delivery.pipeline.run", GeneratedAt: a.now()}
	defer func() {
		if err != nil {
			resp.Message = err.Error()
		}
	}()
	id, err := in.Identity()
	if err != nil {
		return resp, nil, err
	}
	resp.Target = "PipelineRun/" + STGReleaseNamespace + "/" + id.Name
	ctx, cancel := stgContext(ctx)
	defer cancel()
	dyn, err := a.dynamic()
	if err != nil {
		return resp, nil, err
	}
	runs := dyn.Resource(pipelineRunGVR).Namespace(STGReleaseNamespace)
	if existing, e := runs.Get(ctx, id.Name, metav1.GetOptions{}); e == nil {
		if err = MatchNamedDeliverRun(existing, in, true); err != nil {
			return resp, nil, err
		}
		resp.OK, resp.Message = true, "Existing named STG release observed"
		return resp, existing, nil
	} else if !apierrors.IsNotFound(e) {
		return resp, nil, e
	}
	pipe, err := dyn.Resource(pipelineGVR).Namespace(STGReleaseNamespace).Get(ctx, STGReleasePipeline, metav1.GetOptions{})
	if err != nil {
		return resp, nil, err
	}
	desired, err := PrepareNamedDeliverRun(in, pipe)
	if err != nil {
		return resp, nil, err
	}
	// Last read before Create: platform deliver's generic window gate does
	// not require a window, so it cannot provide B5's stronger contract.
	if err = a.namedWindow(ctx, id.Who); err != nil {
		return resp, nil, err
	}
	created, createErr := runs.Create(ctx, desired, metav1.CreateOptions{})
	if createErr != nil {
		created, err = runs.Get(ctx, id.Name, metav1.GetOptions{})
		if err != nil {
			return resp, nil, fmt.Errorf("create outcome unknown: %w (readback: %v)", createErr, err)
		}
	}
	if err = MatchNamedDeliverRun(created, in, true); err != nil {
		return resp, nil, err
	}
	resp.OK, resp.Changed, resp.Message = true, createErr == nil, "Named STG release observed"
	return resp, created, nil
}

func (a *STGReleaseAdapter) namedWindow(ctx context.Context, who string) error {
	core, err := a.core()
	if err != nil {
		return err
	}
	cm, err := core.CoreV1().ConfigMaps(STGReleaseNamespace).Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
	if err != nil {
		return err
	}
	rec, ok := parseWindowCM(cm)
	expires, e := time.Parse(time.RFC3339, rec.ExpiresAt)
	if !ok || e != nil || !a.now().Before(expires) || rec.Who != who || rec.What != "bifrost-platform" || rec.Env != "stg" {
		return fmt.Errorf("STG release requires its live who/what/env window")
	}
	return nil
}
