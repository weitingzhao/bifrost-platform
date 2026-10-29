package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	kfake "k8s.io/client-go/kubernetes/fake"
	kt "k8s.io/client-go/testing"
)

var stgTestNow = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func stgInput() NamedDeliverInput {
	return NamedDeliverInput{Batch: 1, Try: 1, IntentSeq: 17, P: strings.Repeat("a", 40), U: strings.Repeat("b", 40), Infra: strings.Repeat("c", 40)}
}
func stgPipe(t *testing.T) *unstructured.Unstructured {
	t.Helper()
	return readYAMLObjects(t, "testdata/stg_delivery_pipeline.yaml")[0]
}
func stgWindow(t *testing.T) *corev1.ConfigMap {
	t.Helper()
	id, err := stgInput().Identity()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(windowRecord{Who: id.Who, What: "bifrost-platform", Env: "stg", ExpiresAt: stgTestNow.Add(5 * time.Minute).Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: releaseWindowConfigMap, Namespace: STGReleaseNamespace, ResourceVersion: "1"}, Data: map[string]string{releaseWindowDataKey: string(raw)}}
}
func stgAdapter(t *testing.T, objects ...runtime.Object) (*STGReleaseAdapter, *dynfake.FakeDynamicClient, *kfake.Clientset) {
	t.Helper()
	objects = append(objects, stgPipe(t))
	dyn := dynfake.NewSimpleDynamicClient(runtime.NewScheme(), objects...)
	core := kfake.NewSimpleClientset(stgWindow(t))
	a := &STGReleaseAdapter{Dynamic: func() (dynamic.Interface, error) { return dyn, nil }, Core: func() (kubernetes.Interface, error) { return core, nil }, Now: func() time.Time { return stgTestNow }}
	return a, dyn, core
}
func namedObject(t *testing.T) *unstructured.Unstructured {
	t.Helper()
	obj, err := PrepareNamedDeliverRun(stgInput(), stgPipe(t))
	if err != nil {
		t.Fatal(err)
	}
	obj.SetUID("run-uid")
	obj.SetResourceVersion("1")
	return obj
}
func stgSet(t *testing.T, o *unstructured.Unstructured, value any, path ...string) {
	t.Helper()
	if err := unstructured.SetNestedField(o.Object, value, path...); err != nil {
		t.Fatal(err)
	}
}
func successful(o *unstructured.Unstructured) {
	o.Object["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": "True"}}}
}
func trackSTGCreates(t *testing.T, dyn *dynfake.FakeDynamicClient, mode string) *int {
	t.Helper()
	count := 0
	dyn.PrependReactor("create", "pipelineruns", func(action kt.Action) (bool, runtime.Object, error) {
		count++
		obj := action.(kt.CreateAction).GetObject().(*unstructured.Unstructured).DeepCopy()
		obj.SetUID(types.UID(fmt.Sprintf("created-%d", count)))
		obj.SetResourceVersion("1")
		if err := dyn.Tracker().Create(pipelineRunGVR, obj, STGReleaseNamespace); err != nil {
			return true, nil, err
		}
		if mode == "lost" {
			return true, nil, context.DeadlineExceeded
		}
		if mode == "409" {
			return true, nil, apierrors.NewAlreadyExists(pipelineRunGVR.GroupResource(), obj.GetName())
		}
		return true, obj, nil
	})
	return &count
}
func TestSTGIdentityAndPrepareContract(t *testing.T) {
	t.Parallel()
	in := stgInput()
	id, err := in.Identity()
	if err != nil {
		t.Fatal(err)
	}
	if id.Name != "stgrel-1-1-aaaaaaaa" || id.Who != "stgrel-bifrost-platform-1-1" || len(id.Fingerprint) != 64 {
		t.Fatal(id)
	}
	if len(validation.IsDNS1123Subdomain(id.Name)) != 0 {
		t.Fatal(id.Name)
	}
	for k, v := range id.Labels {
		if len(validation.IsQualifiedName(k))+len(validation.IsValidLabelValue(v)) != 0 {
			t.Fatal(k, v)
		}
	}
	o := namedObject(t)
	if err := MatchNamedDeliverRun(o, in, true); err != nil {
		t.Fatal(err)
	}
	spec, _, err := buildPipelineRunSpec(STGReleasePipeline, in.P, "", id.Params, stgPipe(t))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := unstructuredFrom(map[string]any{"spec": spec})
	if err != nil {
		t.Fatal(err)
	}
	// Named B5 runs pin the otherwise implicit default SA before admission.
	if sa, _, _ := unstructured.NestedString(o.Object, "spec", "taskRunTemplate", "serviceAccountName"); sa != "default" {
		t.Fatal("named run did not pin its default SA", sa)
	}
	stgSet(t, ref, "default", "spec", "taskRunTemplate", "serviceAccountName")
	if !reflect.DeepEqual(o.Object["spec"], ref.Object["spec"]) {
		t.Fatal("named spec diverged from StartPipelineRun")
	}
	for _, mutation := range []func(*NamedDeliverInput){func(i *NamedDeliverInput) { i.P = "main" }, func(i *NamedDeliverInput) { i.IntentSeq = 0 }, func(i *NamedDeliverInput) { i.Batch = 9223372036854775807; i.Try = 9223372036854775807 }} {
		bad := in
		mutation(&bad)
		if _, err := bad.Identity(); err == nil {
			t.Fatal("invalid identity accepted", bad)
		}
	}
	pipe := stgPipe(t)
	stgSet(t, pipe, []any{map[string]any{"name": "unknown-required"}}, "spec", "workspaces")
	if _, err := PrepareNamedDeliverRun(in, pipe); err == nil {
		t.Fatal("unbound workspace")
	}
}
func TestSTGCreateRecoversLostResponseAndConflict(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"success", "lost", "409"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			a, dyn, _ := stgAdapter(t)
			count := trackSTGCreates(t, dyn, mode)
			for i := 0; i < 2; i++ {
				resp, obj, err := a.CreateNamedDeliverRun(context.Background(), stgInput())
				if err != nil || !resp.OK || obj.GetUID() != "created-1" {
					t.Fatal(resp, obj, err)
				}
			}
			if *count != 1 {
				t.Fatal("duplicate create", *count)
			}
		})
	}
}
func TestSTGCreateWindowRefusalsHaveControl(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"missing", "expired", "who", "what", "env", "malformed", "unreadable"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			control, dyn, _ := stgAdapter(t)
			count := trackSTGCreates(t, dyn, "")
			if _, _, err := control.CreateNamedDeliverRun(context.Background(), stgInput()); err != nil || *count != 1 {
				t.Fatal("control", err)
			}
			a, dyn, core := stgAdapter(t)
			count = trackSTGCreates(t, dyn, "")
			cm := stgWindow(t)
			rec, _ := parseWindowCM(cm)
			switch mode {
			case "missing":
				if err := core.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("configmaps"), STGReleaseNamespace, cm.Name); err != nil {
					t.Fatal(err)
				}
			case "expired":
				rec.ExpiresAt = stgTestNow.Format(time.RFC3339)
			case "who":
				rec.Who = "someone-else"
			case "what":
				rec.What = "bifrost-platform,bifrost-ui"
			case "env":
				rec.Env = "prod"
			case "unreadable":
				core.PrependReactor("get", "configmaps", func(kt.Action) (bool, runtime.Object, error) { return true, nil, errors.New("unreadable") })
			}
			if mode != "missing" {
				raw, _ := json.Marshal(rec)
				cm.Data[releaseWindowDataKey] = string(raw)
				if mode == "malformed" {
					cm.Data[releaseWindowDataKey] = "{"
				}
				if err := core.Tracker().Update(corev1.SchemeGroupVersion.WithResource("configmaps"), cm, STGReleaseNamespace); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := a.CreateNamedDeliverRun(context.Background(), stgInput()); err == nil || *count != 0 {
				t.Fatal(mode, err, *count)
			}
		})
	}
}
func TestSTGNamedRunCollisionIsNeverAdopted(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*unstructured.Unstructured){
		"intent": func(o *unstructured.Unstructured) {
			ann := o.GetAnnotations()
			ann["bifrost.io/stg-release-intent"] = "18"
			o.SetAnnotations(ann)
		},
		"label":     func(o *unstructured.Unstructured) { o.SetLabels(nil) },
		"params":    func(o *unstructured.Unstructured) { stgSet(t, o, []any{}, "spec", "params") },
		"finalizer": func(o *unstructured.Unstructured) { o.SetFinalizers(nil) },
		"uid":       func(o *unstructured.Unstructured) { o.SetUID("") },
		"pipeline":  func(o *unstructured.Unstructured) { stgSet(t, o, "other", "spec", "pipelineRef", "name") },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			obj := namedObject(t)
			if err := MatchNamedDeliverRun(obj, stgInput(), true); err != nil {
				t.Fatal("control", err)
			}
			mutate(obj)
			a, dyn, _ := stgAdapter(t, obj)
			count := trackSTGCreates(t, dyn, "")
			if _, _, err := a.CreateNamedDeliverRun(context.Background(), stgInput()); !errors.Is(err, ErrSTGCollision) || *count != 0 {
				t.Fatal(err, *count)
			}
		})
	}
}

