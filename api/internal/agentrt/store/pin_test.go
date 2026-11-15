package store_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

const coordinatorSeat = "seat:coordinator"

// §2.2: what builds a task anywhere but the tasks route has no pin to set.
// TaskProposal (goals, briefs, plans, imports) and Task (the controller's
// checks and gates) export no pin and no session: only PinnedTask does.
func TestOnlyPinnedTaskCarriesAPin(t *testing.T) {
	t.Parallel()
	for _, v := range []any{store.TaskProposal{}, store.Task{}} {
		ty := reflect.TypeOf(v)
		for i := range ty.NumField() {
			f := ty.Field(i)
			if f.IsExported() && (strings.Contains(strings.ToLower(f.Name), "pin") || strings.Contains(strings.ToLower(f.Name), "session")) {
				t.Errorf("%s exports %s", ty.Name(), f.Name)
			}
		}
	}
	if _, ok := reflect.TypeOf(store.PinnedTask{}).FieldByName("ExecutorPin"); !ok {
		t.Fatal("PinnedTask has no ExecutorPin")
	}
}
