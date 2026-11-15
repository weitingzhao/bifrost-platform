import type { TimingResult, TimingRun, TimingTask } from '@/api/ciTimings'

export const result = (results: TimingResult[], name: string) => results.find(r => r.name === name)?.value
export const runCommit = (run: TimingRun) => result(run.results, 'platform-commit') || result(run.results, 'commit') || run.revision || ''
// The delivery read API exposes Tekton's Succeeded condition, not its reason.
export function runStatus(run: TimingRun): string {
  if (run.status === 'True') return 'Succeeded'
  if (run.status === 'False') return 'Failed'
  if (run.status === 'Unknown' && run.reason === 'Running') return 'Running'
  return run.status
}
export const reusedBy = (run: TimingRun) => result(run.results, 'tested-by') || ''
export function seconds(from?: string, to?: string): number | null {
  if (!from || !to) return null
  const value = (Date.parse(to) - Date.parse(from)) / 1000
  return Number.isFinite(value) && value >= 0 ? value : null
}
export const secondsText = (n: number | null) => n == null ? 'Not recorded' : `${Number(n.toFixed(1))}s`
export const elapsed = (run: TimingRun) => seconds(run.start_time, run.completion_time)
export const taskSeconds = (task: TimingTask) => seconds(task.created_at, task.finished_at)
const firstStep = (task: TimingTask) => task.steps.map(s => s.started_at).filter((s): s is string => !!s).sort()[0]
export const taskWait = (task: TimingTask) => seconds(task.created_at, firstStep(task))
export function beforeTests(run: TimingRun): number | null {
  if (reusedBy(run)) return null
  const starts = run.tasks.filter(t => /^test-(api|console)/.test(t.task) && !run.skipped.includes(t.task))
    .filter(t => t.task !== 'test-console' || consoleState(run) !== 'Skipped by scope')
    .map(t => t.created_at).filter((s): s is string => !!s).sort()
  return seconds(run.start_time, starts[0])
}
export function scopeResult(run: TimingRun, name: string): string | undefined {
  return result(run.results, name) || result(run.tasks.find(t => t.task === 'test-api')?.results ?? [], name)
}
export function consoleState(run: TimingRun): string {
  if (reusedBy(run)) return 'Reused with the whole run'
  if (run.skipped.includes('test-console')) return 'Skipped by pipeline'
  const modules = result(run.tasks.find(t => t.task === 'changes')?.results ?? [], 'run-modules')
  if (modules != null && modules.trim() !== '*' && !modules.split(/\s+/).includes('console')) return 'Skipped by scope'
  const task = run.tasks.find(t => t.task === 'test-console')
  if (!task) return 'Not recorded'
  if (modules == null) return `Task ${task.status}; execution not reported`
  return `Required · ${task.status}`
}
export type ChangeKind = 'Backend only' | 'Console only' | 'Both' | 'Other / shared' | 'Unknown'
export const CHANGE_KINDS: ChangeKind[] = ['Backend only', 'Console only', 'Both', 'Other / shared', 'Unknown']
export function changeKind(files: string[] | undefined): ChangeKind {
  if (!files) return 'Unknown'
  const api = files.some(f => f.startsWith('api/')), ui = files.some(f => f.startsWith('console/'))
  if (api && ui) return 'Both'
  // Config, shared inputs and root-level changes are a separate cohort.
  if (files.length === 0 || files.some(f => !f.startsWith('api/') && !f.startsWith('console/'))) return 'Other / shared'
  return api ? 'Backend only' : 'Console only'
}
export function median(values: number[]): number | null {
  if (!values.length) return null
  const sorted = [...values].sort((a,b) => a-b), middle = Math.floor(sorted.length / 2)
  return sorted.length % 2 ? sorted[middle] : (sorted[middle-1]+sorted[middle])/2
}
export function trendRuns(runs: TimingRun[]): TimingRun[] {
  const seen = new Set<string>()
  return runs.filter(run => {
    const commit = runCommit(run)
    if (runStatus(run) !== 'Succeeded' || reusedBy(run) || run.trigger === 'agentrt-full-ci' || run.name.startsWith('ci-full-') || elapsed(run) == null || !commit || seen.has(commit)) return false
    seen.add(commit); return true
  })
}
export type SlowTiming = { name: string; seconds: number; kind: 'Package' | 'Test'; run: string }
export function slowTimings(log: string, run: string): SlowTiming[] {
  // Tekton keeps only log tails. Never turn missing/cached timings into zero.
  const rows: SlowTiming[] = []
  const cachedPackages = new Set<string>()
  for (const raw of log.split('\n')) {
    const line = raw.split('\u001b').map(part => part.replace(/^\[[0-9;]*m/, '')).join('')
    if (line.startsWith('{')) {
      try {
        const event = JSON.parse(line) as { Action?: string; Package?: string; Test?: string; Elapsed?: number; Output?: string }
        if (event.Package && event.Output?.includes('(cached)')) cachedPackages.add(event.Package)
        if (event.Package && (event.Action === 'pass' || event.Action === 'fail') && typeof event.Elapsed === 'number' && Number.isFinite(event.Elapsed) && event.Elapsed >= 0) {
          rows.push({ name:event.Test ? `${event.Package} · ${event.Test}` : event.Package, seconds:event.Elapsed, kind:event.Test ? 'Test' : 'Package', run })
        }
      } catch { /* A partial log line is not a timing. */ }
      continue
    }
    const go = line.match(/^ok\s+(\S+)\s+(\d+(?:\.\d+)?)s(?:\s|$)/)
    const test = line.match(/^(?:--- (?:PASS|FAIL): (.+?) \((\d+(?:\.\d+)?)s\))/)
    const vitest = line.match(/[✓✔×❯]\s+(.+?)\s+\(\d+ tests?[^)]*\)\s+(\d+(?:\.\d+)?)m?s\s*$/)
    if (go) rows.push({ name:go[1], seconds:Number(go[2]), kind:'Package', run })
    else if (test) rows.push({ name:test[1], seconds:Number(test[2]), kind:'Test', run })
    else if (vitest) rows.push({ name:vitest[1], seconds:Number(vitest[2]) / (line.trim().endsWith('ms') ? 1000 : 1), kind:'Test', run })
  }
  return rows.filter(row => ![...cachedPackages].some(pkg => row.name === pkg || row.name.startsWith(`${pkg} · `))).sort((a,b) => b.seconds-a.seconds)
}
export function scopeBaseFromLog(log: string): string | null {
  return log.match(/against main \(merge base ([a-f0-9]{40})\)/)?.[1] ?? null
}
