import type { LandingGrant, LandingWait } from '@/api/landingGrant'

export function grantState(g: LandingGrant, now = Date.now()): string {
  if (g.handed_to_push) return 'Handed to push'
  if (g.revoked) return 'Revoked'
  if (Date.parse(g.expires_at) <= now) return 'Expired'
  return g.valid ? 'Authorized, waiting for push' : 'Not currently usable'
}
export function landingWaitWords(w: LandingWait): string {
  const kind = w.soft_at_request.some(s => s === 'core' || s.startsWith('core:')) ? 'trust-core' : 'soft guardrail'
  const request = w.approval_number ? `#${w.approval_number}` : 'request'
  switch (w.shown) {
    case 'authorized': return w.grants.some(g => grantState(g) === 'Authorized, waiting for push')
      ? 'Authorized, waiting for push' : 'No usable grant in this read; refresh the landing stream'
    case 'handed_to_push': return 'Handed to push'
    case 'awaiting_owner': return `Waiting for your tier D ${kind} approval ${request}`
    case 'request_stale': return `Request ${request} is stale; waiting for the coordinator to refresh`
    case 'request_main_unknown': return `Main could not be checked for ${request}; approval readiness is unknown`
    case 'approval_observed': return `Approval ${request} is ${w.approval_status}; waiting for a grant in the landing stream`
    case 'awaiting_request': return w.approval_status === 'unknown'
      ? `Request ${request} has an unknown result; read the landing stream before retrying`
      : `Waiting for the coordinator to create a tier D ${kind} request`
    default: return `Landing ${w.phase.replaceAll('_', ' ')}`
  }
}
