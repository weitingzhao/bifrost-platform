package briefing

import (
	"fmt"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/opscontext"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

type PackRequest struct {
	Track     string
	Lane      string
	Intent    string
	PackSize  string
	SessionID string
	ProgramID string
	PhaseID   string
}

type PackResponse struct {
	Pack         string    `json:"pack"`
	PackSize     string    `json:"pack_size"`
	Track        string    `json:"track,omitempty"`
	Lane         string    `json:"lane,omitempty"`
	Intent       string    `json:"intent,omitempty"`
	CharCount    int       `json:"char_count"`
	GeneratedAt  time.Time `json:"generated_at"`
	HasBaseline  bool      `json:"has_baseline"`
	BaselineAt   string    `json:"baseline_at,omitempty"`
}

func BuildSessionPack(
	ctx *opscontext.File,
	matrices []probe.MatrixResponse,
	clusterReach string,
	clusterDetail string,
	baselineAt string,
	req PackRequest,
) PackResponse {
	packSize := req.PackSize
	if packSize == "" {
		packSize = "compact"
	}
	intent := req.Intent
	if intent == "" {
		intent = "ops"
	}

	var b strings.Builder
	b.WriteString("# Bifrost Ops — Session briefing (MCP/API)\n\n")
	b.WriteString(fmt.Sprintf("Generated: %s UTC\n", time.Now().UTC().Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("session_id: %s\n", orDash(req.SessionID)))
	b.WriteString(fmt.Sprintf("program_id: %s\n", orDash(req.ProgramID)))
	b.WriteString(fmt.Sprintf("phase_id: %s\n", orDash(req.PhaseID)))
  b.WriteString(fmt.Sprintf("Pack size: **%s** · Track: **%s** · Lane: **%s** · Intent: **%s**\n", packSize, orDash(req.Track), orDash(req.Lane), intent))
	b.WriteString("Queue stage: see Console Task Queue for active/completed lane items.\n\n")
	b.WriteString("## Session binding\n\n")
	b.WriteString("Progress reports must use this session_id with matching program_id + phase_id.\n")
	b.WriteString("To advance to another phase: MCP `create_session` with the new phase_id, then `report_phase_progress` with that new session.\n")
	b.WriteString("When the phase defines verify_cmd, status=done requires verify_passed=true after you run verify locally.\n\n")

	if ctx != nil {
		b.WriteString("## Spine focus\n\n")
		b.WriteString(fmt.Sprintf("- Headline: %s\n", ctx.Focus.Headline))
		if ctx.Focus.Blocker != "" {
			b.WriteString(fmt.Sprintf("- Blocker: %s\n", ctx.Focus.Blocker))
		}
		b.WriteString(fmt.Sprintf("- Active track: %s · Deployment phase: %s\n\n", ctx.Deployment.ActiveTrack, ctx.Deployment.Phase))

		if packSize == "full" {
			b.WriteString("## Milestones (snapshot)\n\n")
			for _, m := range ctx.Milestones {
				b.WriteString(fmt.Sprintf("- [%s] %s — %s\n", m.Status, m.ID, m.Label))
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("## Live matrix\n\n")
	for _, m := range matrices {
		ok, fail, deg := 0, 0, 0
		for _, t := range m.Targets {
			switch t.Reachability {
			case "ok":
				ok++
			case "fail":
				fail++
			default:
				deg++
			}
		}
		b.WriteString(fmt.Sprintf("- **%s**: ok %d · fail %d · degraded %d\n", m.Environment, ok, fail, deg))
	}
	b.WriteString("\n")

	if clusterDetail != "" {
		b.WriteString("## Cluster\n\n")
		b.WriteString(fmt.Sprintf("- Reachability: %s — %s\n\n", clusterReach, clusterDetail))
	}

	if baselineAt != "" {
		b.WriteString("## Session baseline\n\n")
		b.WriteString(fmt.Sprintf("- Previous snapshot: %s\n\n", baselineAt))
	}

	b.WriteString("## Agent protocol (required first reply)\n\n")
	b.WriteString("1. Confirm briefing understanding in the Owner's dialogue language.\n")
	b.WriteString("2. Provide a numbered task list — wait for Owner selection before implementing.\n")
	b.WriteString("3. Include a Source Audit table (provenance per fact).\n")

	pack := b.String()
	return PackResponse{
		Pack:        pack,
		PackSize:    packSize,
		Track:       req.Track,
		Lane:        req.Lane,
		Intent:      intent,
		CharCount:   len(pack),
		GeneratedAt: time.Now().UTC(),
		HasBaseline: baselineAt != "",
		BaselineAt:  baselineAt,
	}
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
