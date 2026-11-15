package opsfacts

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeReader map[Source]ReadResult

func (r fakeReader) Read(_ context.Context, s Source) ReadResult { return r[s] }
func result(body string) ReadResult                              { return ReadResult{Status: 200, Body: json.RawMessage(body)} }

var testNow = time.Date(2026, 10, 28, 4, 30, 0, 0, time.UTC)

func fixedNow() time.Time { return testNow }

func TestReadStatesAndRedaction(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		result ReadResult
		state  State
	}{
		{"unmeasured", ReadResult{}, Unknown},
		{"failure", ReadResult{Err: errors.New("private credential")}, Error},
		{"denied", ReadResult{Status: 403, Body: json.RawMessage(`{"error":"private credential"}`)}, Forbidden},
		{"unauthenticated", ReadResult{Status: 401}, Forbidden},
		{"bad gateway", ReadResult{Status: 502}, Error},
		{"empty 200", result(""), Error},
		{"null", result("null"), Error},
		{"malformed", result("{"), Error},
		{"success", result(`{"n":0}`), OK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := Read[map[string]int](context.Background(), fakeReader{HealthSource: tc.result}, HealthSource, fixedNow)
			if f.State != tc.state {
				t.Fatalf("%+v", f)
			}
			b, _ := json.Marshal(f)
			if strings.Contains(string(b), "private") {
				t.Fatal("leaked source error")
			}
			if f.State != OK && f.Value != nil {
				t.Fatal("failure fabricated value")
			}
		})
	}
}
func TestFreshnessAndPriority(t *testing.T) {
	t.Parallel()
	old := testNow.Add(-3 * time.Minute)
	f := Fact[int]{State: OK, AsOf: testNow}
	f.Freshness(&old, time.Minute)
	if f.State != Stale {
		t.Fatal(f)
	}
	f = Fact[int]{State: Empty, AsOf: testNow}
	f.Freshness(nil, time.Minute)
	if f.State != Empty || f.Watermark != nil {
		t.Fatal(f)
	}
	future := testNow.Add(2 * time.Minute)
	f.Freshness(&future, time.Minute)
	if f.State != Unknown {
		t.Fatal(f)
	}
	authoritative := Fact[int]{State: Error, Source: AlertsSource}
	fallback := Fact[int]{State: OK, Source: AverageSource}
	out := Deduplicate([]Candidate[int]{{Key: "same", Priority: 2, Fact: fallback}, {Key: "same", Priority: 1, Fact: authoritative}})
	if len(out) != 1 || out[0].Fact.State != Error || len(out[0].Fact.MergedSources) != 1 || out[0].Fact.MergedSources[0] != AverageSource {
		t.Fatal(out)
	}
}
