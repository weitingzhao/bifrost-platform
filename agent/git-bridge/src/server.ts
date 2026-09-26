import express from 'express'
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import path from 'node:path'
import fs from 'node:fs'

const app = express()
app.use(express.json())

const PORT = parseInt(process.env.GIT_BRIDGE_PORT ?? '8785', 10)
const WORKSPACE = process.env.GIT_WORKSPACE_ROOT ?? '/Users/vision-mac-trader/Desktop/stocks'

const DEPLOY_BRANCH = 'main'

/** Repos cloned by bifrost-deliver-platform* Tekton at param revision (default main). */
const PLATFORM_PIPELINE_MIRROR_REPOS = ['bifrost-platform', 'bifrost-ui'] as const

const MANAGED_REPOS = [
  'bifrost-platform',
  'bifrost-platform-plugin',
  'bifrost-ui',
  'bifrost-trade-infra',
  'bifrost-trade-frontend',
  'bifrost-trade-core',
  'bifrost-trade-socket',
  'bifrost-trade-worker',
  'bifrost-trade-api',
]

/** Untracked files larger than this count as one line instead of being read. */
const UNTRACKED_READ_LIMIT = 2_000_000

const execFileAsync = promisify(execFile)

/**
 * Every git call runs off the event loop. With execSync a push and its
 * pre-push hooks blocked the whole process, /health went silent, and the bdev
 * watchdog killed the bridge mid-push (266 restarts before 2026-09-26).
 *
 * Arguments go to git as an argv array — no shell, so a commit message is
 * passed as written. Read-only calls set GIT_OPTIONAL_LOCKS=0: a status probe
 * never takes index.lock from under a commit being made in the shared worktree.
 */
async function git(repoDir: string, args: string[], opts: { readOnly?: boolean } = {}): Promise<string> {
  const { stdout } = await execFileAsync('git', ['-C', repoDir, ...args], {
    encoding: 'utf-8',
    timeout: 30_000,
    env: opts.readOnly ? { ...process.env, GIT_OPTIONAL_LOCKS: '0' } : process.env,
  })
  // trimEnd only — trim() would strip the leading space on porcelain
  // first lines (` M path`) and mis-parse them as staged `ata/...`.
  return stdout.replace(/\s+$/, '')
}

const readGit = (repoDir: string, args: string[]) => git(repoDir, args, { readOnly: true })

/**
 * Commits and pushes on one repo run one at a time. The blocked event loop
 * used to serialise them by accident; now a per-repo queue does it on purpose.
 */
const repoQueues = new Map<string, Promise<unknown>>()

function withRepoLock<T>(repo: string, fn: () => Promise<T>): Promise<T> {
  const prev = repoQueues.get(repo) ?? Promise.resolve()
  const next = prev.then(fn, fn)
  repoQueues.set(
    repo,
    next.catch(() => undefined),
  )
  return next
}

function isGitRepo(dir: string): boolean {
  return fs.existsSync(path.join(dir, '.git'))
}

/** `git rev-list --count @{u}..HEAD`; 0 when the branch has no upstream. */
async function aheadOfUpstream(dir: string): Promise<number> {
  try {
    return parseInt(await readGit(dir, ['rev-list', '--count', '@{u}..HEAD']), 10) || 0
  } catch {
    return 0
  }
}

/** Parse `git diff --numstat` lines into insertions/deletions (untracked counted as +lines). */
function parseNumstat(output: string): { insertions: number; deletions: number } {
  let insertions = 0
  let deletions = 0
  if (output === '') return { insertions, deletions }
  for (const line of output.split('\n')) {
    const parts = line.split('\t')
    if (parts.length < 3) continue
    const add = parts[0] === '-' ? 0 : parseInt(parts[0] ?? '0', 10) || 0
    const del = parts[1] === '-' ? 0 : parseInt(parts[1] ?? '0', 10) || 0
    insertions += add
    deletions += del
  }
  return { insertions, deletions }
}

async function repoLineStats(dir: string): Promise<{ insertions: number; deletions: number }> {
  let insertions = 0
  let deletions = 0
  try {
    const [staged, unstaged] = await Promise.all([
      readGit(dir, ['diff', '--cached', '--numstat']).then(parseNumstat),
      readGit(dir, ['diff', '--numstat']).then(parseNumstat),
    ])
    insertions = staged.insertions + unstaged.insertions
    deletions = staged.deletions + unstaged.deletions
  } catch {
    // ignore
  }
  // Untracked files are not in numstat; count their lines via filesystem when small enough.
  try {
    const untracked = await readGit(dir, ['ls-files', '--others', '--exclude-standard'])
    if (untracked !== '') {
      for (const file of untracked.split('\n')) {
        if (file === '') continue
        try {
          const full = path.join(dir, file)
          if ((await fs.promises.stat(full)).size > UNTRACKED_READ_LIMIT) {
            insertions += 1
            continue
          }
          const content = await fs.promises.readFile(full, 'utf-8')
          const lines = content === '' ? 0 : content.split('\n').length
          insertions += lines
        } catch {
          insertions += 1
        }
      }
    }
  } catch {
    // ignore
  }
  return { insertions, deletions }
}

