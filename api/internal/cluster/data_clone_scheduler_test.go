package cluster

import (
	"context"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

func TestStartDataCloneSchedulerOffLeavesStoresUnset(t *testing.T) {
	t.Setenv(config.EnvDataCloneScheduler, "off")
	s := &Service{}
	s.StartDataCloneScheduler(context.Background())
	if s.cloneSched != nil || s.cloneJobs != nil || s.cloneLast != nil {
		t.Fatal("PLATFORM_DATA_CLONE_SCHEDULER=off still started the data-clone scheduler")
	}
}
