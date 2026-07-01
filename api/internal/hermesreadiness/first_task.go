package hermesreadiness

// FirstTask returns the Mission Signal Phase 4 — Hermes First Task (L0 read-only).
func FirstTask() FirstTaskDefinition {
	return FirstTaskDefinition{
		ID:       "hermes-mission-health-l0",
		Title:    "Mission health read-only pass (Hermes First Task)",
		Autonomy: "L0",
		Prompt: "# Hermes First Task — Mission health read-only pass (L0)\n\n" +
			"You are the Bifrost Ops autonomous engineer. This is the **first production task** — read-only only.\n\n" +
			"## Rules\n" +
			"- Use **bifrost-platform** MCP tools only (stdio).\n" +
			"- **L0 autonomy**: no writes, no kubectl actuation, no rollout restart, no deploy.\n" +
			"- Call verify_mission_snapshot and verify_payload before summarizing datastore health.\n" +
			"- If post_fix_verification.passed is false, explain blockers — do not claim Mission is fixed.\n\n" +
			"## Steps\n" +
			"1. get_agent_bridge — confirm Hermes + platform MCP + Nous gateway status.\n" +
			"2. verify_mission_snapshot — fresh matrix reprobe + post_fix verdict.\n" +
			"3. get_connectivity_matrix — list failing trade/datastore targets (if any).\n" +
			"4. Summarize: Mission/payload status, PROBE_DRIFT vs DATA_LAYER vs HTTP_FAIL, recommended next action for Owner.\n\n" +
			"## Output\n" +
			"Short structured report (English): Status, Datastore, Payload matrix, Recommended next step (diagnose / escalate L2 probe / L1 datastore / none).",
		RequiredMcpTools: []string{
			"get_agent_bridge",
			"verify_mission_snapshot",
			"verify_payload",
			"get_connectivity_matrix",
		},
		SuccessCriteria: []string{
			"Hermes session completes without actuation tools",
			"Report cites verify_mission_snapshot post_fix_verification",
			"Report distinguishes PROBE_DRIFT from DATA_LAYER",
			"Owner can paste report into Control Room or Briefing",
		},
	}
}
