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
	fmt.Fprintf(&b, "Generated: %s UTC\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "session_id: %s\n", orDash(req.SessionID))
	fmt.Fprintf(&b, "program_id: %s\n", orDash(req.ProgramID))
	fmt.Fprintf(&b, "phase_id: %s\n", orDash(req.PhaseID))
  fmt.Fprintf(&b, "Pack size: **%s** · Track: **%s** · Lane: **%s** · Intent: **%s**\n", packSize, orDash(req.Track), orDash(req.Lane), intent)
	b.WriteString("Queue stage: see Console Task Queue for active/completed lane items.\n\n")
	b.WriteString("## Session binding\n\n")
	b.WriteString("Progress reports must use this session_id with matching program_id + phase_id.\n")
	b.WriteString("To advance to another phase: MCP `create_session` with the new phase_id, then `report_phase_progress` with that new session.\n")
	b.WriteString("When the phase defines verify_cmd, status=done requires verify_passed=true after you run verify locally.\n\n")

	if ctx != nil {
		b.WriteString("## Spine focus\n\n")
		fmt.Fprintf(&b, "- Headline: %s\n", ctx.Focus.Headline)
		if ctx.Focus.Blocker != "" {
			fmt.Fprintf(&b, "- Blocker: %s\n", ctx.Focus.Blocker)
		}
		fmt.Fprintf(&b, "- Active track: %s · Deployment phase: %s\n\n", ctx.Deployment.ActiveTrack, ctx.Deployment.Phase)

		if packSize == "full" {
			b.WriteString("## Milestones (snapshot)\n\n")
			for _, m := range ctx.Milestones {
				fmt.Fprintf(&b, "- [%s] %s — %s\n", m.Status, m.ID, m.Label)
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
		fmt.Fprintf(&b, "- **%s**: ok %d · fail %d · degraded %d\n", m.Environment, ok, fail, deg)
	}
	b.WriteString("\n")

	if clusterDetail != "" {
		b.WriteString("## Cluster\n\n")
		fmt.Fprintf(&b, "- Reachability: %s — %s\n\n", clusterReach, clusterDetail)
	}

	if baselineAt != "" {
		b.WriteString("## Session baseline\n\n")
		fmt.Fprintf(&b, "- Previous snapshot: %s\n\n", baselineAt)
	}

	b.WriteString("## Agent protocol (required first reply — `/briefing`)\n\n")
	b.WriteString("When the Owner types `/briefing` (or this pack is the first chat message), reply in the Owner dialogue language using **five sections**. Do not implement until the Owner confirms direction.\n\n")
	b.WriteString("1. **Original Session Title and Content** — Echo Title/Content (and lane id) from the active Briefing lane / Session verbatim; list session_id/program_id/phase_id or state unbound.\n")
	b.WriteString("2. **Understanding in project context** — What this Session means in Bifrost now; what is out of scope (e.g. D10 live trading).\n")
	b.WriteString("3. **Why (sources)** — Table split into **system facts** (spine/matrix/MCP/lanes.yaml/code/verify) vs **directional guidance** (Agent Protocol, migration rules, Governance catalogs, Owner consensus).\n")
	b.WriteString("4. **Session status** — Plan/discovery vs planned-and-executing, with evidence (queue stage, session binding).\n")
	b.WriteString("5. **Next directions** — Numbered options; invite Owner to change direction **or** confirm and execute. Wait for reply before code changes.\n")
	b.WriteString("\nAuthority: Console pack generator `buildBriefingPack.ts` · Owner contract 2026-07-22.\n")

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
