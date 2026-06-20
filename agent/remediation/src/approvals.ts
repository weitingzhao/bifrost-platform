export interface OperatorResponse {
  option_id: string
  note?: string
}

interface PendingWait {
  resolve: (value: OperatorResponse) => void
  reject: (err: Error) => void
  timeout: ReturnType<typeof setTimeout>
}

const pending = new Map<string, PendingWait>()

const DEFAULT_TIMEOUT_MS = 30 * 60 * 1000

export function waitForOperatorResponse(
  jobId: string,
  timeoutMs = DEFAULT_TIMEOUT_MS,
): Promise<OperatorResponse> {
  return new Promise((resolve, reject) => {
    const existing = pending.get(jobId)
    if (existing != null) {
      clearTimeout(existing.timeout)
      existing.reject(new Error('superseded by new approval request'))
    }
    const timeout = setTimeout(() => {
      pending.delete(jobId)
      reject(new Error('Operator response timed out'))
    }, timeoutMs)
    pending.set(jobId, { resolve, reject, timeout })
  })
}

export function submitOperatorResponse(jobId: string, response: OperatorResponse): boolean {
  const wait = pending.get(jobId)
  if (wait == null) return false
  clearTimeout(wait.timeout)
  pending.delete(jobId)
  wait.resolve(response)
  return true
}

export function hasPendingOperatorResponse(jobId: string): boolean {
  return pending.has(jobId)
}

export function clearPendingOperatorResponse(jobId: string): void {
  const wait = pending.get(jobId)
  if (wait == null) return
  clearTimeout(wait.timeout)
  pending.delete(jobId)
  wait.reject(new Error('approval cleared'))
}
