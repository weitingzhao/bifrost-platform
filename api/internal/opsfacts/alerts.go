package opsfacts

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

type targetRead struct {
	ActiveTargets *[]struct {
		Labels map[string]string `json:"labels"`
	} `json:"active_targets"`
	GeneratedAt time.Time `json:"generated_at"`
}
type Service struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Owner     string `json:"owner"`
}
type Inventory struct {
	Services []Service `json:"services"`
}

func projectInventory(in Fact[targetRead]) Fact[Inventory] {
	out := mapFact(in, func(v targetRead) Inventory {
		result := Inventory{Services: []Service{}}
		seen := map[Service]bool{}
		if v.ActiveTargets != nil {
			for _, t := range *v.ActiveTargets {
				if t.Labels["service"] == "" {
					continue
				}
				s := Service{Namespace: t.Labels["namespace"], Name: t.Labels["service"], Owner: t.Labels["owner"]}
				if s.Owner == "" {
					s.Owner = "unknown"
				}
				if !seen[s] {
					result.Services = append(result.Services, s)
					seen[s] = true
				}
			}
		}
		sort.Slice(result.Services, func(i, j int) bool {
			a, b := result.Services[i], result.Services[j]
			if a.Namespace != b.Namespace {
				return a.Namespace < b.Namespace
			}
			if a.Name != b.Name {
				return a.Name < b.Name
			}
			return a.Owner < b.Owner
		})
		return result
	})
	if in.Value != nil {
		if in.Value.ActiveTargets == nil {
			invalid(&out)
			return out
		}
		if len(out.Value.Services) == 0 {
			out.State = Empty
		}
		out.Coverage = snapshotCoverage()
		out.Coverage.Reason = "active scrape targets only; not a complete organizational ownership registry"
		out.Freshness(timestamp(in.Value.GeneratedAt), 2*time.Minute)
	}
	return out
}

type alertEntry struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	State       string            `json:"state"`
	ActiveAt    string            `json:"active_at"`
}
type alertRead struct {
	Alerts      *[]alertEntry `json:"alerts"`
	GeneratedAt time.Time     `json:"generated_at"`
}
type Responsibility struct {
	State   State  `json:"state"`
	Owner   string `json:"owner"`
	Service string `json:"service"`
	Reason  string `json:"reason"`
	Source  Source `json:"source"`
}
type WindowRelation struct {
	State    State  `json:"state"`
	Held     *bool  `json:"held"`
	Holder   string `json:"holder,omitempty"`
	Relation string `json:"relation"`
	Reason   string `json:"reason"`
}
type Alert struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	State          string         `json:"state"`
	Severity       string         `json:"severity"`
	Symptom        string         `json:"symptom"`
	ActiveAt       *time.Time     `json:"active_at"`
	ActiveSeconds  *int64         `json:"active_seconds"`
	Responsibility Responsibility `json:"responsibility"`
	ReleaseWindow  WindowRelation `json:"release_window"`
	MergedSources  []Source       `json:"merged_sources"`
}
type Alerts struct {
	Total              int                       `json:"total"`
	RawTotal           int                       `json:"raw_total"`
	Firing             int                       `json:"firing"`
	Pending            int                       `json:"pending"`
	BySeverity         map[string]int            `json:"by_severity"`
	ByStateAndSeverity map[string]map[string]int `json:"by_state_and_severity"`
	Items              []Alert                   `json:"items"`
}

