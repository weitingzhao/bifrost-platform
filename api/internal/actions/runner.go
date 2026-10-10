package actions

import (
	"errors"
	"strings"
)

// Runner values: who executes an approved action.
const (
	// RunnerPlatform: platform-api with its own restricted identity, at approve
	// time and on retry. The default.
	RunnerPlatform = "platform"
	// RunnerHost: the out-of-band executor (role executor) claims it.
	RunnerHost = "host"
	// RunnerOwner: the Owner runs it by hand (owner-run.sh, rolling-reboot.sh),
	// which claims with an admin token and posts the result.
	RunnerOwner = "owner"
)

// RunnerOf is the runner for these normalized params. owner_run_command takes
// it from its runner param ("owner" when absent).
func (a Action) RunnerOf(params map[string]any) string {
	if a.ID == "owner_run_command" {
		if str(params["runner"]) == RunnerHost {
			return RunnerHost
		}
		return RunnerOwner
	}
	if a.Runner == "" {
		return RunnerPlatform
	}
	return a.Runner
}

// NormalizeRunner rewrites the runner param of owner_run_command to its
// canonical value before params_hash. "system" is the name the notification
// uses for the host executor.
func NormalizeRunner(id string, params map[string]any) error {
	if id != "owner_run_command" {
		return nil
	}
	raw, ok := params["runner"]
	if !ok {
		return nil
	}
	switch strings.ToLower(str(raw)) {
	case "", RunnerOwner:
		delete(params, "runner")
	case RunnerHost, "system":
		params["runner"] = RunnerHost
	default:
		return errors.New("runner must be owner or host")
	}
	return nil
}

// TransientError is a refusal that happened before anything started and may
// clear by itself (release window held by another release, a dependency
// briefly unavailable). The approval stays approved and is retried.
type TransientError struct{ Reason string }

func (e *TransientError) Error() string { return e.Reason }

// Transient marks reason as retryable.
func Transient(reason string) error { return &TransientError{Reason: reason} }

// IsTransient reports whether err (or anything it wraps) is a TransientError.
func IsTransient(err error) bool {
	var t *TransientError
	return errors.As(err, &t)
}
