package store_test

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

var deterministic = store.Tool{Executor: "exec:deterministic", Provider: "local"}
