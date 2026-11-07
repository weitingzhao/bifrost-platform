package agentd

import (
	"fmt"
	"strconv"
	"time"
)

// agentd's timeouts and retries (R5 phase 2 step 3b, T4): each is an
// AGENTD_* setting of the environment, with the default the code had. A
// setting that is unset or empty keeps its default; one that does not read,
// or lies outside its bounds, is an error and agentd does not start. Nothing
// tunes them on its own.

// Setting is one of agentd's settings: its variable, default and bounds.
// A duration setting reads as Go writes durations (90s, 10m); a count as a
// whole number.
type Setting struct {
	Env      string
	Default  time.Duration // a count's default is Default as a plain number
	Min, Max time.Duration
	Count    bool
}

// The settings, by variable.
const (
	EnvSetupLimit      = "AGENTD_SETUP_LIMIT"       // a task's setup, all its commands
	EnvTestDBLimit     = "AGENTD_TESTDB_LIMIT"      // the test database's start
	EnvDeliverMaxFails = "AGENTD_DELIVER_MAX_FAILS" // heartbeats failing in a row before the lease counts as lost
	EnvPruneEvery      = "AGENTD_PRUNE_EVERY"       // how often agentd asks which branches to prune
	EnvPruneWork       = "AGENTD_PRUNE_WORK"        // a prune round's asking and git
	EnvPruneReport     = "AGENTD_PRUNE_REPORT"      // sending what a prune round did
	EnvCollectEvery    = "AGENTD_COLLECT_EVERY"     // how often an executor's log is read for its actions
	EnvSpecLimit       = "AGENTD_SPEC_LIMIT"        // a spec's verify commands when the spec states no timeout_s
	EnvBenchLimit      = "AGENTD_BENCH_LIMIT"       // the executor's time on one benchmark question (--limit's default)
	EnvBenchCheckLimit = "AGENTD_BENCH_CHECK_LIMIT" // each benchmark setup or check command
)

// Settings lists every setting, in the order the documentation has them.
var Settings = []Setting{
	{Env: EnvSetupLimit, Default: 10 * time.Minute, Min: time.Minute, Max: 2 * time.Hour},
	{Env: EnvTestDBLimit, Default: 2 * time.Minute, Min: 10 * time.Second, Max: 30 * time.Minute},
	{Env: EnvDeliverMaxFails, Default: 3, Min: 1, Max: 20, Count: true},
	{Env: EnvPruneEvery, Default: 10 * time.Minute, Min: time.Minute, Max: 24 * time.Hour},
	{Env: EnvPruneWork, Default: 2 * time.Minute, Min: 10 * time.Second, Max: 30 * time.Minute},
	{Env: EnvPruneReport, Default: 30 * time.Second, Min: 5 * time.Second, Max: 10 * time.Minute},
	{Env: EnvCollectEvery, Default: 2 * time.Second, Min: 500 * time.Millisecond, Max: time.Minute},
	{Env: EnvSpecLimit, Default: 15 * time.Minute, Min: time.Minute, Max: 24 * time.Hour},
	{Env: EnvBenchLimit, Default: 30 * time.Minute, Min: time.Minute, Max: 24 * time.Hour},
	{Env: EnvBenchCheckLimit, Default: 15 * time.Minute, Min: time.Minute, Max: 24 * time.Hour},
}

// Config is the settings as read: a duration or count by variable.
type Config map[string]time.Duration

// Duration is a duration setting's value (its default when c lacks it).
func (c Config) Duration(env string) time.Duration {
	if v, ok := c[env]; ok {
		return v
	}
	for _, s := range Settings {
		if s.Env == env {
			return s.Default
		}
	}
	panic("agentd: no setting " + env)
}

// Count is a count setting's value.
func (c Config) Count(env string) int { return int(c.Duration(env)) }

// DefaultConfig is every setting at its default.
func DefaultConfig() Config {
	c := Config{}
	for _, s := range Settings {
		c[s.Env] = s.Default
	}
	return c
}

// ConfigFromEnv reads the settings with getenv (os.Getenv in agentd).
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	c := DefaultConfig()
	for _, s := range Settings {
		raw := getenv(s.Env)
		if raw == "" {
			continue
		}
		var v time.Duration
		if s.Count {
			n, err := strconv.Atoi(raw)
			if err != nil {
				return nil, fmt.Errorf("%s: %q is not a whole number", s.Env, raw)
			}
			v = time.Duration(n)
		} else {
			d, err := time.ParseDuration(raw)
			if err != nil {
				return nil, fmt.Errorf("%s: %q is not a duration (e.g. 90s, 10m)", s.Env, raw)
			}
			v = d
		}
		if v < s.Min || v > s.Max {
			if s.Count {
				return nil, fmt.Errorf("%s: %d is outside %d..%d", s.Env, int(v), int(s.Min), int(s.Max))
			}
			return nil, fmt.Errorf("%s: %s is outside %s..%s", s.Env, v, s.Min, s.Max)
		}
		c[s.Env] = v
	}
	return c, nil
}

// Apply makes c the settings of this process: call it once at start,
// before any attempt runs. The daemon's own fields (MaxFails) are set from
// c where the daemon is built.
func (c Config) Apply() {
	SetupLimit = c.Duration(EnvSetupLimit)
	testDBLimit = c.Duration(EnvTestDBLimit)
	PruneEvery = c.Duration(EnvPruneEvery)
	PruneWork = c.Duration(EnvPruneWork)
	PruneReport = c.Duration(EnvPruneReport)
	collectEvery = c.Duration(EnvCollectEvery)
	SpecLimit = c.Duration(EnvSpecLimit)
}
