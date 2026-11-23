import { useEffect } from 'react'
import { useObservationContext } from '@bifrost/ui/city/observationContext'
import type { ObservationSnapshot } from '@bifrost/ui/city/observation'
import { publishSharedObservation } from './sharedObservationStore'
import { useRuntimeObservationSnapshot } from './useRuntimeObservationSnapshot'

/** Holds the context for one composite stream; mounted above the routes so a pin survives a change of place. */
function Hold({ snapshot }: { snapshot: ObservationSnapshot }) {
  const observation = useObservationContext(snapshot)
  useEffect(() => {
    publishSharedObservation({ state: 'ready', observation })
  }, [observation])
  return null
}

/** The readers, mounted once Signals has been opened and kept while the Console is open. Renders nothing. */
export default function SharedObservationHost() {
  const { snapshot, error } = useRuntimeObservationSnapshot()
  useEffect(() => {
    if (!snapshot) publishSharedObservation({ state: 'reading', error })
  }, [snapshot, error])
  useEffect(() => () => publishSharedObservation({ state: 'off' }), [])
  return snapshot ? <Hold snapshot={snapshot as ObservationSnapshot} /> : null
}
