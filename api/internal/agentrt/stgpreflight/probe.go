// Package stgpreflight composes the production B5 readers and non-persisted
// permission/admission probes. It is deliberately not wired into workers.
package stgpreflight

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	authnv1 "k8s.io/api/authentication/v1"
	authzv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type Options struct {
	Revision, Sample, SampleUID, WorkersNamespace string
}
type Result struct {
	Name, Reason string
	Passed       bool
}

// ReadOnlyTransport fails closed even if a later refactor accidentally calls
// a writer. Reviews are non-persisted; object CREATE/PATCH require DryRunAll.
// No DELETE, PUT, exec, proxy, token request or arbitrary dry-run is allowed.
func ReadOnlyTransport(next http.RoundTripper) http.RoundTripper {
	return transportFunc(func(r *http.Request) (*http.Response, error) {
		path := r.URL.Path
		read := r.Method == "GET" && !strings.Contains(path, "/proxy") && !strings.Contains(path, "/exec") && !strings.Contains(path, "/attach") && !strings.Contains(path, "/portforward")
		review := r.Method == "POST" && (path == "/apis/authentication.k8s.io/v1/selfsubjectreviews" || path == "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews")
		base := "/apis/tekton.dev/v1/namespaces/cicd/pipelineruns"
		dry := len(r.URL.Query()["dryRun"]) == 1 && r.URL.Query().Get("dryRun") == metav1.DryRunAll
		admission := dry && (r.Method == "POST" && path == base || r.Method == "PATCH" && strings.HasPrefix(path, base+"/stgrel-") && !strings.Contains(strings.TrimPrefix(path, base+"/"), "/"))
		if !read && !review && !admission {
			return nil, fmt.Errorf("preflight rejected persistent or unsupported request: %s %s", r.Method, path)
		}
		return next.RoundTrip(r)
	})
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func Run(ctx context.Context, cfg *rest.Config, httpClient *http.Client, o Options) []Result {
	var results []Result
	check := func(name string, f func() (string, error)) bool {
		reason, err := f()
		if err != nil {
			reason = err.Error()
		}
		results = append(results, Result{Name: name, Passed: err == nil, Reason: reason})
		return err == nil
	}
	core, err := kubernetes.NewForConfigAndClient(cfg, httpClient)
	if err != nil {
		return []Result{{Name: "client", Reason: "invalid Kubernetes client configuration"}}
	}
	dyn, err := dynamic.NewForConfigAndClient(cfg, httpClient)
	if err != nil {
		return []Result{{Name: "client", Reason: "invalid dynamic client configuration"}}
	}
	a := &delivery.STGReleaseAdapter{
		Core:    func() (kubernetes.Interface, error) { return core, nil },
		Dynamic: func() (dynamic.Interface, error) { return dyn, nil },
		// Health must not inherit the Kubernetes transport's credentials.
		// ReadHealth fixes the STG URL and refuses redirects.
		HTTP: &http.Client{Timeout: 10 * time.Second},
	}
	if !check("workers_identity", func() (string, error) {
		if o.WorkersNamespace != "bifrost-platform-prod" && o.WorkersNamespace != "bifrost-platform-stg" {
			return "", fmt.Errorf("workers namespace must be a platform environment")
		}
		d, err := core.AppsV1().Deployments(o.WorkersNamespace).Get(ctx, "platform-workers", metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		who, err := core.AuthenticationV1().SelfSubjectReviews().Create(ctx, &authnv1.SelfSubjectReview{}, metav1.CreateOptions{})
		if err != nil {
			return "", err
		}
		want := "system:serviceaccount:" + o.WorkersNamespace + ":" + d.Spec.Template.Spec.ServiceAccountName
		if d.Spec.Template.Spec.ServiceAccountName == "" || who.Status.UserInfo.Username != want {
			return "", fmt.Errorf("client is not the deployed workers service account %s", want)
		}
		return want + "; deployment UID=" + string(d.UID), nil
	}) {
		return results // Never test admission using an unrelated/privileged identity.
	}
	check("rbac", func() (string, error) {
		return "all required delivery and observation permissions allowed", a.ProbeAccess(ctx)
	})
	// Window writes are not exercised, but must be authorized before opening.
	check("window_rbac", func() (string, error) {
		for _, verb := range []string{"create", "update"} {
			access := &authzv1.ResourceAttributes{Namespace: "cicd", Resource: "configmaps", Verb: verb}
			if verb == "update" {
				access.Name = "bifrost-release-window"
			}
			r, err := core.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authzv1.SelfSubjectAccessReview{Spec: authzv1.SelfSubjectAccessReviewSpec{ResourceAttributes: access}}, metav1.CreateOptions{})
			if err != nil {
				return "", err
			}
			if !r.Status.Allowed || r.Status.Denied || r.Status.EvaluationError != "" {
				return "", fmt.Errorf("window %s permission denied or unknown", verb)
			}
		}
		return "create/update authorized; no window mutation sent", nil
	})
	check("window_read", func() (string, error) {
		w, err := a.ReadSTGWindow(ctx)
		if err != nil {
			return "", err
		}
		if w.ResourceVersion == "" {
			return "ConfigMap absent (404); no creation attempted", nil
		}
		return fmt.Sprintf("resourceVersion=%s generation=%d live=%t; no renewal or removal", w.ResourceVersion, w.Generation, w.Live(time.Now())), nil
	})
	check("workloads", func() (string, error) {
		w, err := a.ObserveWorkloads(ctx)
		return fmt.Sprintf("%d STG deployments passed strict PodDigests and template checks", len(w)), err
	})
	check("argo_read", func() (string, error) {
		app, err := dyn.Resource(schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}).Namespace("cicd").Get(ctx, "bifrost-platform-stg", metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		return "application UID=" + string(app.GetUID()), nil
	})
	// Health uses its own fixed internal STG URL, never a caller-provided URL.
	check("health", func() (string, error) {
		h := a.ReadHealth(ctx)
		if h.Outcome != "passed" {
			return "", fmt.Errorf("STG health classified %s: reason=%s http_status=%d", h.Outcome, h.Reason, h.HTTPStatus)
		}
		return "STG service JSON status=ok", nil
	})
	ci := &landingci.Client{Dynamic: a.Dynamic}
	var evidence landingci.ReleaseEvidence
	check("exact_ci", func() (string, error) {
		var err error
		evidence, err = ci.ReadRelease(ctx, o.Revision)
		if err != nil {
			return "", err
		}
		if _, err = evidence.Record(); err != nil {
			return "", fmt.Errorf("exact latest CI: status=%s reason=%s", evidence.Status, evidence.Reason)
		}
		return fmt.Sprintf("name=%s UID=%s P=%s U=%s infra=%s", evidence.Run, evidence.UID, evidence.PlatformCommit, evidence.UICommit, evidence.InfraCommit), nil
	})
	check("run_and_task_evidence", func() (string, error) {
		if o.Sample == "" || o.SampleUID == "" {
			return "", fmt.Errorf("held sample name and independent UID required; unproven")
		}
		obs, err := a.ObserveRun(ctx, delivery.STGRunIdentity{Namespace: "cicd", Name: o.Sample, UID: types.UID(o.SampleUID)})
		if err != nil {
			return "", err
		}
		if _, err = obs.ExternalContent(); err != nil {
			return "", fmt.Errorf("sample must have complete successful clone/build/pin-sync evidence: %w", err)
		}
		return fmt.Sprintf("UID=%s children=%d", obs.Identity.UID, len(obs.Children)), nil
	})
	check("finalizer_admission", func() (string, error) {
		if _, err := evidence.Record(); err != nil {
			return "", fmt.Errorf("exact CI unavailable; admission inputs unproven")
		}
		if o.Sample == "" || o.SampleUID == "" {
			return "", fmt.Errorf("held sample required for server dry-run removal; unproven")
		}
		obj, err := dyn.Resource(schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"}).Namespace("cicd").Get(ctx, o.Sample, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		sample, err := sampleInput(obj)
		if err != nil {
			return "", err
		}
		next := delivery.NamedDeliverInput{Batch: sample.Batch + 1, Try: 1, IntentSeq: 1, P: o.Revision, U: evidence.UICommit, Infra: evidence.InfraCommit}
		return "CREATE and guarded finalizer PATCH accepted with dryRun=All; nothing persisted", a.ProbeFinalizerAdmission(ctx, next, sample, types.UID(o.SampleUID))
	})
	return results
}

func sampleInput(obj *unstructured.Unstructured) (delivery.NamedDeliverInput, error) {
	var in delivery.NamedDeliverInput
	var err error
	in.Batch, err = strconv.ParseInt(obj.GetLabels()["bifrost.io/stg-release-n"], 10, 64)
	if err != nil {
		return in, err
	}
	in.Try, err = strconv.ParseInt(obj.GetLabels()["bifrost.io/stg-release-try"], 10, 64)
	if err != nil {
		return in, err
	}
	in.IntentSeq, err = strconv.ParseInt(obj.GetAnnotations()["bifrost.io/stg-release-intent"], 10, 64)
	if err != nil {
		return in, err
	}
	params, _, _ := unstructured.NestedSlice(obj.Object, "spec", "params")
	for _, raw := range params {
		p, _ := raw.(map[string]any)
		v, _ := p["value"].(string)
		switch p["name"] {
		case "revision":
			in.P = v
		case "uiRevision":
			in.U = v
		case "infraRevision":
			in.Infra = v
		}
	}
	return in, delivery.MatchNamedDeliverRun(obj, in, true)
}
