import express from 'express'
import { execSync } from 'node:child_process'
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

function git(repoDir: string, args: string): string {
  // trimEnd only — trim() would strip the leading space on porcelain
  // first lines (` M path`) and mis-parse them as staged `ata/...`.
  return execSync(`git -C "${repoDir}" ${args}`, {
    encoding: 'utf-8',
    timeout: 30_000,
  }).replace(/\s+$/, '')
}

function isGitRepo(dir: string): boolean {
  return fs.existsSync(path.join(dir, '.git'))
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

function repoLineStats(dir: string): { insertions: number; deletions: number } {
  let insertions = 0
  let deletions = 0
  try {
    const staged = parseNumstat(git(dir, 'diff --cached --numstat'))
    const unstaged = parseNumstat(git(dir, 'diff --numstat'))
    insertions = staged.insertions + unstaged.insertions
    deletions = staged.deletions + unstaged.deletions
  } catch {
    // ignore
  }
  // Untracked files are not in numstat; count their lines via filesystem when small enough.
  try {
    const untracked = git(dir, 'ls-files --others --exclude-standard')
    if (untracked !== '') {
      for (const file of untracked.split('\n')) {
        if (file === '') continue
        try {
          const full = path.join(dir, file)
          const content = fs.readFileSync(full, 'utf-8')
          if (content.length > 2_000_000) {
            insertions += 1
            continue
          }
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
app.get('/status', (_req, res) => {
  const results: Array<{
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
  }> = []

  for (const name of MANAGED_REPOS) {
    const dir = path.join(WORKSPACE, name)
    if (!isGitRepo(dir)) continue

    try {
      const branch = git(dir, 'rev-parse --abbrev-ref HEAD')
      const statusPorcelain = git(dir, 'status --porcelain')
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

      let ahead = 0
      try {
        const count = git(dir, 'rev-list --count @{u}..HEAD')
        ahead = parseInt(count, 10) || 0
      } catch {
        // no upstream tracking — treat as 0
      }

      const dirty = lines.length > 0
      const onDeployBranch = branch === DEPLOY_BRANCH
      const headSha = git(dir, 'rev-parse --short HEAD')
      const stats = dirty ? repoLineStats(dir) : { insertions: 0, deletions: 0 }

      results.push({
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
      })
    } catch (err) {
      results.push({
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
      })
    }
  }

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
app.post('/diff', (req, res) => {
  const { repos } = req.body as { repos?: string[] }
  const targetRepos = repos ?? MANAGED_REPOS

  const diffs: Array<{ repo: string; diff: string }> = []
  for (const name of targetRepos) {
    if (!MANAGED_REPOS.includes(name)) continue
    const dir = path.join(WORKSPACE, name)
    if (!isGitRepo(dir)) continue

    try {
      const staged = git(dir, 'diff --cached --stat')
      const unstaged = git(dir, 'diff --stat')
      const untrackedFiles = git(dir, 'ls-files --others --exclude-standard')
      const combined = [staged, unstaged, untrackedFiles].filter(Boolean).join('\n')
      if (combined !== '') {
        diffs.push({ repo: name, diff: combined })
      }
    } catch {
      // skip
    }
  }

  res.json({ diffs })
})

// ---------------------------------------------------------------------------
// POST /commit — stage all + commit in specified repos
// ---------------------------------------------------------------------------
app.post('/commit', (req, res) => {
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

    try {
      const statusBefore = git(dir, 'status --porcelain')
      if (statusBefore === '') {
        results.push({ repo: name, status: 'skipped', detail: 'working tree clean' })
        continue
      }

      git(dir, 'add -A')
      git(dir, `commit -m "${message.replace(/"/g, '\\"')}"`)
      const shortSha = git(dir, 'rev-parse --short HEAD')
      results.push({ repo: name, status: 'committed', detail: shortSha })
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      results.push({ repo: name, status: 'error', detail: msg.slice(0, 300) })
    }
  }

  res.json({ results })
})

// ---------------------------------------------------------------------------
// POST /push — push specified repos to origin
// ---------------------------------------------------------------------------
app.post('/push', (req, res) => {
  const { repos } = req.body as { repos?: string[] }
  const targetRepos = repos ?? MANAGED_REPOS

  const results: Array<{ repo: string; status: 'pushed' | 'up-to-date' | 'error'; detail: string }> = []

  for (const name of targetRepos) {
    if (!MANAGED_REPOS.includes(name)) continue
    const dir = path.join(WORKSPACE, name)
    if (!isGitRepo(dir)) continue

    try {
      let ahead = 0
      try {
        const count = git(dir, 'rev-list --count @{u}..HEAD')
        ahead = parseInt(count, 10) || 0
      } catch {
        // no upstream
      }

      if (ahead === 0) {
        results.push({ repo: name, status: 'up-to-date', detail: 'nothing to push' })
        continue
      }

      const branch = git(dir, 'rev-parse --abbrev-ref HEAD')
      const output = git(dir, `push origin ${branch}`)
      results.push({ repo: name, status: 'pushed', detail: output || `pushed ${ahead} commit(s)` })
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      results.push({ repo: name, status: 'error', detail: msg.slice(0, 300) })
    }
  }

  res.json({ results })
})

// ---------------------------------------------------------------------------
// POST /stash — stash working tree changes (never drop; requires operator intent)
// ---------------------------------------------------------------------------
app.post('/stash', (req, res) => {
  const { repos, message, include_untracked } = req.body as {
    repos: string[]
    message?: string
    include_untracked?: boolean
  }

  if (!Array.isArray(repos) || repos.length === 0) {
    res.status(400).json({ error: 'repos[] required' })
    return
  }

  const stashMsg =
    typeof message === 'string' && message.trim() !== ''
      ? message.trim()
      : 'bifrost-git-bridge: stash to clear Fleet dirty'
  const withUntracked = include_untracked !== false
  const results: Array<{
    repo: string
    status: 'stashed' | 'skipped' | 'error'
    detail: string
  }> = []

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

    try {
      const statusBefore = git(dir, 'status --porcelain')
      if (statusBefore === '') {
        results.push({ repo: name, status: 'skipped', detail: 'working tree clean' })
        continue
      }

      const flags = withUntracked ? '-u' : ''
      const safeMsg = stashMsg.replace(/"/g, '\\"')
      const output = git(dir, `stash push ${flags} -m "${safeMsg}"`.replace(/\s+/g, ' ').trim())
      results.push({
        repo: name,
        status: 'stashed',
        detail: output || 'stashed',
      })
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err)
      results.push({ repo: name, status: 'error', detail: msg.slice(0, 300) })
    }
  }

  res.json({ results })
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
