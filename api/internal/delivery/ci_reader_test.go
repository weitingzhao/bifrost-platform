package delivery

import (
	"context"
	"errors"
	"testing"

	"k8s.io/client-go/dynamic"
)

// With a reader set, CIRun and Succeeded answer from it and list no runs (a
// Service with no cluster would fail to); without one they read the runs.
func TestCIRunAsksTheReaderWhenSet(t *testing.T) {
	s := &Service{dynamicFactory: func() (dynamic.Interface, error) { return nil, errors.New("the runs were listed") }}
	facts := s.PolicyFacts()
	if _, _, err := facts.CIRun(context.Background(), "bifrost-platform", factSHA1); err == nil {
		t.Fatal("without a reader the runs were not read")
	}
	asked := ""
	s.SetCIReader(func(_ context.Context, repo, sha string) (string, string, error) {
		asked = repo + "@" + sha
		return CISucceeded, "ci-platform-1", nil
	})
	status, run, err := facts.CIRun(context.Background(), "bifrost-platform", factSHA1)
	if err != nil || status != CISucceeded || run != "ci-platform-1" || asked != "bifrost-platform@"+factSHA1 {
		t.Fatalf("CIRun: %q %q %v, asked %q", status, run, err, asked)
	}
	if ok, err := facts.Succeeded(context.Background(), "bifrost-platform", factSHA1); err != nil || !ok {
		t.Fatalf("Succeeded: %v %v", ok, err)
	}
	s.SetCIReader(func(context.Context, string, string) (string, string, error) { return "", "", nil })
	if ok, err := facts.Succeeded(context.Background(), "bifrost-platform", factSHA1); err != nil || ok {
		t.Fatalf("a commit the reader knows no verdict for is green: %v %v", ok, err)
	}
	s.SetCIReader(nil)
	if _, _, err := facts.CIRun(context.Background(), "bifrost-platform", factSHA1); err == nil {
		t.Fatal("after the reader was removed the runs were not read")
	}
}
