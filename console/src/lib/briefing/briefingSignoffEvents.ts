import { useEffect, useState } from 'react'

const EVENT = 'bifrost-briefing-signoff-changed'

export function notifyBriefingSignoffChanged(): void {
  window.dispatchEvent(new Event(EVENT))
}

/** Bump when any phase sign-off panel writes localStorage — refreshes roadmap strip. */
export function useBriefingSignoffRevision(): number {
  const [revision, setRevision] = useState(0)
  useEffect(() => {
    const onChange = () => setRevision(v => v + 1)
    window.addEventListener(EVENT, onChange)
    return () => window.removeEventListener(EVENT, onChange)
  }, [])
  return revision
}
