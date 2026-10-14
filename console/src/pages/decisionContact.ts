import { getServerContact, serverFetch } from '@/api/serverContact'
import { APPROVAL_WAITING_NOTICE } from '@/pages/approvalWaitingNotice'
import { CHECKING_CONNECTION, notSentNotice, sentNotice } from '@/pages/serverContactNotice'

export class ConnectionCheckError extends Error {}

/**
 * Only this GET has a deadline. /health is answered from memory and needs no
 * sign-in, so it stays fast when the cluster behind the platform is slow.
 * A response of any HTTP status proves contact.
 */
export async function probeServerContact(): Promise<void> {
  const controller = new AbortController()
  let timer: ReturnType<typeof setTimeout> | undefined
  try {
    await Promise.race([
      serverFetch('/health', { cache: 'no-store', signal: controller.signal }),
      new Promise<never>((_, reject) => {
        timer = setTimeout(() => {
          controller.abort()
          reject(new Error('Connection check expired'))
        }, 5000)
      }),
    ])
  } finally {
    clearTimeout(timer)
    controller.abort()
  }
}

/** No retries, and no signal is ever passed to the decision. */
export async function withDecisionContact<T>(send: () => Promise<T>, notice: (text: string | null) => void): Promise<T> {
  notice(null)
  const last = getServerContact()
  if (last == null || Date.now() - last > 10_000) {
    const startedAt = Date.now()
    notice(CHECKING_CONNECTION)
    try { await probeServerContact() } catch {
      // Another request was answered while the check was out: the server is reachable.
      const seen = getServerContact()
      if (seen == null || seen < startedAt) {
        const message = notSentNotice(seen)
        notice(message)
        throw new ConnectionCheckError(message)
      }
    }
    notice(null)
  }
  const sentAt = Date.now()
  const early = setTimeout(() => notice(sentNotice(sentAt)), 5000)
  const waiting = setTimeout(() => notice(APPROVAL_WAITING_NOTICE), 30_000)
  try { return await send() } finally {
    clearTimeout(early)
    clearTimeout(waiting)
    notice(null)
  }
}
