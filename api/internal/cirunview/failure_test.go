package cirunview

import (
	"reflect"
	"testing"
)

func TestRunFailure(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		failed []FailedTask
		tasks  []string
		tests  []string
		class  string
	}{
		{"tests", []FailedTask{{Task: "test-api", NamesTests: true, Tests: []string{"b.TestB", "a.TestA"}}, {Task: "test-api-store", NamesTests: true, Tests: []string{"a.TestA"}}},
			[]string{"test-api", "test-api-store"}, []string{"a.TestA", "b.TestB"}, "test"},
		{"did not compile", []FailedTask{{Task: "test-api", NamesTests: true}}, []string{"test-api"}, nil, "build"},
		{"console", []FailedTask{{Task: "test-console"}}, []string{"test-console"}, nil, ""},
		{"a test and the console", []FailedTask{{Task: "test-api", NamesTests: true, Tests: []string{"a.TestA"}}, {Task: "test-console"}},
			[]string{"test-api", "test-console"}, []string{"a.TestA"}, "test"},
		{"none kept", nil, nil, nil, ""},
	} {
		tasks, tests, class := Failure(tc.failed)
		if !reflect.DeepEqual(tasks, tc.tasks) || !reflect.DeepEqual(tests, tc.tests) || class != tc.class {
			t.Errorf("%s: %v %v %q", tc.name, tasks, tests, class)
		}
	}
}

func TestFailureDoesNotMutateInputs(t *testing.T) {
	tests := []string{"z.Test", "a.Test", "z.Test"}
	input := []FailedTask{{Task: "api", Tests: tests, NamesTests: true}}
	_, got, _ := Failure(input)
	if !reflect.DeepEqual(tests, []string{"z.Test", "a.Test", "z.Test"}) {
		t.Fatal("input reordered")
	}
	got[0] = "changed"
	if tests[1] != "a.Test" {
		t.Fatal("output aliases input")
	}
}
