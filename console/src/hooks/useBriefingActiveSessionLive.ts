import { useEffect, useState } from 'react'
import {
  loadBriefingActiveSession,
  type BriefingActiveSession,
} from '@/lib/briefing/briefingActiveSession'

/** Reactive Active Session — updates on storage / focus / same-tab save events. */
export function useBriefingActiveSessionLive(): BriefingActiveSession | null {
  const [session, setSession] = useState<BriefingActiveSession | null>(() =>
    loadBriefingActiveSession(),
  )

  useEffect(() => {
    const refresh = () => setSession(loadBriefingActiveSession())
    refresh()
    window.addEventListener('storage', refresh)
    window.addEventListener('focus', refresh)
    window.addEventListener('bifrost-briefing-active-session', refresh)
    return () => {
      window.removeEventListener('storage', refresh)
      window.removeEventListener('focus', refresh)
      window.removeEventListener('bifrost-briefing-active-session', refresh)
    }
  }, [])

  return session
}
