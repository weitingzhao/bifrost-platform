import { createRoot } from 'react-dom/client'
import { useEffect, useState } from 'react'
import { ObservationSurface, anomalyLayer, type ObservationSnapshot } from '@bifrost/ui/city/observation'
import { captureReads, snapshotOf } from './read'
import { qualityFixture } from './qualityFixture'

/** Standalone read-only entry. No Console route or other team's transport is modified. */
export function Preview() {
  const [snapshot, setSnapshot] = useState<ObservationSnapshot>()
  const [error, setError] = useState('')
  useEffect(() => {
    if (new URLSearchParams(location.search).get('scenario') === 'quality') { setSnapshot(qualityFixture()); return }
    if (new URLSearchParams(location.search).get('scenario') === 'communication') {
      let active = true
      void fetch('/observation/communication-probe.json').then(r => { if (!r.ok) throw new Error('Generate the communication contract probe first'); return r.json() }).then(s => { if (active) setSnapshot(s) }).catch(e => { if (active) setError(String(e)) })
      return () => { active = false }
    }
    let cancelled = false
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout>
    const read = async () => {
      try {
        const raw = await captureReads(async path => {
          const response = await fetch(path, { signal: controller.signal })
          if (!response.ok) throw new Error(`HTTP ${response.status}: ${path}`)
          return response.json()
        })
        const next = await snapshotOf(raw, 'live')
        if (!cancelled) { setSnapshot(next); setError('') }
      } catch (e) { if (!cancelled) setError(String(e)) }
      finally { if (!cancelled) timer = setTimeout(read, 30000) }
    }
    void read()
    return () => { cancelled = true; controller.abort(); clearTimeout(timer) }
  }, [])
  return <>{error && <p role="alert">Read failed: {error}</p>}{snapshot ? <ObservationSurface snapshot={snapshot} initialSelection={snapshot.provenance === 'fixture' ? [] : ['utilities']} layers={[anomalyLayer, { id: 'communication', label: 'Communication', unavailable: snapshot.layerPanels?.communication ? undefined : 'Live communication is not connected in this slice.', present: s => s.layerPanels?.communication ?? [] }]} /> : <p>Reading the existing platform telemetry contract…</p>}</>
}
createRoot(document.getElementById('root')!).render(<Preview />)
