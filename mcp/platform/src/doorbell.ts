/**
 * doorbell: wait for this thread's platform inbox and exit when a message is
 * there, so a harness that is woken when a background command ends (a Claude
 * Code session) or a supervisor that starts a turn on exit (codex exec resume,
 * a local model) can sleep until it has mail, at no model cost while it waits.
 *
 *   npx tsx mcp/platform/src/doorbell.ts [--minutes N] [--mcp-config PATH] [--server NAME] [--no-register]
 *
 * exit 0: messages are waiting; one summary line each on stdout (id, sender,
 *         kind, size; never the body, which the receiver reads itself)
 * exit 2: none came before the deadline (default 60 minutes)
 * exit 1: an error (one line on stderr)
 *
 * The caller is the thread its session names (X-Bifrost-Session), as for the
 * MCP tools. The token is resolved the way the MCP server resolves it
 * (platformClient); this command never prints it. With PLATFORM_API_URL unset
 * it takes PLATFORM_API_URL and PLATFORM_TOKEN_ENV_KEY, and nothing else, from
 * the named server of an MCP config file (default ./.mcp.json, server
 * bifrost-platform), so a shell can run it with the same target the
 * harness's MCP server has.
 *
 * Before waiting it uploads one small presence artifact naming this thread
 * (the same body every time for one thread): a thread is registered on its
 * first write, and until then nobody can send it a message (HTTP 422 "not a
 * registered actor", 2026-11-14 doorbell test).
 */
import { readFileSync } from 'node:fs'
import { pathToFileURL } from 'node:url'
import { defaultRuntimeDeps, putArtifact, waitForMessages, type RuntimeDeps } from './runtimeTools.js'
import { presenceBody } from './presence.js'

export { presenceBody }

/** The two settings taken from an MCP config; a token is never among them. */
export const CONFIG_KEYS = ['PLATFORM_API_URL', 'PLATFORM_TOKEN_ENV_KEY'] as const

/** ${VAR} and ${VAR:-default}, expanded against env as an MCP host expands its config. */
export function expand(s: string, env: Record<string, string | undefined>): string {
  return s.replace(/\$\{([A-Za-z0-9_]+)(:-([^}]*))?\}/g, (_m, k: string, _d, d?: string) => env[k] ?? d ?? '')
}

/** The CONFIG_KEYS of one server in an MCP config's text; other keys are not read. */
export function settingsFromMcpConfig(
  text: string,
  server: string,
  env: Record<string, string | undefined>,
): Partial<Record<(typeof CONFIG_KEYS)[number], string>> {
  const cfg = JSON.parse(text) as { mcpServers?: Record<string, { env?: Record<string, string> }> }
  const entry = cfg.mcpServers?.[server]
  if (!entry) throw new Error(`no server ${server} in the MCP config`)
  const out: Partial<Record<(typeof CONFIG_KEYS)[number], string>> = {}
  for (const key of CONFIG_KEYS) {
    const v = entry.env?.[key]
    if (v != null) out[key] = expand(v, env)
  }
  return out
}

interface Message { message_id?: string; from?: string; kind?: string; body?: string }

export interface DoorbellIO {
  deps: RuntimeDeps
  out: (line: string) => void
  now: () => number
  /** Waits between retries after a transient failure; tests pass a fake. */
  sleep?: (ms: number) => Promise<void>
}

/**
 * A failure worth waiting out: the platform restarting (every platform
 * release restarts it; 2026-11-14 the doorbell exited on "fetch failed" while
 * the PROD rollout replaced the api pod) or answering 5xx meanwhile. Anything
 * else (401, 403, 422) is the caller's to fix and ends the run.
 */
export function transient(err: unknown): boolean {
  const text = err instanceof Error ? `${err.message} ${String((err as { cause?: unknown }).cause ?? '')}` : String(err)
  return /fetch failed|ECONNREFUSED|ECONNRESET|ETIMEDOUT|EAI_AGAIN|socket hang up|other side closed|HTTP 50[0-4]\b/.test(text)
}

const RETRY_MS = 5000

