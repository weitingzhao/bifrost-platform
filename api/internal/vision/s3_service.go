package vision

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

var s3SpineMilestoneIDs = []string{
	"vision-v1-dev-topology",
	"vision-s3-briefing-alignment",
	"vision-v2-dev-agent",
	"vision-v3-ops-agent",
	"vision-v4-business-agent",
	"vision-v5-convergence",
}

func (s *Service) S3Gate(ctx context.Context) V1GateResponse {
	now := time.Now().UTC()
	rec, _ := s.s3store.LoadGate()
	signoff, _ := s.s3store.LoadSignoff()
	checks := s.collectS3Checks(ctx)
	result := "pass"
	for _, c := range checks {
		if c.Required && c.Reachability == probe.ReachFail {
			result = "fail"
			break
		}
	}
	blockers := gateBlockers(checks)
	ready := result == "pass" && len(blockers) == 0
	reach := probe.ReachOK
	if result == "fail" {
		reach = probe.ReachFail
	} else if !ready {
		reach = probe.ReachDegraded
	}
	out := V1GateResponse{
		Milestone:    "S3",
		Result:       result,
		Ready:        ready,
		Blockers:     blockers,
		Checks:       checks,
		Reachability: reach,
		Detail:       fmt.Sprintf("Vision S3 Briefing ↔ Vision alignment gate %s (%d checks)", result, len(checks)),
		GeneratedAt:  now,
	}
	if rec != nil {
		at := rec.At.UTC()
		out.At = &at
		if rec.Result != "" {
			out.Result = rec.Result
			out.Ready = rec.Result == "pass" && len(blockers) == 0
		}
	}
	if signoff != nil {
		at := signoff.At.UTC()
		out.SignedAt = &at
		out.SignedBy = signoff.SignedBy
	}
	return out
}

func (s *Service) RunS3Gate(ctx context.Context, triggeredBy string) (RunGateResponse, error) {
	now := time.Now().UTC()
	checks := s.collectS3Checks(ctx)
	result := "pass"
	for _, c := range checks {
		if c.Required && c.Reachability == probe.ReachFail {
			result = "fail"
			break
		}
	}
	rec := GateRecord{
		At:          now,
		Result:      result,
		Checks:      checks,
		TriggeredBy: triggeredBy,
		Summary:     fmt.Sprintf("Vision S3 gate %s (%d checks)", result, len(checks)),
	}
	if err := s.s3store.SaveGate(rec); err != nil {
		return RunGateResponse{}, err
	}
	gate := s.S3Gate(ctx)
	msg := fmt.Sprintf("Vision S3 gate %s", result)
	if !gate.Ready {
		msg += fmt.Sprintf(" (blocked: %s)", strings.Join(gate.Blockers, "; "))
	}
	return RunGateResponse{
		OK:          result == "pass",
		Action:      "vision.s3-gate",
		Target:      "vision-s3-briefing-alignment",
		Changed:     true,
		Message:     msg,
		Gate:        gate,
		GeneratedAt: now,
	}, nil
}

func (s *Service) SignS3(ctx context.Context, notes, signedBy string) (SignoffResponse, error) {
	now := time.Now().UTC()
	gate := s.S3Gate(ctx)
	if !gate.Ready {
		return SignoffResponse{}, fmt.Errorf("Vision S3 gate not ready — run gate first and fix blockers")
	}
	rec := V1SignoffRecord{
		At:       now,
		SignedBy: signedBy,
		Notes:    strings.TrimSpace(notes),
		GateAt:   now,
		Result:   "SIGNED",
	}
	if gate.At != nil {
		rec.GateAt = *gate.At
	}
	if err := s.s3store.SaveSignoff(rec); err != nil {
		return SignoffResponse{}, err
	}
	gate = s.S3Gate(ctx)
	return SignoffResponse{
		OK:          true,
		Action:      "vision.s3-signoff",
		Target:      "vision-s3-briefing-alignment",
		Changed:     true,
		Message:     "Vision S3 Briefing ↔ Vision alignment SIGNED",
		Gate:        gate,
		GeneratedAt: now,
	}, nil
}

