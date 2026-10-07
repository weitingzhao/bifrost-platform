package patrol

import (
	"context"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

func TestStartSkipsWhenPatrolLoopOff(t *testing.T) {
	t.Setenv(config.EnvPatrolLoop, "off")
	h := &Handler{}
	h.Start(context.Background())
	if h.Running() {
		t.Fatal("PLATFORM_PATROL_LOOP=off still started the patrol loop")
	}
}
