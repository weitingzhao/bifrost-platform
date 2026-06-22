package agentdeploy

import "testing"

func TestSanitizeDeployRemote(t *testing.T) {
	got, err := sanitizeDeployRemote("vision@192.168.10.50 # comment")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "vision@192.168.10.50" {
		t.Fatalf("got %q", got)
	}
}

func TestSanitizeDeployRemoteRejectsSpaces(t *testing.T) {
	_, err := sanitizeDeployRemote("vision@192.168.10.50 bad")
	if err == nil {
		t.Fatal("expected error for spaces in target")
	}
}
