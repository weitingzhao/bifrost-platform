package opsfacts

import (
	"context"
	"net/url"
	"strings"
	"time"
)

func source(name, path string) Source { return Source{Name: name, Method: "GET", Path: path} }
func promSource(query string) Source {
	return source("prometheus", "/api/v1/telemetry/promql?q="+url.QueryEscape(query))
}

// These paths document the existing authoritative readers. The server adapter
// calls their handlers in-process, without an HTTP loopback or a second cache.
var (
	HealthSource  = source("platform", "/health")
	AlertsSource  = source("prometheus", "/api/v1/telemetry/alerts")
	TargetsSource = source("prometheus", "/api/v1/telemetry/targets?state=active")
	WindowSource  = source("delivery", "/api/v1/delivery/release-window")
	PolicySource  = source("release-policy", "/api/v1/release-policy")
	AverageSource = promSource("avg_over_time(up[7d])")
	CountSource   = promSource("count_over_time(up[7d])")
)

type Health struct {
	Status          string `json:"status"`
	Service         string `json:"service"`
	Role            string `json:"role"`
	AuthLoaded      *bool  `json:"auth_loaded"`
	AgentSchema     string `json:"agentrt_schema"`
	AgentReady      *bool  `json:"agentrt_ready"`
	ContainedPanics *int   `json:"contained_panics"`
}
type ReleaseWindow struct {
	Open   *bool  `json:"open"`
	Window *Lease `json:"window"`
}
type Lease struct {
	Who       string    `json:"who"`
	What      string    `json:"what"`
	Env       string    `json:"env"`
	StartedAt time.Time `json:"started_at"`
	ExpiresAt time.Time `json:"expires_at"`
}
type ReleasePolicy struct {
	Valid        *bool     `json:"valid"`
	SignedAt     string    `json:"signed_at,omitempty"`
	Expired      *bool     `json:"expired"`
	Frozen       *bool     `json:"frozen"`
	CheckedAt    time.Time `json:"checked_at"`
	SigningState string    `json:"signing_state"`
}
type policyRead struct {
	ReleasePolicy
	Reasons []string `json:"reasons"`
}
type System struct {
	AsOf             time.Time           `json:"as_of"`
	Timezone         string              `json:"timezone"`
	Health           Fact[Health]        `json:"health"`
	Alerts           Fact[Alerts]        `json:"alerts"`
	ServiceInventory Fact[Inventory]     `json:"service_inventory"`
	ReleaseWindow    Fact[ReleaseWindow] `json:"release_window"`
	ReleasePolicy    Fact[ReleasePolicy] `json:"release_policy"`
	Availability     Fact[Availability]  `json:"availability_7d"`
}

// ProjectSystem performs only injected reads. A source failure remains local to
// its fact; HTTP success of the projection never asserts whole-system health.
func ProjectSystem(ctx context.Context, reader Reader, now func() time.Time) System {
	out := System{Timezone: "America/Chicago"}
	out.Health = Read[Health](ctx, reader, HealthSource, now)
	if out.Health.Value != nil {
		if out.Health.Value.Status == "" || out.Health.Value.Service == "" {
			invalid(&out.Health)
		} else {
			out.Health.Coverage = snapshotCoverage()
		}
	}
	out.ReleaseWindow = Read[ReleaseWindow](ctx, reader, WindowSource, now)
	if v := out.ReleaseWindow.Value; v != nil {
		if v.Open == nil || (*v.Open && (v.Window == nil || v.Window.Who == "" || v.Window.ExpiresAt.IsZero() || v.Window.StartedAt.IsZero())) {
			invalid(&out.ReleaseWindow)
		} else {
			out.ReleaseWindow.Coverage = snapshotCoverage()
			// A lease's start is not a source refresh timestamp. Its expiry is a
			// separate validity check and must not be passed as watermark.
			if *v.Open && !v.Window.ExpiresAt.After(out.ReleaseWindow.AsOf) {
				out.ReleaseWindow.State = Stale
				out.ReleaseWindow.Reason = "release window lease expired during read"
			}
		}
	}
	policy := Read[policyRead](ctx, reader, PolicySource, now)
	// The legacy status endpoint returns HTTP 200 even when its ConfigMap
	// read failed. Preserve that distinction without forwarding its error text.
	if policy.Value != nil {
		for _, reason := range policy.Value.Reasons {
			if strings.HasPrefix(reason, "cannot read ConfigMap cicd/bifrost-release-policy:") {
				policy.State, policy.Reason, policy.Value = Error, "policy source read failed", nil
				break
			}
		}
	}
	out.ReleasePolicy = mapFact(policy, func(v policyRead) ReleasePolicy {
		p := v.ReleasePolicy
		p.SigningState = "unknown"
		if p.Valid != nil && *p.Valid {
			p.SigningState = "signed_valid"
		} else if p.SignedAt != "" {
			p.SigningState = "signed_invalid"
		} else {
			for _, reason := range v.Reasons {
				if reason == "no signed policy (ConfigMap cicd/bifrost-release-policy is missing or empty)" {
					p.SigningState = "unsigned"
				}
			}
		}
		return p
	})
	if p := out.ReleasePolicy.Value; p != nil {
		if p.Valid == nil {
			invalid(&out.ReleasePolicy)
		} else {
			out.ReleasePolicy.Coverage = snapshotCoverage()
			out.ReleasePolicy.Freshness(timestamp(p.CheckedAt), time.Minute)
		}
	}
	inventory := Read[targetRead](ctx, reader, TargetsSource, now)
	out.ServiceInventory = projectInventory(inventory)
	alerts := Read[alertRead](ctx, reader, AlertsSource, now)
	out.Alerts = projectAlerts(alerts, out.ServiceInventory, out.ReleaseWindow)
	averages := Read[metricRead](ctx, reader, AverageSource, now)
	counts := Read[metricRead](ctx, reader, CountSource, now)
	out.Availability = projectAvailability(averages, counts)
	out.AsOf = now().UTC()
	return out
}

func mapFact[A, B any](in Fact[A], transform func(A) B) Fact[B] {
	out := Fact[B]{AsOf: in.AsOf, Source: in.Source, Watermark: in.Watermark, Coverage: in.Coverage, State: in.State, Reason: in.Reason, MergedSources: in.MergedSources}
	if in.Value != nil {
		value := transform(*in.Value)
		out.Value = &value
	}
	return out
}