// ---------------------------------------------------------------------------
// GET /health
// ---------------------------------------------------------------------------
app.get('/health', (_req, res) => {
  res.json({ status: 'ok', workspace: WORKSPACE, repos: MANAGED_REPOS.length })
})

// ---------------------------------------------------------------------------
// GET /status — scan all repos for uncommitted changes
// ---------------------------------------------------------------------------
type RepoStatus = {
  repo: string
  branch: string
  on_deploy_branch: boolean
  needs_main_for_deploy: boolean
  head_sha: string
  dirty: boolean
  staged: string[]
  modified: string[]
  untracked: string[]
  ahead: number
  insertions: number
  deletions: number
}

async function repoStatus(name: string): Promise<RepoStatus | null> {
  const dir = path.join(WORKSPACE, name)
  if (!isGitRepo(dir)) return null

  try {
    const [branch, statusPorcelain, ahead, headSha] = await Promise.all([
      readGit(dir, ['rev-parse', '--abbrev-ref', 'HEAD']),
      readGit(dir, ['status', '--porcelain']),
      aheadOfUpstream(dir),
      readGit(dir, ['rev-parse', '--short', 'HEAD']),
    ])
    const lines = statusPorcelain === '' ? [] : statusPorcelain.split('\n')

    const staged: string[] = []
    const modified: string[] = []
    const untracked: string[] = []

    for (const line of lines) {
      const idx = line[0] ?? ' '
      const wt = line[1] ?? ' '
      const file = line.slice(3)
      if (idx === '?') untracked.push(file)
      else if (idx !== ' ') staged.push(file)
      if (wt !== ' ' && wt !== '?') modified.push(file)
    }

    const dirty = lines.length > 0
    const onDeployBranch = branch === DEPLOY_BRANCH
    const stats = dirty ? await repoLineStats(dir) : { insertions: 0, deletions: 0 }

    return {
      repo: name,
      branch,
      on_deploy_branch: onDeployBranch,
      needs_main_for_deploy: !onDeployBranch && (dirty || ahead > 0),
      head_sha: headSha,
      dirty,
      staged,
      modified,
      untracked,
      ahead,
      insertions: stats.insertions,
      deletions: stats.deletions,
    }
  } catch {
    return {
      repo: name,
      branch: '(error)',
      on_deploy_branch: false,
      needs_main_for_deploy: false,
      head_sha: '',
      dirty: false,
      staged: [],
      modified: [],
      untracked: [],
      ahead: 0,
      insertions: 0,
      deletions: 0,
    }
  }
}

app.get('/status', async (_req, res) => {
  // Repos scan side by side; Promise.all keeps MANAGED_REPOS order.
  const scanned = await Promise.all(MANAGED_REPOS.map(repoStatus))
  const results = scanned.filter((r): r is RepoStatus => r != null)

  res.json({
    workspace: WORKSPACE,
    deploy_branch: DEPLOY_BRANCH,
    platform_pipeline_mirror_repos: [...PLATFORM_PIPELINE_MIRROR_REPOS],
    repos: results,
    dirty_repos: results.filter(r => r.dirty).map(r => r.repo),
    needs_main_for_deploy: results.filter(r => r.needs_main_for_deploy).map(r => r.repo),
  })
})

// ---------------------------------------------------------------------------
// POST /diff — get diff for specific repos (or all dirty repos)
// ---------------------------------------------------------------------------
app.post('/diff', async (req, res) => {
  const { repos } = req.body as { repos?: string[] }
  const targetRepos = (repos ?? MANAGED_REPOS).filter(name => MANAGED_REPOS.includes(name))

  const scanned = await Promise.all(
    targetRepos.map(async name => {
      const dir = path.join(WORKSPACE, name)
      if (!isGitRepo(dir)) return null
      try {
        const [staged, unstaged, untrackedFiles] = await Promise.all([
          readGit(dir, ['diff', '--cached', '--stat']),
          readGit(dir, ['diff', '--stat']),
          readGit(dir, ['ls-files', '--others', '--exclude-standard']),
        ])
        const combined = [staged, unstaged, untrackedFiles].filter(Boolean).join('\n')
        return combined !== '' ? { repo: name, diff: combined } : null
      } catch {
        return null // skip
      }
    }),
  )
  const diffs = scanned.filter((d): d is { repo: string; diff: string } => d != null)

  res.json({ diffs })
})

