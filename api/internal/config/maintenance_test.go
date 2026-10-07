package config

import "testing"

func TestLoopEnabledDefaultsOn(t *testing.T) {
	for _, key := range []string{EnvDataCloneScheduler, EnvPatrolLoop} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "")
			if !LoopEnabled(key) {
				t.Fatalf("%s unset must be on", key)
			}
			for _, on := range []string{"on", "1", "true", "yes", "ON"} {
				t.Setenv(key, on)
				if !LoopEnabled(key) {
					t.Fatalf("%s=%q must be on", key, on)
				}
			}
			for _, off := range []string{"off", "OFF", "0", "false", "no", "  off  "} {
				t.Setenv(key, off)
				if LoopEnabled(key) {
					t.Fatalf("%s=%q must be off", key, off)
				}
			}
		})
	}
}
