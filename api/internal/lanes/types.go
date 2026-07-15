package lanes

import "regexp"

// Lane is a Briefing work lane — declarative catalog entity.
type Lane struct {
	ID            string `yaml:"id" json:"id"`
	Track         string `yaml:"track" json:"track"`
	ComponentLine string `yaml:"component_line" json:"component_line"`
	TrackType     string `yaml:"track_type" json:"track_type"`
	Label         string `yaml:"label" json:"label"`
	ShortLabel    string `yaml:"short_label" json:"short_label"`
	Description   string `yaml:"description" json:"description"`
	AgentMode     string `yaml:"agent_mode" json:"agent_mode"`
	WorkIntent    string `yaml:"work_intent" json:"work_intent"`
}

type File struct {
	Version string `yaml:"version" json:"version"`
	Lanes   []Lane `yaml:"lanes" json:"lanes"`
}

type ListResponse struct {
	Version string `json:"version"`
	Lanes   []Lane `json:"lanes"`
}

type CreateRequest struct {
	ID            string `json:"id"`
	Track         string `json:"track"`
	ComponentLine string `json:"component_line"`
	TrackType     string `json:"track_type"`
	Label         string `json:"label"`
	ShortLabel    string `json:"short_label"`
	Description   string `json:"description"`
	AgentMode     string `json:"agent_mode"`
	WorkIntent    string `json:"work_intent"`
}

var laneIDRe = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)

var allowedTracks = map[string]bool{
	"build": true, "migrate": true, "automate": true, "infra": true, "operate": true,
}

var allowedComponentLines = map[string]bool{
	"rocket": true, "satellite": true, "engineer": true,
	"ground": true, "operations": true, "subcontractor": true,
}

var allowedTrackTypes = map[string]bool{
	"build": true, "migrate": true, "maintain": true, "release": true,
}

var allowedAgentModes = map[string]bool{
	"Ops": true, "Product": true, "Promote": true,
}

var allowedWorkIntents = map[string]bool{
	"feature": true, "cluster": true, "ops": true, "frontend": true,
	"automate": true, "debug": true, "release": true, "business": true,
}

func ValidateLane(l Lane) error {
	if !laneIDRe.MatchString(l.ID) {
		return errf("id must be kebab-case alphanumeric (2–63 chars)")
	}
	if !allowedTracks[l.Track] {
		return errf("track must be one of build|migrate|automate|infra|operate")
	}
	if !allowedComponentLines[l.ComponentLine] {
		return errf("component_line invalid")
	}
	if !allowedTrackTypes[l.TrackType] {
		return errf("track_type must be one of build|migrate|maintain|release")
	}
	if l.Label == "" {
		return errf("label required")
	}
	if l.ShortLabel == "" {
		return errf("short_label required")
	}
	if l.Description == "" {
		return errf("description required")
	}
	if !allowedAgentModes[l.AgentMode] {
		return errf("agent_mode must be Ops|Product|Promote")
	}
	if !allowedWorkIntents[l.WorkIntent] {
		return errf("work_intent invalid")
	}
	return nil
}

func (r CreateRequest) ToLane() Lane {
	return Lane{
		ID:            r.ID,
		Track:         r.Track,
		ComponentLine: r.ComponentLine,
		TrackType:     r.TrackType,
		Label:         r.Label,
		ShortLabel:    r.ShortLabel,
		Description:   r.Description,
		AgentMode:     r.AgentMode,
		WorkIntent:    r.WorkIntent,
	}
}

type validationError struct{ msg string }

func (e *validationError) Error() string { return e.msg }

func errf(msg string) error { return &validationError{msg: msg} }

func IsValidation(err error) bool {
	_, ok := err.(*validationError)
	return ok
}