/**
 * How long to wait between looks while only letters that were already waiting
 * at the start are unread (2026-11-19: a backlog made the doorbell exit at
 * once, every time it was started, so it woke nobody for anything new).
 */
const BACKLOG_MS = 30_000

/** One run: register, then wait until a message or the deadline. Returns the exit code. */
export async function ring(opts: { minutes: number; register: boolean }, io: DoorbellIO): Promise<number> {
  const { deps, out, now } = io
  if (deps.fellBack()) {
    throw new Error('no session id: set CLAUDE_CODE_HOST_SESSION_ID (or run inside a harness session)')
  }
  const sleep = io.sleep ?? ((ms: number) => new Promise<void>((r) => setTimeout(r, ms)))
  const end = now() + opts.minutes * 60_000
  let registered = !opts.register
  // The letters unread when the run began: the doorbell rings only for others.
  let known: Set<string> | undefined
  for (;;) {
    const left = Math.floor((end - now()) / 1000)
    if (left <= 0) {
      out(`doorbell: no message by the deadline (${new Date(now()).toISOString()})`)
      return 2
    }
    let got: { actor?: string; messages?: Message[]; error?: unknown }
    try {
      if (!registered) {
        const put = (await putArtifact({ kind: 'report', content: presenceBody(deps.session()), media_type: 'text/plain' }, deps)) as
          | { error?: unknown }
          | undefined
        if (put?.error) throw new Error(`register: ${JSON.stringify(put).slice(0, 300)}`)
        registered = true
      }
      // First a single read, so what is already waiting is known and does not ring.
      got = (await waitForMessages({ timeout_seconds: known === undefined ? 0 : Math.min(600, left) }, deps)) as typeof got
    } catch (err) {
      if (!transient(err)) throw err
      out(`doorbell: platform unreachable (${err instanceof Error ? err.message.slice(0, 120) : String(err)}); retrying`)
      await sleep(Math.min(RETRY_MS, Math.max(0, end - now())))
      continue
    }
    if (got.error) throw new Error(JSON.stringify(got).slice(0, 300))
    const all = got.messages ?? []
    if (known === undefined) {
      known = new Set(all.map((m) => m.message_id ?? ''))
      if (all.length > 0) out(`doorbell: ${all.length} letter(s) already waiting; ringing only for new ones`)
      continue
    }
    const msgs = all.filter((m) => !known!.has(m.message_id ?? ""))
    if (msgs.length === 0 && all.length > 0) {
      await sleep(Math.min(BACKLOG_MS, Math.max(0, end - now())))
      continue
    }
    if (msgs.length > 0) {
      out(`doorbell: ${msgs.length} message(s) for ${got.actor ?? 'this thread'} (${new Date(now()).toISOString()})`)
      for (const m of msgs) out(`  ${m.message_id} from=${m.from} kind=${m.kind} bytes=${Buffer.byteLength(m.body ?? '')}`)
      return 0
    }
  }
}

function flag(argv: string[], name: string): string | undefined {
  const i = argv.indexOf(`--${name}`)
  return i >= 0 ? argv[i + 1] : undefined
}

export async function main(argv: string[]): Promise<number> {
  const minutes = Number(flag(argv, 'minutes') ?? '60')
  if (!(minutes > 0)) throw new Error('--minutes is a positive number')
  if (!process.env.PLATFORM_API_URL) {
    const path = flag(argv, 'mcp-config') ?? '.mcp.json'
    const settings = settingsFromMcpConfig(readFileSync(path, 'utf8'), flag(argv, 'server') ?? 'bifrost-platform', process.env)
    for (const [k, v] of Object.entries(settings)) if (v) process.env[k] = v
  }
  return ring(
    { minutes, register: !argv.includes('--no-register') },
    { deps: defaultRuntimeDeps(), out: (l) => console.log(l), now: Date.now },
  )
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? '').href) {
  main(process.argv.slice(2)).then(
    (code) => process.exit(code),
    (err: unknown) => {
      console.error(`doorbell: ${err instanceof Error ? err.message : String(err)}`)
      process.exit(1)
    },
  )
}
