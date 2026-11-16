package cirunview

import (
	"fmt"
	"strings"
)

// WriteFullCIMetric formats the already-observed latest run. The caller decides
// whether an observation exists; this function starts no checks or notifications.
func WriteFullCIMetric(b *strings.Builder, name, status string) {
	value := 0
	if status == "Failed" {
		value = 1
	}
	b.WriteString("# HELP bifrost_full_ci_red Whether the newest finished daily full CI run is red (1) or green (0)\n")
	b.WriteString("# TYPE bifrost_full_ci_red gauge\n")
	fmt.Fprintf(b, "bifrost_full_ci_red{run=%q} %d\n", name, value)
}
