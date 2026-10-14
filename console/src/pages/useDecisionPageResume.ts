import { useEffect, useRef } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { probeServerContact } from '@/pages/decisionContact'

export function useDecisionPageResume(): void {
  const client = useQueryClient()
  const inFlight = useRef(false)
  useEffect(() => {
    const check = () => {
      if (document.visibilityState !== 'visible' || inFlight.current) return
      inFlight.current = true
      void probeServerContact().catch(() => undefined).finally(() => { inFlight.current = false })
      void client.invalidateQueries({ queryKey: ['approvals'] })
      void client.invalidateQueries({ queryKey: ['questions'] })
    }
    document.addEventListener('visibilitychange', check)
    window.addEventListener('pageshow', check)
    return () => {
      document.removeEventListener('visibilitychange', check)
      window.removeEventListener('pageshow', check)
    }
  }, [client])
}
