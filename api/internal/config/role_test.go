package config

import "testing"

func TestCurrentRoleDefaultsToAll(t *testing.T) {
	t.Setenv(RoleEnv, "")
	if got := CurrentRole(); got != RoleAll {
		t.Fatalf("unset role must be %q, got %q", RoleAll, got)
	}
	// An unreadable value must not silently disable the loops: a duplicate run
	// is recoverable, a scheduler nobody starts is a silent outage.
	t.Setenv(RoleEnv, "worker")
	if got := CurrentRole(); got != RoleAll {
		t.Fatalf("unknown role must fall back to %q, got %q", RoleAll, got)
	}
}

func TestRoleSelectsWhoRunsTheLoops(t *testing.T) {
	cases := []struct {
		env          string
		want         Role
		workers, api bool
	}{
		{"api", RoleAPI, false, true},
		{"workers", RoleWorkers, true, false},
		{"WORKERS", RoleWorkers, true, false},
		{" all ", RoleAll, true, true},
	}
	for _, c := range cases {
		t.Setenv(RoleEnv, c.env)
		got := CurrentRole()
		if got != c.want {
			t.Fatalf("role %q = %q, want %q", c.env, got, c.want)
		}
		if got.RunsWorkers() != c.workers {
			t.Fatalf("role %q RunsWorkers = %v, want %v", c.env, got.RunsWorkers(), c.workers)
		}
		if got.RunsAPI() != c.api {
			t.Fatalf("role %q RunsAPI = %v, want %v", c.env, got.RunsAPI(), c.api)
		}
	}
}
