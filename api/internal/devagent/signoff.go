package devagent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var visionGatePhaseSignoffFiles = map[string]string{
	"V1": "vision_v1_gate_signoff.json",
	"S3": "vision_s3_gate_signoff.json",
	"V2": "vision_v2_gate_signoff.json",
	"V3": "vision_v3_gate_signoff.json",
	"V4": "vision_v4_gate_signoff.json",
	"V5": "vision_v5_gate_signoff.json",
}

type visionGateSignoffRecord struct {
	At       time.Time `json:"at"`
	SignedBy string    `json:"signed_by"`
	Notes    string    `json:"notes,omitempty"`
	Result   string    `json:"result"`
}

func visionGateSignoffFromConfig(configDir, phaseID string) (at, by string) {
	fname, ok := visionGatePhaseSignoffFiles[phaseID]
	if !ok {
		return "", ""
	}
	path := filepath.Join(configDir, fname)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	var rec visionGateSignoffRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return "", ""
	}
	if rec.Result != "SIGNED" {
		return "", ""
	}
	return rec.At.UTC().Format(time.RFC3339), rec.SignedBy
}

func (h *Handler) syncVisionSignoffsFromGateFiles() error {
	rt, ok := h.runtimes["vision"]
	if !ok {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if rt.state == nil {
		rt.state = &ProgramStateRecord{ProgramID: "vision", History: []Job{}}
	}
	changed := false
	for phaseID := range visionGatePhaseSignoffFiles {
		if h.phaseSignoffRecordLocked(rt, phaseID) != nil {
			continue
		}
		at, by := visionGateSignoffFromConfig(h.configDir, phaseID)
		if at == "" {
			continue
		}
		if err := h.applyPhaseSignoffLocked(rt, phaseID, by, at, "Synced from vision gate file (Wave 3a)"); err != nil {
			return err
		}
		changed = true
	}
	if changed {
		return h.persistRuntimeLocked("vision")
	}
	return nil
}

// RecordPhaseSignoff persists Owner sign-off for a program phase (D12 single write path).
func (h *Handler) RecordPhaseSignoff(programID, phaseID, signedOffBy, signedOffAt, notes string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	rt, ok := h.runtimes[programID]
	if !ok {
		return fmt.Errorf("program not found: %s", programID)
	}
	if !phaseExists(rt.blueprint, phaseID) {
		return fmt.Errorf("phase not found: %s", phaseID)
	}
	if rt.state == nil {
		rt.state = &ProgramStateRecord{ProgramID: programID, History: []Job{}}
	}
	if err := h.applyPhaseSignoffLocked(rt, phaseID, signedOffBy, signedOffAt, notes); err != nil {
		return err
	}
	return h.persistRuntimeLocked(programID)
}

func (h *Handler) phaseSignoffRecordLocked(rt *programRuntime, phaseID string) *PhaseSignOffRecord {
	if rt.state == nil {
		return nil
	}
	for i := range rt.state.PhaseSignOffs {
		if rt.state.PhaseSignOffs[i].PhaseID == phaseID && rt.state.PhaseSignOffs[i].SignedOffAt != "" {
			return &rt.state.PhaseSignOffs[i]
		}
	}
	return nil
}

func (h *Handler) applyPhaseSignoffLocked(rt *programRuntime, phaseID, signedOffBy, signedOffAt, notes string) error {
	signedAt := strings.TrimSpace(signedOffAt)
	if signedAt != "" {
		if _, err := time.Parse(time.RFC3339, signedAt); err != nil {
			return fmt.Errorf("signed_off_at must be RFC3339")
		}
	} else {
		signedAt = time.Now().UTC().Format(time.RFC3339)
	}
	by := strings.TrimSpace(signedOffBy)
	if by == "" {
		by = "owner"
	}
	for i := range rt.state.PhaseSignOffs {
		if rt.state.PhaseSignOffs[i].PhaseID == phaseID {
			if rt.state.PhaseSignOffs[i].SignedOffAt != "" {
				return fmt.Errorf("phase already signed off")
			}
			rt.state.PhaseSignOffs[i].SignedOffAt = signedAt
			rt.state.PhaseSignOffs[i].SignedOffBy = by
			rt.state.PhaseSignOffs[i].Notes = notes
			for j := range rt.phases {
				if rt.phases[j].ID == phaseID {
					rt.phases[j].Status = PhaseDone
					rt.phases[j].CompletedAt = signedAt
				}
			}
			return nil
		}
	}
	rt.state.PhaseSignOffs = append(rt.state.PhaseSignOffs, PhaseSignOffRecord{
		PhaseID: phaseID, SignedOffAt: signedAt, SignedOffBy: by, Notes: notes,
	})
	for i := range rt.phases {
		if rt.phases[i].ID == phaseID {
			rt.phases[i].Status = PhaseDone
			rt.phases[i].CompletedAt = signedAt
		}
	}
	return nil
}
