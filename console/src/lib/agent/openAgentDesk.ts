/** Arguments accepted by Console shell `openAgentDesk`. */
export type OpenAgentDeskArg =
  | string
  | { prefill: string }
  | { focusHandoffId: string }

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
