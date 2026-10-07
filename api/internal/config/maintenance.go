package config

import (
	"os"
	"strings"
)

const (
	// EnvDataCloneScheduler gates the hourly data-clone loop. Unset means on.
	// The STG overlay sets it to off (STG observes; the default source is bifrost_prod).
	EnvDataCloneScheduler = "PLATFORM_DATA_CLONE_SCHEDULER"
	// EnvPatrolLoop gates the patrol autopilot loop. Unset means on.
	// The STG overlay sets it to off (STG has no skills catalog).
	EnvPatrolLoop = "PLATFORM_PATROL_LOOP"
)

// LoopEnabled reports whether a background maintenance loop should start.
// "0", "false", "off" and "no" turn it off. Anything else, including unset, is on,
// so PROD and a local `make start` keep the loops without an explicit value.
func LoopEnabled(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}
