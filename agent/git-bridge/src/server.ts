import express from 'express'
import { execSync } from 'node:child_process'
import path from 'node:path'
import fs from 'node:fs'

const app = express()
app.use(express.json())

const PORT = parseInt(process.env.GIT_BRIDGE_PORT ?? '8785', 10)
const WORKSPACE = process.env.GIT_WORKSPACE_ROOT ?? '/Users/vision-mac-trader/Desktop/stocks'

const MANAGED_REPOS = [
  'bifrost-platform',
  'bifrost-ui',
  'bifrost-trade-infra',
  'bifrost-trade-frontend',
  'bifrost-trade-core',
  'bifrost-trade-socket',
  'bifrost-trade-worker',
  'bifrost-trade-api',
]

function git(repoDir: string, args: string): string {
  return execSync(`git -C "${repoDir}" ${args}`, {
    encoding: 'utf-8',
    timeout: 30_000,
  }).trim()
}

function isGitRepo(dir: string): boolean {
  return fs.existsSync(path.join(dir, '.git'))
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
    dirty: boolean
    staged: string[]
    modified: string[]
    untracked: string[]
    ahead: number
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

      results.push({
        repo: name,
        branch,
        dirty: lines.length > 0,
        staged,
        modified,
        untracked,
        ahead,
      })
    } catch (err) {
      results.push({
        repo: name,
        branch: '(error)',
        dirty: false,
        staged: [],
        modified: [],
        untracked: [],
        ahead: 0,
      })
    }
  }

  res.json({
    workspace: WORKSPACE,
    repos: results,
    dirty_repos: results.filter(r => r.dirty).map(r => r.repo),
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