func (s *Service) collectS3Checks(ctx context.Context) []GateCheck {
	checks := make([]GateCheck, 0, 12)
	checks = append(checks, s.checkV1Prerequisite())
	checks = append(checks, s.checkSpineMapFile())
	checks = append(checks, s.checkFileContains(
		"briefing-pack-vision", "buildBriefingPack.ts includes vision appendix", true,
		consolePath(s.configDir, "src/lib/briefing/buildBriefingPack.ts"),
		"formatVisionBriefingSection",
	))
	checks = append(checks, s.checkFileContains(
		"alignment-pack-vision", "buildBriefingAlignmentPack references visionSpineMap", true,
		consolePath(s.configDir, "src/lib/briefing/buildBriefingAlignmentPack.ts"),
		"visionSpineMap",
	))
	checks = append(checks, s.checkFileContains(
		"ui-progress-vision", "uiProgressSnapshot lists Dual Flywheel Vision", true,
		consolePath(s.configDir, "src/lib/briefing/uiProgressSnapshot.ts"),
		"Dual Flywheel Vision",
	))
	checks = append(checks, s.checkFileContains(
		"governance-queue-vision", "workLanes governance uses visionGovernanceQueueItems", true,
		consolePath(s.configDir, "src/lib/briefing/workLanes.ts"),
		"visionGovernanceQueueItems",
	))
	for _, id := range s3SpineMilestoneIDs {
		checks = append(checks, s.checkSpineMilestone(id))
	}
	return checks
}

func (s *Service) checkV1Prerequisite() GateCheck {
	check := GateCheck{
		ID: "v1-prerequisite", Label: "Vision V1 signed (prerequisite)", Required: true,
		Reachability: probe.ReachFail, Detail: "V1 signoff not recorded",
	}
	signoff, err := s.store.LoadSignoff()
	if err != nil {
		check.Detail = err.Error()
		return check
	}
	if signoff == nil || signoff.Result != "SIGNED" {
		return check
	}
	check.Reachability = probe.ReachOK
	check.Detail = fmt.Sprintf("V1 SIGNED by %s", signoff.SignedBy)
	return check
}

func (s *Service) checkSpineMapFile() GateCheck {
	check := GateCheck{
		ID: "spine-map-catalog", Label: "visionSpineMap.ts (V1–V5 map)", Required: true,
		Reachability: probe.ReachFail, Detail: "file not found",
	}
	path := consolePath(s.configDir, "src/lib/architecture/visionSpineMap.ts")
	data, err := os.ReadFile(path)
	if err != nil {
		check.Detail = path + ": " + err.Error()
		return check
	}
	body := string(data)
	if !strings.Contains(body, "VISION_SPINE_MAP") {
		check.Detail = "missing VISION_SPINE_MAP export"
		return check
	}
	count := 0
	for _, id := range []string{"V1", "V2", "V3", "V4", "V5"} {
		if strings.Contains(body, `visionId: '`+id+`'`) || strings.Contains(body, `"`+id+`"`) {
			count++
		}
	}
	if count < 5 {
		check.Detail = fmt.Sprintf("expected 5 vision entries, found %d", count)
		return check
	}
	check.Reachability = probe.ReachOK
	check.Detail = "5 V1–V5 entries in visionSpineMap.ts"
	return check
}

func (s *Service) checkSpineMilestone(id string) GateCheck {
	label := "Spine milestone: " + id
	check := GateCheck{
		ID: "spine-" + id, Label: label, Required: true,
		Reachability: probe.ReachFail, Detail: "not in ops-context.yaml",
	}
	if s.cfg == nil || s.cfg.OpsContext == nil {
		return check
	}
	for _, m := range s.cfg.OpsContext.Milestones {
		if m.ID != id {
			continue
		}
		check.Reachability = probe.ReachOK
		check.Detail = "status=" + m.Status
		if id == "vision-v1-dev-topology" && m.Status != "SIGNED" {
			check.Reachability = probe.ReachFail
			check.Detail = "V1 must be SIGNED before S3"
		}
		return check
	}
	return check
}

func (s *Service) checkFileContains(id, label string, required bool, path, needle string) GateCheck {
	check := GateCheck{
		ID: id, Label: label, Required: required,
		Reachability: probe.ReachFail, Detail: "file not found",
	}
	data, err := os.ReadFile(path)
	if err != nil {
		check.Detail = filepath.Base(path) + ": " + err.Error()
		return check
	}
	if strings.Contains(string(data), needle) {
		check.Reachability = probe.ReachOK
		check.Detail = filepath.Base(path) + " OK"
		return check
	}
	check.Detail = "missing " + needle + " in " + filepath.Base(path)
	return check
}

func consolePath(configDir, rel string) string {
	candidates := []string{}
	if configDir != "" {
		candidates = append(candidates, filepath.Join(configDir, "..", "console", rel))
	}
	candidates = append(candidates, filepath.Join("console", rel))
	for _, p := range candidates {
		if abs, err := filepath.Abs(p); err == nil {
			if _, err := os.Stat(abs); err == nil {
				return abs
			}
		}
	}
	if configDir != "" {
		return filepath.Join(configDir, "..", "console", rel)
	}
	return filepath.Join("console", rel)
}
