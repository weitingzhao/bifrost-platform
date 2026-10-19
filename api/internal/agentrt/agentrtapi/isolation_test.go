package agentrtapi_test

import (
	"net/http"
	"testing"
)

// T11 隔离: an upload that names a task's workspace is refused (422
// workspace_ref), whoever uploads it and whatever its kind, a task spec
// included; raw output is let through.
func TestUploadNamingAWorkspace(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	ref := []byte("# task\nstart from tasks/t_01M57YSQN6B43ZFNESD9VQ2HA9/a1\n")
	for _, c := range []struct{ who, kind string }{{"reporter", "task_spec"}, {"reporter", "brief"}, {"agentd:h1", "report"}} {
		code, out := call(t, s, c.who, "PUT", "artifacts/"+artifactID(ref), ref, "X-Artifact-Kind", c.kind, "X-Bifrost-Session", "s-rep")
		if code != http.StatusUnprocessableEntity || out["error"] != "workspace_ref" {
			t.Errorf("%s's %s naming a workspace: %d %v", c.who, c.kind, code, out)
		}
	}
	if code, _ := call(t, s, "agentd:h1", "PUT", "artifacts/"+artifactID(ref), ref, "X-Artifact-Kind", "log_tail"); code != http.StatusOK {
		t.Errorf("a log tail naming a workspace: %d", code)
	}
}
