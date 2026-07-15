package devagent

import "testing"

func TestRequireVerifyPassedForDone(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		status       string
		verifyCmd    string
		verifyPassed bool
		wantErr      bool
	}{
		{name: "in_progress ignores verify", status: "in_progress", verifyCmd: "make test", verifyPassed: false, wantErr: false},
		{name: "done empty verify ok", status: "done", verifyCmd: "", verifyPassed: false, wantErr: false},
		{name: "done with verify false rejected", status: "done", verifyCmd: "make test", verifyPassed: false, wantErr: true},
		{name: "done with verify true ok", status: "done", verifyCmd: "make test", verifyPassed: true, wantErr: false},
		{name: "complete alias rejected", status: "complete", verifyCmd: "echo ok", verifyPassed: false, wantErr: true},
		{name: "complete alias passed", status: "COMPLETE", verifyCmd: "echo ok", verifyPassed: true, wantErr: false},
		{name: "whitespace verify treated empty", status: "done", verifyCmd: "  ", verifyPassed: false, wantErr: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := requireVerifyPassedForDone(tc.status, tc.verifyCmd, tc.verifyPassed)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestPhaseVerifyCmd(t *testing.T) {
	t.Parallel()
	bp := &ProgramBlueprint{
		Phases: []PhaseBlueprint{
			{ID: "a", VerifyCmd: ""},
			{ID: "b", VerifyCmd: " make lint "},
		},
	}
	if got := phaseVerifyCmd(bp, "a"); got != "" {
		t.Fatalf("a = %q", got)
	}
	if got := phaseVerifyCmd(bp, "b"); got != "make lint" {
		t.Fatalf("b = %q", got)
	}
	if got := phaseVerifyCmd(bp, "missing"); got != "" {
		t.Fatalf("missing = %q", got)
	}
}
