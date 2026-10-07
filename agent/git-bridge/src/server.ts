import express from 'express'
import type { NextFunction, Request, Response } from 'express'
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import path from 'node:path'
import fs from 'node:fs'
import { pathToFileURL } from 'node:url'
import { bearerMatches, loadGitBridgeTokens, resolveGitBridgeBind } from './auth.js'

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

export type GitRunner = (
  repoDir: string,
  args: string[],
  opts?: { readOnly?: boolean },
) => Promise<string>

/**
 * Every git call runs off the event loop. With execSync a push and its
 * pre-push hooks blocked the whole process, /health went silent, and the bdev
 * watchdog killed the bridge mid-push (266 restarts before 2026-09-26).
 *
 * Arguments go to git as an argv array — no shell, so a commit message is
 * passed as written. Read-only calls set GIT_OPTIONAL_LOCKS=0: a status probe
 * never takes index.lock from under a commit being made in the shared worktree.
 */
export async function git(repoDir: string, args: string[], opts: { readOnly?: boolean } = {}): Promise<string> {
  const { stdout } = await execFileAsync('git', ['-C', repoDir, ...args], {
    encoding: 'utf-8',
    timeout: 30_000,
    env: opts.readOnly ? { ...process.env, GIT_OPTIONAL_LOCKS: '0' } : process.env,
  })
  // trimEnd only — trim() would strip the leading space on porcelain
  // first lines (` M path`) and mis-parse them as staged `ata/...`.
  return stdout.replace(/\s+$/, '')
}

type GitCtx = {
  git: GitRunner
  workspace: string
  managedRepos: readonly string[]
}

