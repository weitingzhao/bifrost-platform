package opsfacts

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func systemReads() fakeReader {
	return fakeReader{
		HealthSource:  result(`{"status":"ok","service":"platform-api","role":"api","auth_loaded":true,"agentrt_schema":"current","agentrt_ready":true,"contained_panics":0}`),
		PolicySource:  result(`{"valid":false,"reasons":["no signed policy (ConfigMap cicd/bifrost-release-policy is missing or empty)"],"checked_at":"2026-10-28T04:30:00Z"}`),
		WindowSource:  result(`{"open":true,"window":{"who":"release-session","what":"platform","env":"stg","started_at":"2026-10-28T04:29:00Z","expires_at":"2026-10-28T04:34:00Z"}}`),
		TargetsSource: result(`{"generated_at":"2026-10-28T04:30:00Z","active_targets":[{"labels":{"namespace":"ops","service":"platform-api","owner":"platform-team"}}]}`),
		AlertsSource:  result(`{"generated_at":"2026-10-28T04:30:00Z","alerts":[{"labels":{"alertname":"APIUnavailable","severity":"critical","namespace":"ops","service":"platform-api"},"annotations":{"summary":"API probe failed"},"state":"firing","active_at":"2026-10-28T04:29:00Z"},{"labels":{"alertname":"DiskQueue","severity":"warning"},"state":"pending"}]}`),
		AverageSource: result(`{"generated_at":"2026-10-28T04:30:00Z","points":[{"labels":{"namespace":"ops","service":"platform-api"},"value":1,"timestamp":1793161800}]}`),
		CountSource:   result(`{"generated_at":"2026-10-28T04:30:00Z","points":[{"labels":{"namespace":"ops","service":"platform-api"},"value":2,"timestamp":1793161800}]}`),
	}
}
func TestSystemContract(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"system", "system-degraded"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reads := systemReads()
			if name == "system-degraded" {
				reads[AlertsSource] = result("")
				reads[TargetsSource] = ReadResult{Status: 403}
				reads[WindowSource] = ReadResult{Status: 502}
				reads[AverageSource] = result(`{"points":[]}`)
				reads[CountSource] = ReadResult{}
			}
			got := ProjectSystem(context.Background(), reads, fixedNow)
			path := filepath.Join("testdata", name+".json")
			if os.Getenv("UPDATE_OPSFACTS_FIXTURES") == "1" {
				b, err := json.MarshalIndent(got, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(b, '\n'), 0644); err != nil {
					t.Fatal(err)
				}
			}
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var want System
			if err := json.Unmarshal(b, &want); err != nil {
				t.Fatal(err)
			}
			// JSON comparison avoids time.Time location identity differences.
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(want)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("contract differs from %s", path)
			}
		})
	}
}
func TestSystemDoesNotInventCertainty(t *testing.T) {
	t.Parallel()
	got := ProjectSystem(context.Background(), systemReads(), fixedNow)
	if got.ReleasePolicy.State != OK || *got.ReleasePolicy.Value.Valid || got.ReleasePolicy.Value.SigningState != "unsigned" {
		t.Fatal(got.ReleasePolicy)
	}
	if got.Alerts.Value.Firing != 1 || got.Alerts.Value.Pending != 1 || got.Alerts.Value.ByStateAndSeverity["firing"]["critical"] != 1 {
		t.Fatal(got.Alerts)
	}
	if got.Availability.State != Unknown || got.Availability.Value.Overall != nil || got.Availability.Value.Series[0].Availability != nil || *got.Availability.Value.Series[0].Samples != 2 || got.Availability.Watermark != nil {
		t.Fatal(got.Availability)
	}
	for _, a := range got.Alerts.Value.Items {
		if a.ReleaseWindow.Relation != "unknown" || a.ReleaseWindow.Holder != "release-session" {
			t.Fatal(a)
		}
		if a.Name == "APIUnavailable" && (a.Responsibility.Owner != "platform-team" || *a.ActiveSeconds != 60) {
			t.Fatal(a)
		}
		if a.Name == "DiskQueue" && a.Responsibility.State != Unknown {
			t.Fatal(a)
		}
	}
}
func TestEmptyMalformedStaleAndDuplicateAlerts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body string
		state      State
		total      int
	}{
		{"empty", `{"alerts":[],"generated_at":"2026-10-28T04:30:00Z"}`, Empty, 0},
		{"missing", `{}`, Error, 0},
		{"null", `{"alerts":null}`, Error, 0},
		{"bad state", `{"alerts":[{"labels":{"alertname":"x"},"state":"resolved"}]}`, Error, 0},
		{"stale", `{"alerts":[],"generated_at":"2026-10-28T04:20:00Z"}`, Stale, 0},
		{"duplicate", `{"alerts":[{"labels":{"alertname":"x"},"state":"pending"},{"labels":{"alertname":"x"},"state":"firing"}]}`, OK, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := systemReads()
			r[AlertsSource] = result(tc.body)
			f := ProjectSystem(context.Background(), r, fixedNow).Alerts
			if f.State != tc.state {
				t.Fatalf("%+v", f)
			}
			if f.Value != nil && f.Value.Total != tc.total {
				t.Fatal(f)
			}
			if tc.name == "duplicate" && (f.Value.Firing != 1 || len(f.Value.Items[0].MergedSources) != 1) {
				t.Fatal(f)
			}
		})
	}
}
func TestOwnershipPrecedenceAndAmbiguity(t *testing.T) {
	t.Parallel()
	inventory := Fact[Inventory]{State: OK, Value: &Inventory{Services: []Service{{Namespace: "ops", Name: "api", Owner: "a"}, {Namespace: "ops", Name: "api", Owner: "b"}}}}
	labels := map[string]string{"namespace": "ops", "service": "api"}
	if responsibility(labels, inventory).State != Unknown {
		t.Fatal("ambiguous owner accepted")
	}
	labels["owner"] = "explicit"
	if responsibility(labels, inventory).Owner != "explicit" {
		t.Fatal("explicit label did not win")
	}
	delete(labels, "owner")
	inventory.State = Stale
	if responsibility(labels, inventory).State != Unknown {
		t.Fatal("stale inventory used")
	}
}
func TestInvalidSourcesAndExpiredLease(t *testing.T) {
	t.Parallel()
	for _, src := range []Source{HealthSource, PolicySource, WindowSource, TargetsSource, AverageSource, CountSource} {
		t.Run(src.Path, func(t *testing.T) {
			t.Parallel()
			r := systemReads()
			r[src] = result(`{}`)
			got := ProjectSystem(context.Background(), r, fixedNow)
			countState := Unknown
			if got.Availability.Value != nil {
				countState = got.Availability.Value.SampleCounts.State
			}
			states := map[Source]State{HealthSource: got.Health.State, PolicySource: got.ReleasePolicy.State, WindowSource: got.ReleaseWindow.State, TargetsSource: got.ServiceInventory.State, AverageSource: got.Availability.State, CountSource: countState}
			if states[src] != Error {
				t.Fatal(src, states)
			}
		})
	}
	r := systemReads()
	r[WindowSource] = result(`{"open":true,"window":{"who":"session","started_at":"2026-10-28T04:20:00Z","expires_at":"2026-10-28T04:25:00Z"}}`)
	got := ProjectSystem(context.Background(), r, fixedNow)
	if got.ReleaseWindow.State != Stale || got.ReleaseWindow.Watermark != nil {
		t.Fatal(got.ReleaseWindow)
	}
	r[WindowSource] = result(`{"open":false}`)
	got = ProjectSystem(context.Background(), r, fixedNow)
	if got.ReleaseWindow.State != OK || *got.ReleaseWindow.Value.Open {
		t.Fatal(got.ReleaseWindow)
	}
}
func TestReadClockIsPerSource(t *testing.T) {
	t.Parallel()
	ticks := 0
	now := func() time.Time { ticks++; return testNow.Add(time.Duration(ticks) * time.Second) }
	got := ProjectSystem(context.Background(), systemReads(), now)
	if !got.AsOf.After(got.Availability.AsOf) || reflect.DeepEqual(got.Health.AsOf, got.Alerts.AsOf) {
		t.Fatal("read timestamps collapsed")
	}
}
