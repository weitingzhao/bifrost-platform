import { useEffect, useState } from 'react'

const EVENT = 'bifrost-control-room-signoff-changed'

export function notifyControlRoomSignoffChanged(): void {
  window.dispatchEvent(new Event(EVENT))
}

/** Bump when any Control Room commander phase sign-off panel writes localStorage. */
export function useControlRoomSignoffRevision(): number {
  const [revision, setRevision] = useState(0)
  useEffect(() => {
    const onChange = () => setRevision(v => v + 1)
    window.addEventListener(EVENT, onChange)
    return () => window.removeEventListener(EVENT, onChange)
  }, [])
  return revision
}