const readGit = (ctx: GitCtx, repoDir: string, args: string[]) => ctx.git(repoDir, args, { readOnly: true })

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
async function aheadOfUpstream(ctx: GitCtx, dir: string): Promise<number> {
  try {
    return parseInt(await readGit(ctx, dir, ['rev-list', '--count', '@{u}..HEAD']), 10) || 0
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

async function repoLineStats(ctx: GitCtx, dir: string): Promise<{ insertions: number; deletions: number }> {
  let insertions = 0
  let deletions = 0
  try {
    const [staged, unstaged] = await Promise.all([
      readGit(ctx, dir, ['diff', '--cached', '--numstat']).then(parseNumstat),
      readGit(ctx, dir, ['diff', '--numstat']).then(parseNumstat),
    ])
    insertions = staged.insertions + unstaged.insertions
    deletions = staged.deletions + unstaged.deletions
  } catch {
    // ignore
  }
  // Untracked files are not in numstat; count their lines via filesystem when small enough.
  try {
    const untracked = await readGit(ctx, dir, ['ls-files', '--others', '--exclude-standard'])
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

async function repoStatus(ctx: GitCtx, name: string): Promise<RepoStatus | null> {
  const dir = path.join(ctx.workspace, name)
  if (!isGitRepo(dir)) return null

  try {
    const [branch, statusPorcelain, ahead, headSha] = await Promise.all([
      readGit(ctx, dir, ['rev-parse', '--abbrev-ref', 'HEAD']),
      readGit(ctx, dir, ['status', '--porcelain']),
      aheadOfUpstream(ctx, dir),
      readGit(ctx, dir, ['rev-parse', '--short', 'HEAD']),
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
    const stats = dirty ? await repoLineStats(ctx, dir) : { insertions: 0, deletions: 0 }

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

/**
 * Explicit file list for `git add --`. Rejects an empty list, `.`, and any
 * path that could leave the repo or be parsed as a flag.
 */
export function normalizeCommitPaths(paths: unknown): { ok: true; paths: string[] } | { ok: false; error: string } {
  if (!Array.isArray(paths) || paths.length === 0) {
    return { ok: false, error: 'paths[] required' }
  }
  const out: string[] = []
  for (const raw of paths) {
    if (typeof raw !== 'string') return { ok: false, error: 'paths[] must be strings' }
    const item = raw.trim()
    if (item === '' || item === '.' || item === './') {
      return { ok: false, error: 'paths[] must name specific files' }
    }
    if (item.startsWith('-') || item.startsWith('/') || item.includes('\\')) {
      return { ok: false, error: 'path must stay inside the repo' }
    }
    if (item.split('/').includes('..')) {
      return { ok: false, error: 'path must stay inside the repo' }
    }
    out.push(item)
  }
  return { ok: true, paths: out }
}

/** Push only the ref that HEAD names. Never `--all`, never a bare `git push`. */
export function branchRefspec(branch: string): string | null {
  if (branch === '' || branch === 'HEAD') return null
  if (!/^[A-Za-z0-9][A-Za-z0-9._/-]*$/.test(branch)) return null
  if (branch.includes('..') || branch.endsWith('/') || branch.endsWith('.lock') || branch.includes('@{')) {
    return null
  }
  return `HEAD:refs/heads/${branch}`
}

export type GitBridgeAppOptions = {
  tokens: readonly string[]
  workspace?: string
  managedRepos?: readonly string[]
  git?: GitRunner
}

export function createGitBridgeApp(options: GitBridgeAppOptions): express.Express {
  const tokens = options.tokens
  const ctx: GitCtx = {
    git: options.git ?? git,
    workspace: options.workspace ?? WORKSPACE,
    managedRepos: options.managedRepos ?? MANAGED_REPOS,
  }
  const app = express()
  app.use(express.json())

  function requireBearer(req: Request, res: Response, next: NextFunction): void {
    if ((req.method === 'GET' || req.method === 'HEAD') && req.path === '/health') {
      next()
      return
    }
    if (tokens.length === 0) {
      next()
      return
    }
    if (!bearerMatches(req.header('authorization'), tokens)) {
      res.status(401).json({ error: 'bearer token required' })
      return
    }
    next()
  }
  app.use(requireBearer)

  app.get('/health', (_req, res) => {
    res.json({ status: 'ok', workspace: ctx.workspace, repos: ctx.managedRepos.length })
  })

  app.get('/status', async (_req, res) => {
    const scanned = await Promise.all(ctx.managedRepos.map(name => repoStatus(ctx, name)))
    const results = scanned.filter((r): r is RepoStatus => r != null)

    res.json({
      workspace: ctx.workspace,
      deploy_branch: DEPLOY_BRANCH,
      platform_pipeline_mirror_repos: [...PLATFORM_PIPELINE_MIRROR_REPOS],
      repos: results,
      dirty_repos: results.filter(r => r.dirty).map(r => r.repo),
      needs_main_for_deploy: results.filter(r => r.needs_main_for_deploy).map(r => r.repo),
    })
  })

  app.post('/diff', async (req, res) => {
    const { repos } = req.body as { repos?: string[] }
    const targetRepos = (repos ?? [...ctx.managedRepos]).filter(name => ctx.managedRepos.includes(name))

    const scanned = await Promise.all(
      targetRepos.map(async name => {
        const dir = path.join(ctx.workspace, name)
        if (!isGitRepo(dir)) return null
        try {
          const [staged, unstaged, untrackedFiles] = await Promise.all([
            readGit(ctx, dir, ['diff', '--cached', '--stat']),
            readGit(ctx, dir, ['diff', '--stat']),
            readGit(ctx, dir, ['ls-files', '--others', '--exclude-standard']),
          ])
          const combined = [staged, unstaged, untrackedFiles].filter(Boolean).join('\n')
          return combined !== '' ? { repo: name, diff: combined } : null
        } catch {
          return null
        }
      }),
    )
    const diffs = scanned.filter((d): d is { repo: string; diff: string } => d != null)

    res.json({ diffs })
  })

  app.post('/commit', async (req, res) => {
    const { repos, message, paths } = req.body as { repos?: string[]; message?: string; paths?: unknown }

    if (!Array.isArray(repos) || repos.length === 0) {
      res.status(400).json({ error: 'repos[] required' })
      return
    }
    if (typeof message !== 'string' || message.trim() === '') {
      res.status(400).json({ error: 'message required' })
      return
    }
    const commitMessage = message
    const normalized = normalizeCommitPaths(paths)
    if (!normalized.ok) {
      res.status(400).json({ error: normalized.error })
      return
    }

    const results: Array<{ repo: string; status: 'committed' | 'skipped' | 'error'; detail: string }> = []

    for (const name of repos) {
      if (!ctx.managedRepos.includes(name)) {
        results.push({ repo: name, status: 'error', detail: 'not a managed repo' })
        continue
      }
      const dir = path.join(ctx.workspace, name)
      if (!isGitRepo(dir)) {
        results.push({ repo: name, status: 'error', detail: 'not a git repo' })
        continue
      }

      results.push(
        await withRepoLock(name, async () => {
          try {
            const present = normalized.paths.filter(rel => fs.existsSync(path.join(dir, rel)))
            if (present.length === 0) {
              return { repo: name, status: 'error' as const, detail: 'none of paths[] exist in this repo' }
            }

            await ctx.git(dir, ['add', '--', ...present])
            await ctx.git(dir, ['commit', '-m', commitMessage])
            const shortSha = await ctx.git(dir, ['rev-parse', '--short', 'HEAD'])
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

  app.post('/push', async (req, res) => {
    const { repos } = req.body as { repos?: string[] }
    const targetRepos = repos ?? [...ctx.managedRepos]

    const results: Array<{ repo: string; status: 'pushed' | 'up-to-date' | 'error'; detail: string }> = []

    for (const name of targetRepos) {
      if (!ctx.managedRepos.includes(name)) continue
      const dir = path.join(ctx.workspace, name)
      if (!isGitRepo(dir)) continue

      results.push(
        await withRepoLock(name, async () => {
          try {
            const ahead = await aheadOfUpstream(ctx, dir)
            if (ahead === 0) {
              return { repo: name, status: 'up-to-date' as const, detail: 'nothing to push' }
            }

            const branch = await ctx.git(dir, ['rev-parse', '--abbrev-ref', 'HEAD'])
            const refspec = branchRefspec(branch)
            if (refspec == null) {
              return {
                repo: name,
                status: 'error' as const,
                detail: 'refusing to push: HEAD is not a single branch ref',
              }
            }
            const output = await ctx.git(dir, ['push', 'origin', refspec])
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

  app.post('/stash', (_req, res) => {
    res.status(410).json({
      error: 'POST /stash is deprecated. Stashing hides Owner WIP and causes code loss. Use POST /commit with operator approval instead.',
    })
  })

  return app
}

function start(): void {
  const loaded = loadGitBridgeTokens(WORKSPACE)
  const bindHost = resolveGitBridgeBind(loaded.tokens.length, process.env.GIT_BRIDGE_BIND)
  if (loaded.tokens.length === 0) {
    console.error(`[git-bridge] ${loaded.reason}; binding 127.0.0.1`)
  }
  const app = createGitBridgeApp({ tokens: loaded.tokens })
  const server = app.listen(PORT, bindHost, () => {
    const auth = loaded.tokens.length > 0 ? 'bearer' : 'loopback-only'
    console.log(`[git-bridge] listening on ${bindHost}:${PORT}  workspace=${WORKSPACE} auth=${auth}`)
    console.log(`[git-bridge] managed repos: ${MANAGED_REPOS.join(', ')}`)
  })

  server.on('error', (err: Error) => {
    console.error(`[git-bridge] server error: ${err.message}`)
    process.exit(1)
  })
}

process.on('uncaughtException', (err) => {
  console.error(`[git-bridge] uncaught exception: ${err.message}`)
  process.exit(1)
})

process.on('unhandledRejection', (reason) => {
  console.error(`[git-bridge] unhandled rejection: ${reason}`)
})

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  start()
}
