package agentd_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
)

type claimTransport func(*http.Request) (*http.Response, error)

func (f claimTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClaimBuildDiskHeadroomAndSlotGrantTogether(t *testing.T) {
	t.Parallel()
	for _, old := range []bool{false, true} {
		t.Run(map[bool]string{false: "current", true: "old"}[old], func(t *testing.T) {
			calls := 0
			client := &agentd.Client{Base: "http://platform.invalid", Host: "h1", Token: "fixture", Build: &agentd.Build{Commit: strings.Repeat("a", 40), SHA256: strings.Repeat("b", 64)}}
			client.HTTP = &http.Client{Transport: claimTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				header := make(http.Header)
				header.Set("Content-Type", "application/json")
				status, text := http.StatusNoContent, ""
				if old {
					for _, key := range []string{"build", "cpus", "disk_free_bytes"} {
						if body[key] != nil {
							status = http.StatusBadRequest
							b, _ := json.Marshal(map[string]string{"error": "bad_request", "message": "unknown field \"" + key + "\""})
							text = string(b)
							break
						}
					}
					if status == http.StatusNoContent {
						for _, key := range []string{"load1", "mem_avail_bytes", "mem_total_bytes", "root_free_bytes"} {
							if body[key] != nil {
								t.Fatalf("optional group left %s", key)
							}
						}
					}
				} else {
					if body["build"] == nil || body["disk_free_bytes"] == nil || body["load1"] != 1.25 {
						t.Fatalf("lost evidence: %v", body)
					}
					header.Set(agentd.GrantedSlotsHeader, "3")
					if calls == 2 {
						status, text = http.StatusOK, `{"task_id":"t_1","attempt_id":"a_1","granted_slots":0}`
					}
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(text))}, nil
			})}
			for range 2 {
				if _, err := client.ClaimHost(ctx, 1, []agentd.Tool{cursor}, &agentd.Disk{Host: 3 << 30, Root: 2 << 30}, fixtureHeadroom); err != nil {
					t.Fatal(err)
				}
				if !old && calls == 1 {
					if n, ok := client.Granted(); !ok || n != 3 {
						t.Fatalf("204 grant: %d %v", n, ok)
					}
				}
			}
			if old {
				if calls != 5 {
					t.Fatalf("%d calls; want three group refusals, success, then success", calls)
				}
				if _, ok := client.Granted(); ok {
					t.Fatal("old platform invented a grant")
				}
			} else if n, ok := client.Granted(); calls != 2 || !ok || n != 0 {
				t.Fatalf("body grant must override header: %d %v (%d calls)", n, ok, calls)
			}
		})
	}
}

func TestSlotHeadroomParserRejectsInvalidReadings(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"", "x 1 2", "-1 0 0"} {
		if load := agentd.ParseLoad(input); load != nil {
			t.Fatalf("accepted load %q", input)
		}
	}
	for _, input := range []string{"MemTotal: 10 kB\n", "MemTotal: 10 kB\nMemAvailable: 20 kB\n", "MemTotal: x kB\nMemAvailable: 1 kB\n"} {
		if avail, total := agentd.ParseMeminfo(input); avail != nil || total != nil {
			t.Fatalf("accepted memory %q", input)
		}
	}
}