func projectAlerts(in Fact[alertRead], inventory Fact[Inventory], window Fact[ReleaseWindow]) Fact[Alerts] {
	out := mapFact(in, func(v alertRead) Alerts {
		return Alerts{BySeverity: map[string]int{}, ByStateAndSeverity: map[string]map[string]int{"firing": {}, "pending": {}}, Items: []Alert{}}
	})
	if in.Value == nil {
		return out
	}
	if in.Value.Alerts == nil {
		invalid(&out)
		return out
	}
	candidates := []Candidate[alertEntry]{}
	for _, a := range *in.Value.Alerts {
		if a.Labels["alertname"] == "" || (a.State != "firing" && a.State != "pending") {
			invalid(&out)
			return out
		}
		// Prometheus label set is the alert identity, independent of active_at and
		// state. Prefer firing over pending if a transition appears twice.
		labels, _ := json.Marshal(a.Labels)
		priority := 1
		if a.State == "firing" {
			priority = 0
		}
		copy := a
		candidates = append(candidates, Candidate[alertEntry]{Key: string(labels), Priority: priority, Fact: Fact[alertEntry]{Source: in.Source, Value: &copy}})
	}
	deduped := Deduplicate(candidates)
	sort.Slice(deduped, func(i, j int) bool { return deduped[i].Key < deduped[j].Key })
	out.Value.RawTotal = len(*in.Value.Alerts)
	for _, c := range deduped {
		a := *c.Fact.Value
		severity := a.Labels["severity"]
		if severity == "" {
			severity = "unknown"
		}
		item := Alert{ID: fmt.Sprintf("%x", sha256.Sum256([]byte(c.Key))), Name: a.Labels["alertname"], State: a.State, Severity: severity, Symptom: a.Annotations["summary"], Responsibility: responsibility(a.Labels, inventory), ReleaseWindow: windowRelation(window), MergedSources: c.Fact.MergedSources}
		if item.Symptom == "" {
			item.Symptom = item.Name
		}
		if active, err := time.Parse(time.RFC3339Nano, a.ActiveAt); err == nil && !active.After(in.AsOf) {
			item.ActiveAt = timestamp(active)
			seconds := int64(in.AsOf.Sub(active).Seconds())
			item.ActiveSeconds = &seconds
		}
		out.Value.Items = append(out.Value.Items, item)
		out.Value.BySeverity[severity]++
		out.Value.ByStateAndSeverity[a.State][severity]++
		if a.State == "firing" {
			out.Value.Firing++
		} else {
			out.Value.Pending++
		}
	}
	out.Value.Total = len(out.Value.Items)
	if out.Value.Total == 0 {
		out.State = Empty
	}
	out.Coverage = snapshotCoverage()
	out.Coverage.Reason = "current alert instances; not incidents, noise classifications, or historical openings"
	out.Freshness(timestamp(in.Value.GeneratedAt), 2*time.Minute)
	return out
}
func responsibility(labels map[string]string, inventory Fact[Inventory]) Responsibility {
	r := Responsibility{State: Unknown, Owner: "unknown", Service: labels["service"], Reason: "no explicit responsibility label or unambiguous registered service owner", Source: TargetsSource}
	if owner := labels["owner"]; owner != "" {
		r.State = OK
		r.Owner = owner
		r.Reason = "explicit alert owner label; not proof of root cause or required Owner action"
		r.Source = AlertsSource
		return r
	}
	if inventory.State != OK || inventory.Value == nil || labels["namespace"] == "" || r.Service == "" {
		return r
	}
	owners := map[string]bool{}
	for _, s := range inventory.Value.Services {
		if s.Namespace == labels["namespace"] && s.Name == r.Service {
			owners[s.Owner] = true
		}
	}
	if len(owners) == 1 && !owners["unknown"] {
		for owner := range owners {
			r.Owner = owner
		}
		r.State = OK
		r.Reason = "exact namespace/service match in current scrape inventory"
	}
	return r
}
func windowRelation(window Fact[ReleaseWindow]) WindowRelation {
	r := WindowRelation{State: Unknown, Relation: "unknown", Reason: "window status alone does not establish causality or queued work"}
	if window.State == OK && window.Value != nil {
		r.State = OK
		r.Held = window.Value.Open
		if window.Value.Window != nil {
			r.Holder = window.Value.Window.Who
		}
	}
	return r
}
