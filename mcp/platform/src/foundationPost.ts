/**
 * foundation-post: Foundation's two readings from a machine to the bus
 * (决定 2026-11-21: Foundation's state goes on the coordination bus).
 *
 *   npx tsx mcp/platform/src/foundationPost.ts guard-blocks [--log PATH]
 *   npx tsx mcp/platform/src/foundationPost.ts acceptance FILE.json
 *
 * guard-blocks: posts the lines the agent guard appended to its local block
 * log (~/.bifrost-dev/logs/agent-guard-blocks.log, one "time<TAB>rule<TAB>session"
 * per refusal, written by preflight.js) since the last run, one
 * POST /api/v1/bus/guard-blocks each, with this machine's name. Where it
 * stopped is kept beside the log (.shipped, a byte offset), so a line is
 * posted once; a line the platform refuses is skipped with one line on stderr.
 *
 * acceptance: posts one decoupling acceptance reading, the --json output of
 * benchmark/decoupling_acceptance.py, to POST /api/v1/bus/acceptance.
 *
 * The target and token are resolved the way the MCP server resolves them
 * (as doorbell.ts does); this command never prints the token.
 */
import { closeSync, existsSync, openSync, readFileSync, statSync, unlinkSync, writeFileSync } from 'node:fs'
import { homedir, hostname } from 'node:os'
import { join } from 'node:path'
import { pathToFileURL } from 'node:url'
import { settingsFromMcpConfig } from './doorbell.js'
import { defaultRuntimeDeps, type RuntimeDeps } from './runtimeTools.js'

export interface Block { rule: string; session: string; host: string; blocked_at?: string }

/** A block and the log offset just after its line. */
export interface Line { block: Block; end: number }

/** The blocks in the log's text after offset, and the offset after the last whole line. */
export function blocksSince(text: string, offset: number, host: string): { blocks: Block[]; next: number; lines: Line[] } {
  const blocks: Block[] = []
  const lines: Line[] = []
  let next = offset
  const rest = text.slice(offset)
  let pos = 0
  for (;;) {
    const nl = rest.indexOf('\n', pos)
    if (nl < 0) break
    const line = rest.slice(pos, nl)
    pos = nl + 1
    next = offset + Buffer.byteLength(rest.slice(0, pos))
    const [at, rule, session] = line.split('\t')
    if (rule) {
      const block: Block = { rule, session: session || '-', host }
      if (/^\d{4}-\d{2}-\d{2}T[\d:.]+Z$/.test(at ?? '')) block.blocked_at = at
      blocks.push(block)
      lines.push({ block, end: next })
    }
  }
  return { blocks, next, lines }
}

/** A machine name the platform accepts (letters, digits, _.-). */
export function machineName(raw: string): string {
  return raw.replace(/\.local$/, '').replace(/[^A-Za-z0-9_.-]/g, '-').slice(0, 63)
}

export async function shipBlocks(logPath: string, deps: RuntimeDeps, err: (l: string) => void): Promise<number> {
  if (!existsSync(logPath)) return 0
  const mark = `${logPath}.shipped`
  const text = readFileSync(logPath, 'utf8')
  let offset = existsSync(mark) ? Number(readFileSync(mark, 'utf8').trim()) || 0 : 0
  if (offset > Buffer.byteLength(text)) offset = 0 // the log was rotated
  // The log is ASCII (an ISO time, a rule id, a session id), so bytes and characters agree.
  const { lines, next } = blocksSince(text, offset, machineName(hostname()))
  let sent = 0
  let done = offset
  for (const { block, end } of lines) {
    try {
      await deps.send('POST', '/api/v1/bus/guard-blocks', block)
      sent++
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e)
      // 400: the platform will never take this line; skip it. Anything else
      // (the platform away, a 5xx, a token refused) stops here, so the line is
      // posted on the next run and none after it is posted out of order.
      if (!/HTTP 400\b/.test(msg)) {
        writeFileSync(mark, String(done))
        err(`guard-blocks: stopped at ${block.rule} (${msg.slice(0, 160)}); it is posted on the next run`)
        return sent
      }
      err(`guard-blocks: refused ${block.rule}: ${msg.slice(0, 200)}`)
    }
    done = end
  }
  writeFileSync(mark, String(next))
  return sent
}

/** The machine's block log, where preflight.js appends one line per refusal. */
export function defaultBlockLog(): string {
  return join(homedir(), '.bifrost-dev', 'logs', 'agent-guard-blocks.log')
}

/** How old a lock may be before it is taken as left by a process that died. */
const STALE_LOCK_MS = 2 * 60_000

/**
 * shipBlocks under a lock beside the log, so the doorbells of several
 * sessions on one machine do not post the same lines twice. A lock younger
 * than STALE_LOCK_MS means another process is shipping: this run posts
 * nothing. Returns the number posted, or -1 when it did not run.
 */
export async function shipBlocksLocked(logPath: string, deps: RuntimeDeps, err: (l: string) => void): Promise<number> {
  if (!existsSync(logPath)) return 0
  const lock = `${logPath}.lock`
  let fd: number
  try {
    fd = openSync(lock, 'wx')
  } catch {
    try {
      if (Date.now() - statSync(lock).mtimeMs < STALE_LOCK_MS) return -1
      unlinkSync(lock)
      fd = openSync(lock, 'wx')
    } catch {
      return -1
    }
  }
  try {
    return await shipBlocks(logPath, deps, err)
  } finally {
    closeSync(fd)
    try {
      unlinkSync(lock)
    } catch {
      // another process took a stale lock meanwhile; nothing to undo
    }
  }
}

/** The acceptance script's --json output as the route's body. */
export function acceptanceBody(raw: { ruler?: number; score?: string; head?: string; read_at?: string; items?: Array<{ item: number; status: string }> }): Record<string, unknown> {
  const items = [...(raw.items ?? [])].sort((a, b) => a.item - b.item).map((i) => i.status)
  return { ruler: raw.ruler, score: raw.score, items, platform_commit: raw.head, read_at: raw.read_at }
}

export async function main(argv: string[], deps: RuntimeDeps = defaultRuntimeDeps()): Promise<number> {
  if (!process.env.PLATFORM_API_URL) {
    const settings = settingsFromMcpConfig(readFileSync('.mcp.json', 'utf8'), 'bifrost-platform', process.env)
    for (const [k, v] of Object.entries(settings)) if (v) process.env[k] = v
  }
  const [mode, arg] = argv
  if (mode === 'guard-blocks') {
    const i = argv.indexOf('--log')
    const log = i >= 0 ? argv[i + 1] : defaultBlockLog()
    const n = await shipBlocksLocked(log, deps, (l) => console.error(l))
    console.log(`guard-blocks: ${n} posted`)
    return 0
  }
  if (mode === 'acceptance' && arg) {
    const res = (await deps.send('POST', '/api/v1/bus/acceptance', acceptanceBody(JSON.parse(readFileSync(arg, 'utf8'))))) as { error?: unknown; event?: number }
    if (res?.error) throw new Error(JSON.stringify(res.error).slice(0, 300))
    console.log(`acceptance: event ${res?.event}`)
    return 0
  }
  throw new Error('usage: foundationPost.ts guard-blocks [--log PATH] | acceptance FILE.json')
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? '').href) {
  main(process.argv.slice(2)).then(
    (code) => process.exit(code),
    (e: unknown) => {
      console.error(`foundation-post: ${e instanceof Error ? e.message : String(e)}`)
      process.exit(1)
    },
  )
}
