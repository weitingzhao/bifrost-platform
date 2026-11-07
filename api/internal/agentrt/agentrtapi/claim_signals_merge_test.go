package agentrtapi

import "testing"

func TestPartialHeadroomDoesNotInventSlotLoad(t *testing.T) {
	t.Parallel()
	load, cpus := 1.25, 8
	for _, req := range []claimRequest{{CPUs: &cpus}, {Load1: &load}, {}} {
		if sig, why := req.signals(); sig != nil || why != "" {
			t.Fatalf("partial measurement: %+v %s", sig, why)
		}
	}
	if sig, why := (claimRequest{Load1: &load, CPUs: &cpus}).signals(); why != "" || sig == nil || sig.Load1 != load || sig.CPUs != cpus {
		t.Fatalf("complete measurement: %+v %s", sig, why)
	}
}
