import { useEffect, useState } from 'react'

const EVENT = 'bifrost-governance-signoff-changed'

export function notifyGovernanceSignoffChanged(): void {
  window.dispatchEvent(new Event(EVENT))
}

/** Bump when any governance phase sign-off panel writes localStorage. */
export function useGovernanceSignoffRevision(): number {
  const [revision, setRevision] = useState(0)
  useEffect(() => {
    const onChange = () => setRevision(v => v + 1)
    window.addEventListener(EVENT, onChange)
    return () => window.removeEventListener(EVENT, onChange)
  }, [])
  return revision
}
