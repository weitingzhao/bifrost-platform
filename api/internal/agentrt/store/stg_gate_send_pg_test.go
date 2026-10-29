package store_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/stgrelease"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

// b5Runs uses the real frozen-object constructor and identity matcher. It
// models a lost response after creation, without running a control loop.
type b5Runs struct {
	pipe, obj  *unstructured.Unstructured
	creates    int
	lost       bool
	before     func()
	afterCheck func()
	readErr    error
}

func b5RunAdapter(t *testing.T) *b5Runs {
	t.Helper()
	raw, err := os.ReadFile("../../delivery/testdata/stg_delivery_pipeline.yaml")
	ok(t, err)
	var obj map[string]any
	ok(t, yaml.Unmarshal(raw, &obj))
	return &b5Runs{pipe: &unstructured.Unstructured{Object: obj}}
}
func (r *b5Runs) ReadNamedDeliverRun(_ context.Context, in delivery.NamedDeliverInput) (*unstructured.Unstructured, error) {
	if r.readErr != nil {
		return nil, r.readErr
	}
	if r.obj == nil {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: "tekton.dev", Resource: "pipelineruns"}, "stg")
	}
	return r.obj.DeepCopy(), delivery.MatchNamedDeliverRun(r.obj, in, true)
}
func (r *b5Runs) CreatePermittedNamedDeliverRun(ctx context.Context, in delivery.NamedDeliverInput, permit func(context.Context) error) (cluster.ActuationResponse, *unstructured.Unstructured, error) {
	if r.before != nil {
		r.before()
	}
	if err := permit(ctx); err != nil {
		return cluster.ActuationResponse{}, nil, err
	}
	if r.afterCheck != nil {
		r.afterCheck()
	}
	obj, err := delivery.PrepareNamedDeliverRun(in, r.pipe)
	if err != nil {
		return cluster.ActuationResponse{}, nil, err
	}
	r.creates++
	obj.SetUID("uid-one")
	r.obj = obj
	if r.lost {
		return cluster.ActuationResponse{}, nil, context.DeadlineExceeded
	}
	return cluster.ActuationResponse{OK: true}, obj, nil
}

// Serial: FreezeEventClock. PROD's none rule creates zero objects through
// the same Sender entry as the success and lost-response controls.
func TestStgGatePGSenderNoneAndLostResponse(t *testing.T) {
	for _, mode := range []string{"success", "lost response", "none", "expired after reads", "generation changed"} {
		t.Run(mode, func(t *testing.T) {
			x := b5Ready(t)
			runs := b5RunAdapter(t)
			sender := stgrelease.Sender{DB: x.w.rt, Runs: runs, Gate: x.src, FinalizerGuaranteed: true}
			if mode == "none" {
				x.none(t)
			}
			if mode == "lost response" {
				runs.lost = true
			}
			if mode == "expired after reads" {
				runs.before = func() { x.advance(x.at.Add(3 * time.Minute)) }
			}
			if mode == "generation changed" {
				var gen int64 = 1
				sender.Gate.Generation = func() int64 { return gen }
				runs.before = func() { gen = 2 }
			}
			d, err := sender.Start(ctx, b5Batch, 1)
			s := x.read(t)
			tr := s.Batches[b5Batch].Tries[1]
			switch mode {
			case "success", "lost response":
				ok(t, err)
				if !d.Allowed || runs.creates != 1 || tr.Bound.Record.String("uid") != "uid-one" {
					t.Fatal(d, err, runs.creates, tr)
				}
				action, err := sender.Recover(ctx, b5Batch, 1)
				ok(t, err)
				if action != "bound" || runs.creates != 1 {
					t.Fatal("restart duplicated UID", action, runs.creates)
				}
			case "none":
				ok(t, err)
				if d.Allowed || d.Why != "rule" || runs.creates != 0 || tr.Started.Seq != 0 {
					t.Fatal("PROD none created", d, runs.creates)
				}
			default:
				if err == nil || runs.creates != 0 || tr.Started.Seq == 0 {
					t.Fatal("late sender created", d, err, runs.creates)
				}
			}
		})
	}
}

// Serial: FreezeEventClock. Unknown GETs keep the slot; a retained-object
// 404 under withdrawn authorization isolates without refund or Create.
func TestStgGatePGSenderAbsenceAndQuarantine(t *testing.T) {
	for _, mode := range []string{"none absence", "no retention", "bound lost", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			x := b5Ready(t)
			p := x.permit(t)
			x.advance(p.Deadline.Add(time.Second))
			runs := b5RunAdapter(t)
			sender := stgrelease.Sender{DB: x.w.rt, Runs: runs, Gate: x.src, FinalizerGuaranteed: mode != "no retention"}
			if mode == "none absence" {
				x.none(t)
			}
			if mode == "bound lost" {
				b5Append(t, x.w, b5Record("bound", "batch", "try", 1, "uid", "lost-uid"))
			}
			if mode == "unknown" {
				runs.readErr = errors.New("403 forbidden")
			}
			before := x.read(t).LastSeq
			action, err := sender.Recover(ctx, b5Batch, 1)
			ok(t, err)
			s := x.read(t)
			tr := s.Batches[b5Batch].Tries[1]
			if runs.creates != 0 {
				t.Fatal("recovery created", mode, runs.creates)
			}
			switch mode {
			case "none absence":
				if action != "unknown_execution" || tr.Unsent.Seq != 0 || !tr.Occupied() || !tr.IsQuarantined() {
					t.Fatal(action, tr)
				}
			case "unknown":
				if action != "wait" || s.LastSeq != before || !tr.Occupied() {
					t.Fatal(action, tr)
				}
			case "no retention":
				if action != "unknown_execution" || !tr.IsQuarantined() || !tr.Occupied() {
					t.Fatal(action, tr)
				}
			case "bound lost":
				if action != "object_lost" || !tr.IsQuarantined() || !tr.Occupied() {
					t.Fatal(action, tr)
				}
			}
		})
	}
}