type stgArchive struct {
	present bool
	err     error
	id      STGRunIdentity
}

func (a stgArchive) HasRunEvidence(_ context.Context, id STGRunIdentity) (bool, error) {
	if a.id != (STGRunIdentity{}) && a.id != id {
		return false, errors.New("wrong archive identity")
	}
	return a.present, a.err
}
func TestSTGFinalizerSurvivesEveryDeleteUntilArchive(t *testing.T) {
	t.Parallel()
	for _, entry := range []string{"delete_pipeline_run", "ttl"} {
		t.Run(entry, func(t *testing.T) {
			t.Parallel()
			a, dyn, _ := stgAdapter(t)
			count := trackSTGCreates(t, dyn, "")
			_, obj, err := a.CreateNamedDeliverRun(context.Background(), stgInput())
			if err != nil {
				t.Fatal(err)
			}
			successful(obj)
			if err := dyn.Tracker().Update(pipelineRunGVR, obj, STGReleaseNamespace); err != nil {
				t.Fatal(err)
			}
			// client-go's tracker does not implement apiserver finalizers. Model its
			// deletion contract explicitly; both entry points issue ordinary DELETE.
			dyn.PrependReactor("delete", "pipelineruns", func(action kt.Action) (bool, runtime.Object, error) {
				name := action.(kt.DeleteAction).GetName()
				got, e := dyn.Tracker().Get(pipelineRunGVR, STGReleaseNamespace, name)
				if e != nil {
					return true, nil, e
				}
				held := got.(*unstructured.Unstructured).DeepCopy()
				if !slices.Contains(held.GetFinalizers(), STGReleaseFinalizer) {
					return false, nil, nil
				}
				now := metav1.NewTime(stgTestNow)
				held.SetDeletionTimestamp(&now)
				held.SetResourceVersion("2")
				return true, nil, dyn.Tracker().Update(pipelineRunGVR, held, STGReleaseNamespace)
			})
			if entry == "delete_pipeline_run" {
				svc := &Service{dynamicFactory: func() (dynamic.Interface, error) { return dyn, nil }}
				if _, err = svc.DeletePipelineRun(context.Background(), STGReleaseNamespace, obj.GetName()); err != nil {
					t.Fatal(err)
				}
			} else {
				err = dyn.Resource(pipelineRunGVR).Namespace(STGReleaseNamespace).Delete(context.Background(), obj.GetName(), metav1.DeleteOptions{})
				if err != nil {
					t.Fatal(err)
				}
			}
			_, held, err := a.CreateNamedDeliverRun(context.Background(), stgInput())
			if err != nil || held.GetDeletionTimestamp() == nil || held.GetUID() != obj.GetUID() || *count != 1 {
				t.Fatal("lost retention", held, err, *count)
			}
			if err := a.RemoveArchivedFinalizer(context.Background(), stgInput(), held.GetUID()); err == nil {
				t.Fatal("released before archive")
			}
			a.Archive = stgArchive{present: true, id: STGRunIdentity{STGReleaseNamespace, held.GetName(), held.GetUID()}}
			if err := a.RemoveArchivedFinalizer(context.Background(), stgInput(), held.GetUID()); err != nil {
				t.Fatal(err)
			}
			after, err := dyn.Resource(pipelineRunGVR).Namespace(STGReleaseNamespace).Get(context.Background(), held.GetName(), metav1.GetOptions{})
			if err != nil || slices.Contains(after.GetFinalizers(), STGReleaseFinalizer) {
				t.Fatal(after, err)
			}
		})
	}
}
func TestSTGFinalizerPatchRefusesStaleOrUnarchived(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"control", "unarchived", "archive error", "running", "uid", "conflict"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			obj := namedObject(t)
			successful(obj)
			obj.SetFinalizers([]string{"other.example/hold", STGReleaseFinalizer})
			a, dyn, _ := stgAdapter(t, obj)
			a.Archive = stgArchive{present: true}
			uid := obj.GetUID()
			switch mode {
			case "unarchived":
				a.Archive = stgArchive{}
			case "archive error":
				a.Archive = stgArchive{err: errors.New("db")}
			case "running":
				delete(obj.Object, "status")
				if err := dyn.Tracker().Update(pipelineRunGVR, obj, STGReleaseNamespace); err != nil {
					t.Fatal(err)
				}
			case "uid":
				uid = "replacement"
			case "conflict":
				dyn.PrependReactor("patch", "pipelineruns", func(kt.Action) (bool, runtime.Object, error) {
					return true, nil, apierrors.NewConflict(pipelineRunGVR.GroupResource(), obj.GetName(), errors.New("changed"))
				})
			}
			err := a.RemoveArchivedFinalizer(context.Background(), stgInput(), uid)
			after, e := dyn.Resource(pipelineRunGVR).Namespace(STGReleaseNamespace).Get(context.Background(), obj.GetName(), metav1.GetOptions{})
			if e != nil {
				t.Fatal(e)
			}
			if mode == "control" {
				if err != nil || !reflect.DeepEqual(after.GetFinalizers(), []string{"other.example/hold"}) {
					t.Fatal(after.GetFinalizers(), err)
				}
			} else if err == nil || !slices.Contains(after.GetFinalizers(), STGReleaseFinalizer) {
				t.Fatal("removed on refusal", err)
			}
		})
	}
}

