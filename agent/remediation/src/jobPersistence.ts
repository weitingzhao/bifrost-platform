import fs from 'node:fs'
import path from 'node:path'
import type { RemediationJob } from './types.js'

function configuredJobsDir(): string {
  const custom = process.env.REMEDIATION_JOBS_DIR?.trim()
  if (custom) return custom.replace(/^~/, process.env.HOME ?? '')
  const agentRoot = process.env.BIFROST_AGENT_ROOT?.trim()
  if (agentRoot) {
    return path.join(agentRoot.replace(/^~/, process.env.HOME ?? ''), 'jobs')
  }
  const projectRoot = process.env.PLATFORM_PROJECT_ROOT?.trim()
  if (projectRoot) {
    return path.join(projectRoot, 'agent', 'remediation-jobs')
  }
  return path.join(process.env.HOME ?? '', 'bifrost-agent', 'jobs')
}

function localJobsFallback(configured: string): string {
  return path.join(path.dirname(configured), 'jobs-local')
}

function isBrokenSymlink(target: string): boolean {
  try {
    const st = fs.lstatSync(target)
    if (!st.isSymbolicLink()) return false
    fs.realpathSync(target)
    return false
  } catch {
    return true
  }
}

let resolvedJobsDir: string | undefined

/** Writable jobs directory — falls back to jobs-local when NAS symlink is unmounted. */
export function jobsDir(): string {
  if (resolvedJobsDir != null) return resolvedJobsDir

  const configured = configuredJobsDir()
  if (!fs.existsSync(configured)) {
    resolvedJobsDir = configured
    return resolvedJobsDir
  }

  if (isBrokenSymlink(configured)) {
    const fallback = localJobsFallback(configured)
    console.warn(
      `[remediation] jobs path is a broken symlink (${configured}); using local fallback ${fallback}`,
    )
    resolvedJobsDir = fallback
    return resolvedJobsDir
  }

  const st = fs.lstatSync(configured)
  if (!st.isDirectory()) {
    const fallback = localJobsFallback(configured)
    console.warn(`[remediation] jobs path is not a directory (${configured}); using ${fallback}`)
    resolvedJobsDir = fallback
    return resolvedJobsDir
  }

  resolvedJobsDir = configured
  return resolvedJobsDir
}

function jobPath(id: string): string {
  return path.join(jobsDir(), `${id}.json`)
}

function readJobsFromDir(dir: string): RemediationJob[] {
  if (!fs.existsSync(dir)) return []
  const out: RemediationJob[] = []
  for (const name of fs.readdirSync(dir)) {
    if (!name.endsWith('.json')) continue
    try {
      const raw = fs.readFileSync(path.join(dir, name), 'utf8')
      const job = JSON.parse(raw) as RemediationJob
      if (job.id != null) out.push(job)
    } catch {
      // skip corrupt file
    }
  }
  return out
}

export function loadPersistedJobs(): RemediationJob[] {
  try {
    const primary = jobsDir()
    const configured = configuredJobsDir()
    const dirs =
      primary === configured ? [primary] : [primary, configured].filter((d, i, a) => a.indexOf(d) === i)
    const merged = new Map<string, RemediationJob>()
    for (const dir of dirs) {
      for (const job of readJobsFromDir(dir)) {
        merged.set(job.id, job)
      }
    }
    return [...merged.values()].sort((a, b) => b.updated_at.localeCompare(a.updated_at))
  } catch (err) {
    console.error('[remediation] loadPersistedJobs failed:', err)
    return []
  }
}

export function persistJob(job: RemediationJob): void {
  try {
    const dir = jobsDir()
    fs.mkdirSync(dir, { recursive: true })
    const { ...publicJob } = job
    fs.writeFileSync(jobPath(job.id), JSON.stringify(publicJob, null, 2), 'utf8')
  } catch (err) {
    console.error('[remediation] persistJob failed (job still runs in memory):', err)
  }
}
