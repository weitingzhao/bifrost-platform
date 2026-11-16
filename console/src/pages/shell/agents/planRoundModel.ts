import { formatShellHash } from '@/lib/shell/consoleRoutes'

/** Work showing a round: the newest when none is named, else that round. No `section=`, so nothing scrolls to the plan's top. */
export const workRoundHref = (root = '') => formatShellHash('work', root ? new URLSearchParams({ root }).toString() : '')
