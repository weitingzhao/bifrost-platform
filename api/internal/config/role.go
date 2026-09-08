package config

import (
	"log/slog"
	"os"
	"strings"
)

// Role decides whether this process serves requests, runs the always-on
// background loops, or both.
//
// platform-api used to do both unconditionally, so every replica started its
// own patrol autopilot, IB auto-repair loop and hourly data-clone scheduler.
// In prod that is two of each, and each replica keeps its own last-run state in
// an emptyDir, so neither can see that the other already ran. Splitting the
// deployment into a request tier and a singleton worker tier makes the loops
// run exactly once and keeps a wedged worker away from the request path.
type Role string

const (
	// RoleAll serves requests and runs the loops — a single process doing
	// everything. The default, so a local `make start` and any deployment that
	// predates the split keep behaving exactly as before.
	RoleAll Role = "all"
	// RoleAPI serves requests only.
	RoleAPI Role = "api"
	// RoleWorkers runs the loops. It still serves HTTP: probes need an
	// endpoint, and the patrol autopilot reads the platform's own API.
	RoleWorkers Role = "workers"
)

// RoleEnv is the variable that selects the role.
const RoleEnv = "PLATFORM_ROLE"

// CurrentRole reads PLATFORM_ROLE. An unset or unrecognised value is RoleAll,
// which is the safe direction: doing the work twice is a duplicate, not doing
// it at all is an outage nobody is watching for.
func CurrentRole() Role {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(RoleEnv))) {
	case string(RoleAPI):
		return RoleAPI
	case string(RoleWorkers):
		return RoleWorkers
	case "", string(RoleAll):
		return RoleAll
	default:
		slog.Warn("unknown platform role, running as all",
			"env", RoleEnv, "value", os.Getenv(RoleEnv))
		return RoleAll
	}
}

// RunsWorkers reports whether this process owns the always-on loops.
func (r Role) RunsWorkers() bool { return r == RoleAll || r == RoleWorkers }

// RunsAPI reports whether this process is meant to serve the public API.
// Both roles listen on HTTP; only this one belongs behind the Service.
func (r Role) RunsAPI() bool { return r == RoleAll || r == RoleAPI }

func (r Role) String() string { return string(r) }
