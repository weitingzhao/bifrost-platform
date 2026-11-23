package codeguard

// Assembly's imports of verification packages verification has not made public
// (assembly_contracts_test.go), 2026-11-22. The list only shrinks.
var _ = heldAssemblyReach(
	"api/internal/agentrt/planwiring -> api/internal/agentrt/rules (verification)",
	"api/internal/agentrt/planwiring -> api/internal/agentrt/store (verification)",
	"api/internal/server -> api/internal/agentrt/controller (verification)",
	"api/internal/server -> api/internal/agentrt/evidence (verification)",
	"api/internal/server -> api/internal/agentrt/fullci (verification)",
	"api/internal/server -> api/internal/agentrt/landing (verification)",
	"api/internal/server -> api/internal/agentrt/landingci (verification)",
	"api/internal/server -> api/internal/agentrt/landinggit (verification)",
	"api/internal/server -> api/internal/agentrt/rules (verification)",
	"api/internal/server -> api/internal/agentrt/rulesapi (verification)",
	"api/internal/server -> api/internal/agentrt/runfacts (verification)",
	"api/internal/server -> api/internal/agentrt/store (verification)",
	"api/internal/server -> api/internal/ciconfig (verification)",
	"api/internal/server -> api/internal/cidomains (verification)",
	"api/internal/server -> api/internal/cirunview (verification)",
	"api/internal/server -> api/internal/ciscore (verification)",
	"api/internal/server -> api/internal/evidenceread (verification)",
	"api/internal/server -> api/internal/unitexec (verification)",
	"api/internal/server -> api/internal/unitplan (verification)",
	"api/internal/server -> api/internal/unitwatch (verification)",
	"api/internal/server -> api/internal/verification/assembly (verification)",
)
