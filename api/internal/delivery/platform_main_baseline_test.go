package delivery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

// Calls and pipeline declarations are identical to the generator executed on
// main 26a10b5. Normalize only the timestamp-derived name in the entire object.
func platformMainCreatedRuns(t *testing.T) map[string]any {
	t.Helper()
	out := map[string]any{}
	for _, name := range []string{"bifrost-deliver-platform", "bifrost-deliver-platform-prod"} {
		for _, call := range []struct {
			label, revision, ui string
		}{
			{"main", "main", "main"},
			{"main-pinned-ui", "main", factSHA2},
			{"no-revision", "", "main"},
			{"shared-name", "release-next", "release-next"},
		} {
			t.Run(name+"/"+call.label, func(t *testing.T) {
				ref := call.revision
				if ref == "" {
					ref = "main"
				}
				s, dyn, _ := pinnedService(t, name, platformRefs(ref, call.ui), nil, "")
				// Main checks main even when UI is pinned; the mirror has both refs.
				base := s.httpClient.Transport
				s.httpClient.Transport = contractTransport(func(r *http.Request) (*http.Response, error) {
					if call.label == "main-pinned-ui" && strings.Contains(r.URL.Path, "/bifrost-ui/branches") {
						return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`[{"name":"main","commit":{"id":"` + factSHA2 + `"}}]`)), Header: http.Header{}, Request: r}, nil
					}
					return base.RoundTrip(r)
				})
				extra := map[string]string{}
				if call.label == "main-pinned-ui" {
					extra["uiRevision"] = call.ui
				}
				resp, _, err := s.StartPipelineRun(context.Background(), name, call.revision, "", "tester", extra)
				if err != nil || !resp.OK {
					t.Fatal(resp, err)
				}
				run := createdContractRun(t, dyn)
				if !strings.HasPrefix(run.GetName(), name+"-") {
					t.Fatal(run.GetName())
				}
				run.SetName("timestamp-name")
				out[name+"/"+call.label] = run.Object
			})
		}
	}
	return out
}

func TestPlatformCreatedRunsUnchangedFromMain(t *testing.T) {
	want, err := os.ReadFile("testdata/platform_created_runs_main_26a10b5.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(platformMainCreatedRuns(t), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if string(append(got, '\n')) != string(want) {
		t.Fatal("whole platform PipelineRun JSON differs from main 26a10b5")
	}
}
