/** Arguments accepted by Console shell `openAgentDesk`. */
export type OpenAgentDeskArg =
  | string
  | { prefill: string }
  | { focusHandoffId: string }
  | { focusDecisionBriefs?: boolean; focusDecisionBriefId?: string }

export function isOpenAgentDeskPrefill(
  arg: OpenAgentDeskArg,
): arg is { prefill: string } {
  return typeof arg === 'object' && arg != null && 'prefill' in arg
}

export function isOpenAgentDeskFocusHandoff(
  arg: OpenAgentDeskArg,
): arg is { focusHandoffId: string } {
  return typeof arg === 'object' && arg != null && 'focusHandoffId' in arg
}

export function isOpenAgentDeskFocusDecisionBriefs(
  arg: OpenAgentDeskArg,
): arg is { focusDecisionBriefs?: boolean; focusDecisionBriefId?: string } {
  return (
    typeof arg === 'object' &&
    arg != null &&
    ('focusDecisionBriefs' in arg || 'focusDecisionBriefId' in arg) &&
    !('focusHandoffId' in arg) &&
    !('prefill' in arg)
  )
}
