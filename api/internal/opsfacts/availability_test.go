package opsfacts

import (
	"context"
	"testing"
)

func TestAvailabilityNeverTreatsMissingOrInvalidSamplesAsZero(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"points":[{"labels":{"job":"api"}}]}`,
		`{"points":[{"labels":{"job":"api"},"value":null}]}`,
		`{"points":[{"labels":{"job":"api"},"value":-1}]}`,
		`{"points":[{"labels":{"job":"api"},"value":2}]}`,
		`{"points":[{"labels":{"job":"api"},"value":1},{"labels":{"job":"api"},"value":1}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			r := systemReads()
			r[AverageSource] = result(body)
			got := ProjectSystem(context.Background(), r, fixedNow)
			if got.Availability.State != Error || got.Availability.Value != nil {
				t.Fatal(got.Availability)
			}
		})
	}
	for _, body := range []string{`{"points":[]}`, `{"points":[{"labels":{"job":"different"},"value":3}]}`} {
		t.Run("unmatched"+body, func(t *testing.T) {
			t.Parallel()
			r := systemReads()
			r[CountSource] = result(body)
			got := ProjectSystem(context.Background(), r, fixedNow)
			if got.Availability.Value.Series[0].Samples != nil || got.Availability.State != Unknown {
				t.Fatal(got.Availability)
			}
		})
	}
	for _, body := range []string{`{"points":[{"labels":{"job":"api"}}]}`, `{"points":[{"labels":{"job":"api"},"value":0.5}]}`} {
		t.Run("bad count"+body, func(t *testing.T) {
			t.Parallel()
			r := systemReads()
			r[CountSource] = result(body)
			got := ProjectSystem(context.Background(), r, fixedNow)
			if got.Availability.Value.SampleCounts.State != Error {
				t.Fatal(got.Availability)
			}
		})
	}
}
func TestInvalidPolicyDoesNotAlwaysMeanUnsigned(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ body, want string }{
		{`{"valid":false,"reasons":["private unknown source error"]}`, "unknown"},
		{`{"valid":false,"signed_at":"2026-10-01T00:00:00Z","expired":true}`, "signed_invalid"},
		{`{"valid":true,"signed_at":"2026-10-01T00:00:00Z"}`, "signed_valid"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			r := systemReads()
			r[PolicySource] = result(tc.body)
			p := ProjectSystem(context.Background(), r, fixedNow).ReleasePolicy
			if p.State != OK || p.Value.SigningState != tc.want {
				t.Fatal(p)
			}
		})
	}
}

func TestPolicyReadErrorInsideSuccessResponseIsNotUnsigned(t *testing.T) {
	t.Parallel()
	r := systemReads()
	r[PolicySource] = result(`{"valid":false,"reasons":["cannot read ConfigMap cicd/bifrost-release-policy: private upstream detail"]}`)
	p := ProjectSystem(context.Background(), r, fixedNow).ReleasePolicy
	if p.State != Error || p.Value != nil || p.Reason != "policy source read failed" {
		t.Fatal(p)
	}
}
func TestStaleCountResponseDoesNotSupplyCurrentCoverage(t *testing.T) {
	t.Parallel()
	r := systemReads()
	r[CountSource] = result(`{"generated_at":"2026-10-27T04:30:00Z","points":[{"labels":{"namespace":"ops","service":"platform-api"},"value":100}]}`)
	a := ProjectSystem(context.Background(), r, fixedNow).Availability
	if a.Value.SampleCounts.State != Stale || a.Value.SampleCounts.Watermark != nil || a.Value.Series[0].Samples != nil {
		t.Fatal(a)
	}
}
