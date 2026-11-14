package landinggit

import (
	"strings"
	"testing"
)

func TestSTGLandedOperationTrailer(t *testing.T) {
	t.Parallel()
	good := "Change\n\nAgentrt-Task: " + testTask + "\nLanded-By: agentrt-prod/land:one:1\n"
	for _, bad := range []string{"Change\nLanded-By: agentrt-prod/land:one:1\n\nbody", good + "Landed-By: agentrt-prod/land:one:1\n", good + " continuation\n", strings.ReplaceAll(good, "agentrt-prod", "agentrt-stg"), "Change\n\n> Landed-By: agentrt-prod/land:one:1"} {
		if LandedOperation(good) != "land:one:1" {
			t.Fatal("positive trailer")
		}
		if LandedOperation(bad) != "" {
			t.Fatal("accepted malformed trailer", bad)
		}
	}
}
