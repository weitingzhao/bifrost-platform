package actions

import (
	"context"
	"sync"
)

// Executor runs an action with the params stored on the approval.
type Executor func(ctx context.Context, params map[string]any) (any, error)

var (
	execMu    sync.RWMutex
	executors = map[string]Executor{}
)

// RegisterExecutor installs the function approve calls for id.
func RegisterExecutor(id string, fn Executor) {
	execMu.Lock()
	executors[id] = fn
	execMu.Unlock()
}

// Execute runs the registered executor. A missing executor is an error.
func Execute(ctx context.Context, id string, params map[string]any) (any, error) {
	execMu.RLock()
	fn := executors[id]
	execMu.RUnlock()
	if fn == nil {
		return nil, errNoExecutor(id)
	}
	if params == nil {
		params = map[string]any{}
	}
	return fn(ctx, cloneMap(params))
}

type noExecutor string

func errNoExecutor(id string) error { return noExecutor(id) }

func (e noExecutor) Error() string { return "no executor for " + string(e) }
