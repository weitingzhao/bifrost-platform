export function collapseExpandAriaLabel(open: boolean, subject = 'section'): string {
  return open ? `Collapse ${subject}` : `Expand ${subject}`
}
