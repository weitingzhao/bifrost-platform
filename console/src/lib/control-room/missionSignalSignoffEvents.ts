import { useEffect, useState } from 'react'

const EVENT = 'bifrost-mission-signal-signoff-changed'

export function notifyMissionSignalSignoffChanged(): void {
  window.dispatchEvent(new Event(EVENT))
}

/** Bump when any Mission Signal phase sign-off panel writes localStorage. */
export function useMissionSignalSignoffRevision(): number {
  const [revision, setRevision] = useState(0)
  useEffect(() => {
    const onChange = () => setRevision(v => v + 1)
    window.addEventListener(EVENT, onChange)
    return () => window.removeEventListener(EVENT, onChange)
  }, [])
  return revision
}
