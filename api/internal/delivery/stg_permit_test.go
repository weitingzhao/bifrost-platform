package delivery

import (
	"context"
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	kt "k8s.io/client-go/testing"
)

func TestSTGPermitCheckedAfterSlowReadsBeforeCreate(t *testing.T) {
	t.Parallel()
	for _, late := range []bool{false, true} {
		t.Run(map[bool]string{false: "allowed", true: "expired during pipeline read"}[late], func(t *testing.T) {
			t.Parallel()
			a, dyn, _ := stgAdapter(t)
			creates := trackSTGCreates(t, dyn, "")
			valid, reads, checks := true, 0, 0
			dyn.PrependReactor("get", "pipelines", func(kt.Action) (bool, runtime.Object, error) {
				reads++
				if late {
					valid = false
				}
				return false, nil, nil
			})
			_, _, err := a.CreatePermittedNamedDeliverRun(context.Background(), stgInput(), func(context.Context) error {
				checks++
				if !valid {
					return errors.New("expired lease generation")
				}
				return nil
			})
			if reads != 1 || checks != 1 {
				t.Fatal("did not reach the final boundary", reads, checks)
			}
			if late {
				if err == nil || *creates != 0 {
					t.Fatal("expired permit created", err, *creates)
				}
			} else if err != nil || *creates != 1 {
				t.Fatal("same-entry success control", err, *creates)
			}
		})
	}
}

func TestSTGPermitRequiredButExistingRunStillReadable(t *testing.T) {
	t.Parallel()
	a, dyn, _ := stgAdapter(t, namedObject(t))
	creates := trackSTGCreates(t, dyn, "")
	if _, _, err := a.CreatePermittedNamedDeliverRun(context.Background(), stgInput(), nil); err == nil {
		t.Fatal("missing permit accepted")
	}
	// Existing objects are facts, even when no new send is allowed.
	_, obj, err := a.CreatePermittedNamedDeliverRun(context.Background(), stgInput(), func(context.Context) error { return errors.New("expired") })
	if err != nil || string(obj.GetUID()) != "run-uid" || *creates != 0 {
		t.Fatal(obj, err, *creates)
	}
}
