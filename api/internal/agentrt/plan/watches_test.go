package plan

import (
	"fmt"
	"strings"
	"testing"
)

// Watches lists every outside record a rule depends on, each once, sorted;
// task, sign-off and manual atoms are the runtime's own and are not watched.
func TestWatches(t *testing.T) {
	commit := strings.Repeat("e", 40)
	spec := strings.Repeat("ab", 32)
	doc := `{"all":[{"approval_executed":16},{"approval_executed":16},{"question_answered":{"question":"Q3","option":"A"}},` +
		`{"any":[{"ci_succeeded":{"repo":"bifrost-platform","commit":"` + commit + `"}},{"task_accepted":"t_01M4XQ4ZZKHA2YMMP11478VAXY"}]},` +
		`{"gates_passed":[{"test":"T19","spec_sha256":"` + spec + `"}],"env":"stg"},` +
		`{"any":[{"gates_passed":[{"test":"T20","spec_sha256":"` + spec + `"}],"env":"prod"},{"owner_signoff":"x"},{"manual":"m"}]}]}`
	r, err := Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprint(r.Watches())
	want := fmt.Sprint([]Watch{{WatchApproval, "16"}, {WatchCI, "bifrost-platform@" + commit}, {WatchDeploy, "prod"},
		{WatchDeploy, "stg"}, {WatchQuestion, "Q3"}})
	if got != want {
		t.Fatalf("watches = %s; want %s", got, want)
	}
	empty, err := Parse([]byte(`{}`))
	if err != nil || len(empty.Watches()) != 0 {
		t.Fatalf("an empty rule watches %v (%v)", empty.Watches(), err)
	}
}
