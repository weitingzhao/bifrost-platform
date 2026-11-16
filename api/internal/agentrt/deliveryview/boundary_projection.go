package deliveryview

import (
	"encoding/json"
	"time"
)

// BoundaryVersion is an already selected recorded rule; no signature, expiry or authority is established here.
type BoundaryVersion struct {
	Document, DraftDocument map[string]any
	Version                 int
	Effective, Expires      time.Time
}

// BoundaryRule keeps the effective version separate from the latest version:
// a scheduled or expired rule must never be presented as current authority.
type BoundaryRule struct {
	ID      string         `json:"rule_id"`
	Current map[string]any `json:"in_force"`
	Latest  map[string]any `json:"latest"`
	Draft   map[string]any `json:"draft"`
	Status  string         `json:"status"`
	Notes   []string       `json:"notes"`
}

type BoundaryValue struct {
	Key      string `json:"key"`
	Value    int    `json:"value"`
	Default  int    `json:"default"`
	Min      int    `json:"min"`
	Max      int    `json:"max"`
	Unit     string `json:"unit"`
	Source   string `json:"source"`
	Consumer string `json:"consumer"`
}

type DeliveryBoundaries struct {
	AsOf       time.Time        `json:"as_of"`
	Rules      []BoundaryRule   `json:"rules"`
	Tuning     []BoundaryValue  `json:"tuning"`
	Landing    []map[string]any `json:"landing"`
	Hosts      json.RawMessage  `json:"hosts"`
	Pools      []map[string]any `json:"pools"`
	DiskFloor  int64            `json:"disk_floor_bytes"`
	Unknown    []string         `json:"unknown"`
	CodeLimits []map[string]any `json:"code_limits"`
}

func ProjectBoundaryRule(id string, current, latest *BoundaryVersion, now time.Time, tuningKind, loosen, initial string, defaults map[string]int) BoundaryRule {
	v := BoundaryRule{ID: id, Status: "not_in_force", Notes: []string{}}
	if current != nil {
		v.Current = current.Document
		v.Status = "in_force"
	}
	if latest != nil {
		v.Latest = latest.Document
		v.Draft = latest.DraftDocument
		v.Draft["rule_version"] = latest.Version + 1
		v.Draft["direction"] = loosen
		if latest.Effective.After(now) {
			v.Notes = append(v.Notes, "The latest version is scheduled; the effective version is shown separately.")
		}
		if !latest.Expires.After(now) {
			v.Notes = append(v.Notes, "The latest version has expired. Review its expiry before proposing a replacement.")
		}
	}
	if id == tuningKind && latest == nil {
		v.Status = "code_defaults"
		values := map[string]int{}
		for name, value := range defaults {
			values[name] = value
		}
		v.Draft = map[string]any{"rule_id": id, "rule_version": 1, "direction": initial, "effective_at": now.UTC().Format(time.RFC3339), "expires_at": "", "values": values}
		v.Notes = append(v.Notes, "No tuning rule is installed. Defaults below are read from the runtime registry. The first version requires a signature and an explicit expiry.")
	}
	return v
}
