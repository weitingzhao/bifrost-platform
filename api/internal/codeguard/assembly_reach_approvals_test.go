package codeguard

// Assembly's imports of approvals packages approvals has not made public
// (assembly_contracts_test.go), 2026-11-22. The list only shrinks.
var _ = heldAssemblyReach(
	"api/internal/agentrt/planwiring -> api/internal/questions (approvals)",
	"api/internal/server -> api/internal/actions (approvals)",
	"api/internal/server -> api/internal/actuationpolicy (approvals)",
	"api/internal/server -> api/internal/agentgovernance (approvals)",
	"api/internal/server -> api/internal/agentrt/approvalfacts (approvals)",
	"api/internal/server -> api/internal/agentrt/questionbridge (approvals)",
	"api/internal/server -> api/internal/owner/approvalhome (approvals)",
	"api/internal/server -> api/internal/approvalnotify (approvals)",
	"api/internal/server -> api/internal/approvalreadings (approvals)",
	"api/internal/server -> api/internal/approvalrules (approvals)",
	"api/internal/server -> api/internal/approvals (approvals)",
	"api/internal/server -> api/internal/agentrt/assessmentreadings (approvals)",
	"api/internal/server -> api/internal/assessmentview (approvals)",
	"api/internal/server -> api/internal/homelayout (approvals)",
	"api/internal/server -> api/internal/instruments (approvals)",
	"api/internal/server -> api/internal/owner/assembly (approvals)",
	"api/internal/server -> api/internal/workactions (approvals)",
)
