package config

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/outsideinput"
)

// This package's tests read files outside the Go module, which Go's test cache
// does not look at again (CI feedback 18, 2026-11-13; see outsideinput). Measured
// that day with go test's own log of what each test process opened.
// Filled in by the CI line from that measurement, not by this package's owners:
// they may change what is declared here.
func TestOutsideInputs(t *testing.T) { outsideinput.Declare(t, "config") }
