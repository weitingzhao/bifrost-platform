package opscontext

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Meta struct {
	Version         string `yaml:"version" json:"version"`
	CatalogVersion  string `yaml:"catalog_version" json:"catalog_version"`
}

type Deployment struct {
	Phase       string `yaml:"phase" json:"phase"`
	ActiveTrack string `yaml:"active_track" json:"active_track"`
}

type Focus struct {
	Headline        string `yaml:"headline" json:"headline"`
	FlywheelPrimary string `yaml:"flywheel_primary" json:"flywheel_primary"`
	Blocker         string `yaml:"blocker,omitempty" json:"blocker,omitempty"`
}

type Milestone struct {
	ID             string `yaml:"id" json:"id"`
	Label          string `yaml:"label,omitempty" json:"label,omitempty"`
	Status         string `yaml:"status" json:"status"`
	Blocker        string `yaml:"blocker,omitempty" json:"blocker,omitempty"`
	SignedAt       string `yaml:"signed_at,omitempty" json:"signed_at,omitempty"`
	Authority      string `yaml:"authority,omitempty" json:"authority,omitempty"`
	PipelineLane   string `yaml:"pipeline_lane,omitempty" json:"pipeline_lane,omitempty"`
	PipelineAfter  string `yaml:"pipeline_after,omitempty" json:"pipeline_after,omitempty"`
}

type Decision struct {
	ID         string `yaml:"id" json:"id"`
	Status     string `yaml:"status" json:"status"`
	Topic      string `yaml:"topic,omitempty" json:"topic,omitempty"`
	Conclusion string `yaml:"conclusion" json:"conclusion"`
	SignedAt   string `yaml:"signed_at,omitempty" json:"signed_at,omitempty"`
	Authority  string `yaml:"authority,omitempty" json:"authority,omitempty"`
}

type PlatformPhase struct {
	ID            string `yaml:"id" json:"id"`
	Label         string `yaml:"label" json:"label"`
	Timeframe     string `yaml:"timeframe" json:"timeframe"`
	Deliverables  string `yaml:"deliverables" json:"deliverables"`
}

type LastGate struct {
	At       *string `yaml:"at" json:"at"`
	Result   *string `yaml:"result" json:"result"`
	LogPath  string  `yaml:"log_path" json:"log_path"`
}

type Promotion struct {
	LastGate LastGate `yaml:"last_gate" json:"last_gate"`
}

type EnvironmentExtended struct {
	Status string `yaml:"status" json:"status"`
	Note   string `yaml:"note,omitempty" json:"note,omitempty"`
}

type ProbeHint struct {
	TargetID   string `yaml:"target_id" json:"target_id"`
	TradeRoute string `yaml:"trade_route" json:"trade_route"`
	Hint       string `yaml:"hint" json:"hint"`
}

type NorthStar struct {
	ID              string   `yaml:"id" json:"id"`
	Statement       string   `yaml:"statement" json:"statement"`
	Strategy        string   `yaml:"strategy" json:"strategy"`
	Principles      []string `yaml:"principles" json:"principles"`
	OwnerException  string   `yaml:"owner_exception" json:"owner_exception"`
	Authority       string   `yaml:"authority" json:"authority"`
	SuccessCriteria []string `yaml:"success_criteria" json:"success_criteria"`
}

type TrackTask struct {
	ID     string `yaml:"id" json:"id"`
	Label  string `yaml:"label" json:"label"`
	Status string `yaml:"status" json:"status"`
}

type BuildTrack struct {
	Label        string      `yaml:"label" json:"label"`
	CurrentPhase string      `yaml:"current_phase" json:"current_phase"`
	Tasks        []TrackTask `yaml:"tasks" json:"tasks"`
}

type MigrateStream struct {
	ID       string  `yaml:"id" json:"id"`
	Label    string  `yaml:"label" json:"label"`
	Total    int     `yaml:"total" json:"total"`
	Done     int     `yaml:"done" json:"done"`
	Status   string  `yaml:"status" json:"status"`
	NextTask *string `yaml:"next_task,omitempty" json:"next_task,omitempty"`
	Note     string  `yaml:"note,omitempty" json:"note,omitempty"`
}

type MigrateTrack struct {
	Label   string          `yaml:"label" json:"label"`
	Streams []MigrateStream `yaml:"streams" json:"streams"`
}

type OperateTrack struct {
	Label string `yaml:"label" json:"label"`
	Note  string `yaml:"note,omitempty" json:"note,omitempty"`
}

type Tracks struct {
	Build   *BuildTrack   `yaml:"build,omitempty" json:"build,omitempty"`
	Migrate *MigrateTrack `yaml:"migrate,omitempty" json:"migrate,omitempty"`
	Operate *OperateTrack `yaml:"operate,omitempty" json:"operate,omitempty"`
}

type File struct {
	Meta                 Meta                            `yaml:"meta" json:"meta"`
	NorthStar            *NorthStar                      `yaml:"north_star,omitempty" json:"north_star,omitempty"`
	Deployment           Deployment                      `yaml:"deployment" json:"deployment"`
	Focus                Focus                           `yaml:"focus" json:"focus"`
	Milestones           []Milestone                     `yaml:"milestones" json:"milestones"`
	Decisions            []Decision                      `yaml:"decisions" json:"decisions"`
	PlatformPhases       []PlatformPhase                 `yaml:"platform_phases" json:"platform_phases"`
	CouplingSurfaces     []string                        `yaml:"coupling_surfaces" json:"coupling_surfaces"`
	Promotion            Promotion                       `yaml:"promotion" json:"promotion"`
	EnvironmentsExtended map[string]EnvironmentExtended  `yaml:"environments_extended" json:"environments_extended"`
	ProbeHints           []ProbeHint                     `yaml:"probe_hints" json:"probe_hints"`
	Tracks               *Tracks                         `yaml:"tracks,omitempty" json:"tracks,omitempty"`
}

func ContextPathFromConfigDir(configDir string) string {
	return filepath.Join(configDir, "ops-context.yaml")
}

func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read ops context %s: %w", path, err)
	}
	var file File
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse ops context: %w", err)
	}
	if err := validate(&file); err != nil {
		return nil, err
	}
	return &file, nil
}

func validate(f *File) error {
	if f.Meta.Version == "" {
		return fmt.Errorf("ops context: meta.version required")
	}
	if f.Meta.CatalogVersion == "" {
		return fmt.Errorf("ops context: meta.catalog_version required")
	}
	if f.Deployment.Phase == "" {
		return fmt.Errorf("ops context: deployment.phase required")
	}
	if f.Focus.Headline == "" {
		return fmt.Errorf("ops context: focus.headline required")
	}
	if len(f.Milestones) == 0 {
		return fmt.Errorf("ops context: milestones required")
	}
	return nil
}

func ResolvePath(configPath string) string {
	if p := os.Getenv("PLATFORM_OPS_CONTEXT"); p != "" {
		return p
	}
	return ContextPathFromConfigDir(filepath.Dir(configPath))
}
