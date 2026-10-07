const TERMINAL = new Set(['executed', 'failed', 'rejected', 'expired'])

export interface PollRequestOptions {
  id: string
  timeoutMs: number
  intervalMs: number
  now: () => number
  sleep: (ms: number) => Promise<void>
  get: (id: string) => Promise<{ status?: string } & Record<string, unknown>>
}

/** Poll until a terminal approval status or the deadline. */
export async function pollRequest(opts: PollRequestOptions): Promise<Record<string, unknown>> {
  const deadline = opts.now() + opts.timeoutMs
  let last: Record<string, unknown> = { id: opts.id }
  for (;;) {
    const got = await opts.get(opts.id)
    last = got
    const status = typeof got.status === 'string' ? got.status : ''
    if (TERMINAL.has(status)) return got
    const remaining = deadline - opts.now()
    if (remaining <= 0) return { ...last, id: opts.id, wait: 'timeout' }
    await opts.sleep(Math.min(opts.intervalMs, remaining))
  }
}
