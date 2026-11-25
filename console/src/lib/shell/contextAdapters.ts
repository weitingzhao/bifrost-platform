import { registerContextAdapter } from '@/lib/shell/sharedContext'
import { consoleContext } from '@/lib/shell/consoleContext'
import { agentsContext } from '@/domains/coordination/communication/agentsContext'

/*
 * Every place's adapter for the shared context, registered once by the shell. Each view's line adds its own line
 * here with its adapter (City and the ledger: World; Signals; Agents); a place without one keeps its frame link as
 * its host gives it.
 */
let done = false
export function registerContextAdapters(): void {
  if (done) return
  done = true
  registerContextAdapter('console', consoleContext)
  registerContextAdapter('agents', agentsContext)
}
