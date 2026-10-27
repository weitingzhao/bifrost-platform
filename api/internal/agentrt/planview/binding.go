package planview

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

const BindingKind = "agentrt.plan_binding/v1"

// Binding pins one condition to one acceptance and one real step. Empty
// bindings remain legal for visibility-only plans; they prove nothing.
type Binding struct {
	V                int      `json:"v"`
	Kind             string   `json:"kind"`
	ID               string   `json:"id"`
	Version          int      `json:"version"`
	Tenant           string   `json:"tenant"`
	Root             string   `json:"root"`
	Node             string   `json:"node"`
	Step             string   `json:"step"`
	Acceptance       string   `json:"acceptance"`
	AcceptanceSHA256 string   `json:"acceptance_sha256"`
	Atom             string   `json:"atom"`
	Source           string   `json:"source"`
	Mission          string   `json:"mission,omitempty"`
	Task             string   `json:"task,omitempty"`
	Attempt          string   `json:"attempt,omitempty"`
	Round            int      `json:"round,omitempty"`
	Result           string   `json:"result,omitempty"`
	Repo             string   `json:"repo,omitempty"`
	Verification     string   `json:"verification,omitempty"`
	ExternalWork     string   `json:"external_work,omitempty"`
	ExternalSeq      int64    `json:"external_seq,omitempty"`
	Report           string   `json:"report,omitempty"`
	Application      string   `json:"application,omitempty"`
	Environment      string   `json:"environment,omitempty"`
	Check            string   `json:"check,omitempty"`
	Spec             string   `json:"spec_sha256,omitempty"`
	Evidence         []string `json:"evidence,omitempty"`
}

var sha = regexp.MustCompile(`^[0-9a-f]{40}$`)
var hash = regexp.MustCompile(`^[0-9a-f]{64}$`)
var artifact = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var bindingID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

func AcceptanceDigest(a Acceptance) string {
	raw, _ := json.Marshal(a)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}
func (b Binding) Digest() string {
	if b.Source == "owner_signoff" {
		b.Evidence = nil // Evidence belongs to the signed event, never the adopted identity.
	}
	raw, _ := json.Marshal(b)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}
func (b Binding) SignoffRef() string { return "plan-acceptance:" + b.Digest() }
func ParseBinding(raw []byte) (Binding, error) {
	var b Binding
	if v, err := canon.Parse(raw); err != nil {
		return b, err
	} else if obj, ok := v.(map[string]any); ok {
		if obj["source"] == "owner_signoff" {
			if _, present := obj["evidence"]; present {
				return b, fmt.Errorf("Owner evidence must be attached at signing")
			}
		}
		for _, value := range obj {
			if value == nil {
				return b, fmt.Errorf("null binding field")
			}
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&b); err != nil {
		return b, err
	}
	if b.V != 1 || b.Kind != BindingKind || !bindingID.MatchString(b.ID) || b.Version < 1 || b.Tenant != "dev" || !text(b.Root) || !text(b.Node) || !text(b.Step) || !text(b.Acceptance) || !hash.MatchString(b.AcceptanceSHA256) {
		return b, fmt.Errorf("invalid binding identity")
	}
	if !slices.Contains([]string{"main_effective", "deployment_verified", "review_valid", "external_verified"}, b.Atom) {
		return b, fmt.Errorf("unknown binding atom")
	}
	if b.Task != "" && b.Round < 1 || b.Task == "" && b.Round != 0 {
		return b, fmt.Errorf("runtime round required")
	}
	if b.Source == "owner_signoff" {
		if b.ExternalWork != "" || b.ExternalSeq != 0 || b.Report != "" || b.Evidence != nil || b.Atom != "external_verified" || b.Verification != "" || b.Task != "" || b.Attempt != "" || b.Mission != "" || b.Repo != "" || b.Result != "" {
			return b, fmt.Errorf("Owner source pins an acceptance, not a runtime result")
		}
	} else {
		if b.Source != "git" && b.Source != "verification" && b.Source != "deployment" {
			return b, fmt.Errorf("untrusted binding source")
		}
		if !sha.MatchString(b.Result) || !text(b.Repo) {
			return b, fmt.Errorf("result identity required")
		}
		if b.Source != "git" && (b.Task == "" || b.Attempt == "" || b.Mission == "") {
			return b, fmt.Errorf("exact task/attempt required")
		}
		if (b.Task == "") != (b.Attempt == "") || (b.Task == "") != (b.Mission == "") {
			return b, fmt.Errorf("partial runtime identity")
		}
		if b.Atom == "main_effective" && b.Source != "git" || b.Atom == "deployment_verified" && b.Source != "deployment" || (b.Atom == "review_valid" || b.Atom == "external_verified") && b.Source != "verification" {
			return b, fmt.Errorf("atom/source mismatch")
		}
		if b.Source == "verification" && !text(b.Verification) {
			return b, fmt.Errorf("verification reference required")
		}
		if len(b.Evidence) != 0 {
			return b, fmt.Errorf("runtime evidence comes from the verifier")
		}
	}
	if b.Atom == "deployment_verified" {
		if !text(b.Application) || !slices.Contains([]string{"dev", "stg", "prod"}, b.Environment) || !text(b.Check) || !hash.MatchString(b.Spec) {
			return b, fmt.Errorf("deployment identity and health specification required")
		}
	} else if b.Application != "" || b.Environment != "" || b.Check != "" || b.Spec != "" {
		return b, fmt.Errorf("unexpected deployment fields")
	}
	if b.Atom == "external_verified" {
		if b.Source == "verification" && b.ExternalWork == "" {
			return b, fmt.Errorf("external verification requires submitted work")
		}
	} else if b.ExternalWork != "" {
		return b, fmt.Errorf("unexpected external work")
	}
	if b.ExternalWork != "" {
		if b.ExternalSeq < 1 || !artifact.MatchString(b.Report) {
			return b, fmt.Errorf("external submission version required")
		}
	} else if b.ExternalSeq != 0 || b.Report != "" {
		return b, fmt.Errorf("unexpected external version")
	}
	if b.Source != "verification" && b.Verification != "" {
		return b, fmt.Errorf("unexpected verification")
	}
	return b, nil
}

func (d *Document) Bindings() ([]Binding, error) {
	var out []Binding
	seen := map[string]bool{}
	stepOwners := map[string]string{}
	for _, n := range d.Items {
		for _, raw := range n.Bindings {
			b, err := ParseBinding(raw)
			if err != nil {
				return nil, err
			}
			if b.Root != d.Root || b.Node != n.Node || b.Tenant != d.Tenant || b.Version != d.Version || seen[b.ID] {
				return nil, fmt.Errorf("binding scope/version mismatch or duplicate: %s", b.ID)
			}
			found := false
			for _, a := range n.Acceptance {
				if a.ID == b.Acceptance && AcceptanceDigest(a) == b.AcceptanceSHA256 {
					found = true
				}
			}
			if !found {
				return nil, fmt.Errorf("binding acceptance missing or changed: %s", b.ID)
			}
			if owner := stepOwners[b.Step]; owner != "" && owner != b.Node {
				return nil, fmt.Errorf("step is bound by multiple view nodes")
			}
			stepOwners[b.Step] = b.Node
			seen[b.ID] = true
			out = append(out, b)
		}
	}
	return out, nil
}
