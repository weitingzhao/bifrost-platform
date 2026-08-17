package delivery

import (
	"fmt"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func TestStgAPIProbePath(t *testing.T) {
	cases := []struct {
		domain string
		want   string
	}{
		{domain: "monitor", want: "/status"},
		{domain: "docs", want: "/research/docs/health"},
		{domain: "ops", want: "/ops/health"},
		{domain: "trading", want: "/health"},
		{domain: "", want: "/health"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q→%q", tc.domain, tc.want), func(t *testing.T) {
			if got := stgAPIProbePath(tc.domain); got != tc.want {
				t.Fatalf("stgAPIProbePath(%q) = %q, want %q", tc.domain, got, tc.want)
			}
		})
	}
}

func TestSmokeHTTPStatus(t *testing.T) {
	cases := []struct {
		code  int
		reach probe.Reachability
	}{
		{code: 200, reach: probe.ReachOK},
		{code: 503, reach: probe.ReachDegraded},
		{code: 404, reach: probe.ReachFail},
		{code: 500, reach: probe.ReachFail},
		{code: 301, reach: probe.ReachUnknown},
		{code: 0, reach: probe.ReachUnknown},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("status-%d", tc.code), func(t *testing.T) {
			reach, detail := smokeHTTPStatus(tc.code)
			if reach != tc.reach {
				t.Fatalf("reach = %q, want %q", reach, tc.reach)
			}
			if detail == "" {
				t.Fatal("expected non-empty detail")
			}
		})
	}
}

func TestAggregateStgSmoke(t *testing.T) {
	cases := []struct {
		name  string
		in    []StgSmokeTargetView
		reach probe.Reachability
		want  string
	}{
		{
			name: "all-api-ok",
			in: []StgSmokeTargetView{
				{ID: "stg-frontend", Reachability: probe.ReachOK},
				{ID: "stg-api-monitor", Reachability: probe.ReachOK},
				{ID: "stg-api-ops", Reachability: probe.ReachDegraded},
			},
			reach: probe.ReachOK,
			want:  "stg 2/2 API domains reachable",
		},
		{
			name: "partial-api",
			in: []StgSmokeTargetView{
				{ID: "stg-api-monitor", Reachability: probe.ReachOK},
				{ID: "stg-api-ops", Reachability: probe.ReachFail},
			},
			reach: probe.ReachDegraded,
			want:  "stg 1/2 API domains reachable",
		},
		{
			name: "first-target-fail",
			in: []StgSmokeTargetView{
				{ID: "stg-frontend", Reachability: probe.ReachFail},
			},
			reach: probe.ReachFail,
			want:  "stg smoke unreachable",
		},
		{
			name:  "empty-partial",
			in:    nil,
			reach: probe.ReachDegraded,
			want:  "stg smoke partial",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reach, detail := aggregateStgSmoke(tc.in)
			if reach != tc.reach {
				t.Fatalf("reach = %q, want %q", reach, tc.reach)
			}
			if detail != tc.want {
				t.Fatalf("detail = %q, want %q", detail, tc.want)
			}
		})
	}
}

func TestAggregateFailCountSmoke(t *testing.T) {
	cases := []struct {
		name  string
		in    []StgSmokeTargetView
		reach probe.Reachability
	}{
		{
			name: "all-ok",
			in: []StgSmokeTargetView{
				{ID: "prod-frontend", Reachability: probe.ReachOK},
				{ID: "prod-api-monitor", Reachability: probe.ReachDegraded},
			},
			reach: probe.ReachOK,
		},
		{
			name: "one-fail",
			in: []StgSmokeTargetView{
				{ID: "prod-api-monitor", Reachability: probe.ReachFail},
			},
			reach: probe.ReachFail,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reach, detail := aggregateFailCountSmoke(tc.in, "prod")
			if reach != tc.reach {
				t.Fatalf("reach = %q, want %q (detail=%q)", reach, tc.reach, detail)
			}
			if detail == "" {
				t.Fatal("expected non-empty detail")
			}
		})
	}
}
