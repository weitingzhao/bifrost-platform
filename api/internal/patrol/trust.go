package patrol

import "fmt"

// EvaluateTrust decides whether a trigger may dispatch.
// L0: read tools only. Write tools → escalate.
// L1: manual may write; cron escalates unless cron_actuation=confirm.
// L2: always escalate (reserved).
func EvaluateTrust(skill PatrolSkill, trigger Trigger, writeTools map[string]struct{}) gateDecision {
	writes := writeToolsIn(skill.MCPTools, writeTools)
	switch skill.TrustLevel {
	case TrustL0:
		if len(writes) > 0 {
			return gateDecision{
				Allow:  false,
				Result: ResultEscalated,
				Reason: fmt.Sprintf("L0 patrol cannot use write tools: %v", writes),
			}
		}
		return gateDecision{Allow: true}
	case TrustL1:
		if (trigger == TriggerCron || trigger == TriggerWebhook) && skill.CronActuation != CronActuationConfirm {
			return gateDecision{
				Allow:  false,
				Result: ResultEscalated,
				Reason: "L1 automated trigger escalates; Owner confirm required for write",
			}
		}
		return gateDecision{Allow: true}
	case TrustL2:
		return gateDecision{
			Allow:  false,
			Result: ResultEscalated,
			Reason: "L2 patrol is reserved; automatic execution escalates",
		}
	default:
		return gateDecision{
			Allow:  false,
			Result: ResultEscalated,
			Reason: fmt.Sprintf("unknown trust_level %q", skill.TrustLevel),
		}
	}
}
