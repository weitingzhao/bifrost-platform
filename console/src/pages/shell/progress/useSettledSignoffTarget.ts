import { useEffect, useRef } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { hashQueryParam } from '@/lib/shell/consoleRoutes'
import { canonicalQuery, hashNamesSection } from '@/lib/shell/useHashSection'

function targetsSignoff(): boolean {
  const goal = hashQueryParam(canonicalQuery(window.location.hash), 'goal')
  return !/^M-\d+$/.test(goal ?? '') && hashNamesSection(window.location.hash, 'signoff')
}

/** Finish a sign-off deep link after the goal cards above it have loaded, once per navigation. */
export function useSettledSignoffTarget(goalIds: readonly string[], ready: boolean) {
  const client = useQueryClient()
  const completed = useRef(false)
  const ids = JSON.stringify(goalIds)
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined
    const attempt = () => {
      clearTimeout(timer)
      if (!ready || completed.current || !targetsSignoff()) return
      const goals = JSON.parse(ids) as string[]
      if (goals.some(id => {
        const state = client.getQueryState(['agent-runtime-goal', id])
        return state == null || (state.status === 'pending' && state.fetchStatus !== 'paused') || state.fetchStatus === 'fetching'
      })) return
      // Allow query observers to commit their expanded cards before measuring the target.
      timer = setTimeout(() => {
        if (completed.current || !targetsSignoff()) return
        const target = document.getElementById('signoff')
        if (target == null) return
        target.scrollIntoView?.({ block: 'start' })
        completed.current = true
      }, 50)
    }
    const navigate = () => { completed.current = false; attempt() }
    const cancelForUser = () => { completed.current = true; clearTimeout(timer) }
    const keyScroll = (event: KeyboardEvent) => {
      if (['ArrowUp', 'ArrowDown', 'PageUp', 'PageDown', 'Home', 'End', ' '].includes(event.key)) cancelForUser()
    }
    window.addEventListener('wheel', cancelForUser, { passive: true })
    window.addEventListener('touchmove', cancelForUser, { passive: true })
    window.addEventListener('keydown', keyScroll)
    const unsubscribe = client.getQueryCache().subscribe(attempt)
    window.addEventListener('hashchange', navigate)
    attempt()
    return () => {
      clearTimeout(timer); unsubscribe(); window.removeEventListener('hashchange', navigate)
      window.removeEventListener('wheel', cancelForUser)
      window.removeEventListener('touchmove', cancelForUser)
      window.removeEventListener('keydown', keyScroll)
    }
  }, [client, ids, ready])
}