// ---------------------------------------------------------------------------
// POST /commit — stage all + commit in specified repos
// ---------------------------------------------------------------------------
app.post('/commit', async (req, res) => {
  const { repos, message } = req.body as { repos: string[]; message: string }

  if (!Array.isArray(repos) || repos.length === 0) {
    res.status(400).json({ error: 'repos[] required' })
    return
  }
  if (typeof message !== 'string' || message.trim() === '') {
    res.status(400).json({ error: 'message required' })
    return
  }

  const results: Array<{ repo: string; status: 'committed' | 'skipped' | 'error'; detail: string }> = []

  for (const name of repos) {
    if (!MANAGED_REPOS.includes(name)) {
      results.push({ repo: name, status: 'error', detail: 'not a managed repo' })
      continue
    }
    const dir = path.join(WORKSPACE, name)
    if (!isGitRepo(dir)) {
      results.push({ repo: name, status: 'error', detail: 'not a git repo' })
      continue
    }

    results.push(
      await withRepoLock(name, async () => {
        try {
          const statusBefore = await git(dir, ['status', '--porcelain'])
          if (statusBefore === '') {
            return { repo: name, status: 'skipped' as const, detail: 'working tree clean' }
          }

          await git(dir, ['add', '-A'])
          await git(dir, ['commit', '-m', message])
          const shortSha = await git(dir, ['rev-parse', '--short', 'HEAD'])
          return { repo: name, status: 'committed' as const, detail: shortSha }
        } catch (err) {
          const msg = err instanceof Error ? err.message : String(err)
          return { repo: name, status: 'error' as const, detail: msg.slice(0, 300) }
        }
      }),
    )
  }

  res.json({ results })
})

// ---------------------------------------------------------------------------
// POST /push — push specified repos to origin
// ---------------------------------------------------------------------------
app.post('/push', async (req, res) => {
  const { repos } = req.body as { repos?: string[] }
  const targetRepos = repos ?? MANAGED_REPOS

  const results: Array<{ repo: string; status: 'pushed' | 'up-to-date' | 'error'; detail: string }> = []

  for (const name of targetRepos) {
    if (!MANAGED_REPOS.includes(name)) continue
    const dir = path.join(WORKSPACE, name)
    if (!isGitRepo(dir)) continue

    results.push(
      await withRepoLock(name, async () => {
        try {
          const ahead = await aheadOfUpstream(dir)
          if (ahead === 0) {
            return { repo: name, status: 'up-to-date' as const, detail: 'nothing to push' }
          }

          const branch = await git(dir, ['rev-parse', '--abbrev-ref', 'HEAD'])
          const output = await git(dir, ['push', 'origin', branch])
          return { repo: name, status: 'pushed' as const, detail: output || `pushed ${ahead} commit(s)` }
        } catch (err) {
          const msg = err instanceof Error ? err.message : String(err)
          return { repo: name, status: 'error' as const, detail: msg.slice(0, 300) }
        }
      }),
    )
  }

  res.json({ results })
})

// ---------------------------------------------------------------------------
// POST /stash — DEPRECATED: stashing hides Owner WIP and causes code loss.
// Returns 410 Gone. Use POST /commit with operator approval instead.
// ---------------------------------------------------------------------------
app.post('/stash', (_req, res) => {
  res.status(410).json({
    error: 'POST /stash is deprecated. Stashing hides Owner WIP and causes code loss. Use POST /commit with operator approval instead.',
  })
})

// ---------------------------------------------------------------------------
// Start
// ---------------------------------------------------------------------------
const server = app.listen(PORT, '0.0.0.0', () => {
  console.log(`[git-bridge] listening on :${PORT}  workspace=${WORKSPACE}`)
  console.log(`[git-bridge] managed repos: ${MANAGED_REPOS.join(', ')}`)
})

server.on('error', (err: Error) => {
  console.error(`[git-bridge] server error: ${err.message}`)
  process.exit(1)
})

process.on('uncaughtException', (err) => {
  console.error(`[git-bridge] uncaught exception: ${err.message}`)
  process.exit(1)
})

process.on('unhandledRejection', (reason) => {
  console.error(`[git-bridge] unhandled rejection: ${reason}`)
})