func TestSTGReadNamedPreservesAbsenceAndReadErrors(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"control", "not found", "forbidden", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			obj := namedObject(t)
			a, dyn, _ := stgAdapter(t, obj)
			count := trackSTGCreates(t, dyn, "")
			switch mode {
			case "not found":
				if err := dyn.Tracker().Delete(pipelineRunGVR, STGReleaseNamespace, obj.GetName()); err != nil {
					t.Fatal(err)
				}
			case "forbidden":
				dyn.PrependReactor("get", "pipelineruns", func(kt.Action) (bool, runtime.Object, error) {
					return true, nil, apierrors.NewForbidden(pipelineRunGVR.GroupResource(), obj.GetName(), errors.New("denied"))
				})
			case "timeout":
				dyn.PrependReactor("get", "pipelineruns", func(kt.Action) (bool, runtime.Object, error) { return true, nil, context.DeadlineExceeded })
			}
			got, err := a.ReadNamedDeliverRun(context.Background(), stgInput())
			switch mode {
			case "control":
				if err != nil || got.GetUID() != obj.GetUID() {
					t.Fatal(got, err)
				}
			case "not found":
				if !apierrors.IsNotFound(err) {
					t.Fatal(err)
				}
			case "forbidden":
				if !apierrors.IsForbidden(err) {
					t.Fatal(err)
				}
			case "timeout":
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal(err)
				}
			}
			if *count != 0 {
				t.Fatal("read sent create", *count)
			}
		})
	}
}

func TestSTGArchivedDeletionIsIdempotent(t *testing.T) {
	t.Parallel()
	a, dyn, _ := stgAdapter(t)
	a.Archive = stgArchive{present: true}
	if err := a.RemoveArchivedFinalizer(context.Background(), stgInput(), "already-archived-uid"); err != nil {
		t.Fatal(err)
	}
	a.Archive = stgArchive{}
	if err := a.RemoveArchivedFinalizer(context.Background(), stgInput(), "unarchived-uid"); err == nil {
		t.Fatal("absence treated as archived")
	}
	for _, action := range dyn.Actions() {
		if action.GetVerb() != "get" {
			t.Fatal("unexpected write", action)
		}
	}
}
