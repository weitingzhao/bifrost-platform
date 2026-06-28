import { useCallback, useEffect, useRef, useState } from 'react'
import type { RemediationEvent, RemediationJob, RemediationPhase } from '@/api/types'
import { remediationStreamUrl } from '@/api/platform'
import { getPlatformOperatorToken } from '@/lib/platformAuth'

const KNOWN_PHASES = new Set<string>([
  'starting',
  'diagnosing',
  'awaiting_approval',
  'remediating',
  'verifying',
  'done',
  'failed',
  'cancelled',
])

interface StreamEnvelope {
  type: 'job' | 'event' | 'error'
  job?: RemediationJob
  event?: RemediationEvent
  text?: string
}

export interface UseRemediationStreamResult {
  job: RemediationJob | null
  events: RemediationEvent[]
  connected: boolean
  error: string | null
  stop: () => void
}

function parseSseChunk(buffer: string, onData: (payload: string) => void): string {
  const parts = buffer.split('\n\n')
  const rest = parts.pop() ?? ''
  for (const part of parts) {
    for (const line of part.split('\n')) {
      if (line.startsWith('data: ')) {
        onData(line.slice(6))
      }
    }
  }
  return rest
}

export function useRemediationStream(jobId: string | null): UseRemediationStreamResult {
  const [job, setJob] = useState<RemediationJob | null>(null)
  const [events, setEvents] = useState<RemediationEvent[]>([])
  const [connected, setConnected] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const abortRef = useRef<AbortController | null>(null)

  const stop = useCallback(() => {
    abortRef.current?.abort()
    abortRef.current = null
    setConnected(false)
  }, [])

  useEffect(() => {
    if (jobId == null || jobId === '') {
      stop()
      setJob(null)
      setEvents([])
      setError(null)
      return
    }

    setJob(null)
    setEvents([])
    setError(null)

    const controller = new AbortController()
    abortRef.current = controller

    const token = getPlatformOperatorToken()
    const headers: HeadersInit = { Accept: 'text/event-stream' }
    if (token !== '') headers.Authorization = `Bearer ${token}`

    void (async () => {
      try {
        const r = await fetch(remediationStreamUrl(jobId), {
          headers,
          signal: controller.signal,
        })
        if (!r.ok) {
          let detail = `HTTP ${r.status}`
          try {
            const body = (await r.json()) as { error?: string }
            detail = body.error ?? detail
          } catch {
            // keep status
          }
          setError(detail)
          return
        }
        if (r.body == null) {
          setError('Stream body unavailable')
          return
        }
        setConnected(true)
        const reader = r.body.getReader()
        const decoder = new TextDecoder()
        let buffer = ''
        while (!controller.signal.aborted) {
          const { done, value } = await reader.read()
          if (done) break
          buffer += decoder.decode(value, { stream: true })
          buffer = parseSseChunk(buffer, raw => {
            try {
              const payload = JSON.parse(raw) as StreamEnvelope
              if (payload.type === 'job' && payload.job != null) {
                setJob(payload.job)
                if (payload.job.events != null && payload.job.events.length > events.length) {
                  setEvents(payload.job.events)
                }
              } else if (payload.type === 'event' && payload.event != null) {
                setEvents(prev => {
                  if (prev.some(e => e.id === payload.event!.id)) return prev
                  return [...prev, payload.event!]
                })
                if (
                  payload.event.type === 'status' &&
                  typeof payload.event.meta?.phase === 'string' &&
                  KNOWN_PHASES.has(payload.event.meta.phase)
                ) {
                  const phase = payload.event.meta.phase as RemediationPhase
                  setJob(prev =>
                    prev != null && prev.phase !== phase ? { ...prev, phase, updated_at: payload.event!.at } : prev,
                  )
                }
              } else if (payload.type === 'error' && payload.text != null) {
                setError(payload.text)
              }
            } catch {
              // ignore malformed frames
            }
          })
        }
      } catch (err) {
        if (controller.signal.aborted) return
        setError(err instanceof Error ? err.message : 'Stream failed')
      } finally {
        setConnected(false)
      }
    })()

    return () => {
      controller.abort()
      abortRef.current = null
      setConnected(false)
    }
  }, [jobId, stop])

  return { job, events, connected, error, stop }
}
