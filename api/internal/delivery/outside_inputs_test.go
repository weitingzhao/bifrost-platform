package delivery

import (
	"os"
	"testing"
)

// This package's tests read files outside the Go module, which Go's test cache
// does not look at again (CI feedback 18, 2026-11-13; see outsideinput). Measured
// that day with go test's own log of what each test process opened.
// Filled in by the CI line from that measurement, not by this package's owners:
// they may change what is declared here.
//
// The variable's name is the contract with CI: scripts/ci/outside_inputs.sh
// exports it and outsideinput's Env gives it the same name. Reading it here
// rather than through outsideinput keeps this test inside its own domain
// (2026-11-21, tests reaching across domains go to zero).
func TestOutsideInputs(t *testing.T) {
	const key = "BIFROST_INPUT_BIFROST_TRADE_INFRA__K8S_CICD"
	t.Logf("outside input bifrost-trade-infra:k8s/cicd: %s=%q", key, os.Getenv(key))
}
