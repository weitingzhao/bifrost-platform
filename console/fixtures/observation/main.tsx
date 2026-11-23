import { createRoot } from 'react-dom/client'
import { useEffect, useState } from 'react'
import { ObservationSurface, RuntimeWorld, anomalyLayer, type ObservationSnapshot } from '@bifrost/ui/city/observation'
import { useObservationContext } from '@bifrost/ui/city/observationContext'
import { captureReads, snapshotOf } from './read'
import { qualityFixture } from './qualityFixture'
import { readOperations } from '../../src/domains/foundation/runtime-observation/operations'
import { COVERAGE, DATABASES, GITOPS, WORKLOADS, databasePanels, gitopsPanel, readCoverage, readStates, workloadPanels } from '../../src/domains/foundation/runtime-observation/states'

/** Standalone read-only entry. No Console route or other team's transport is modified. */
export function Preview() {
  const [snapshot, setSnapshot] = useState<ObservationSnapshot>()
  const [error, setError] = useState('')
  useEffect(() => {
    if (new URLSearchParams(location.search).get('scenario') === 'village') {
      let active = true
      // Recorded captures of real platform reads (untracked, .observation-cache); a missing one reads as its error.
      const recorded: Record<string, string> = { '/api/v1/catalog/status': 'domain-status-recorded.json', [WORKLOADS]: 'bus-workloads-recorded.json', [DATABASES]: 'bus-databases-recorded.json', [COVERAGE]: 'coverage-recorded.json', [GITOPS]: 'gitops-recorded.json' }
      const get = async (path: string) => { const r = await fetch(recorded[path] ? '/observation-recorded/' + recorded[path] : path); if (!r.ok) throw new Error('HTTP ' + r.status); return r.json() }
      void Promise.all([captureReads(get).then(r=>snapshotOf(r,'recorded')),readOperations(get),readStates(get),readCoverage(get)]).then(([base,ops,states,coverage])=>{
        const evidence={...base.evidence,...ops.evidence,...states.evidence,...coverage.evidence}
        const layerPanels={...ops.panels,ci:[...ops.panels.ci,gitopsPanel(states.gitops,evidence)],data:[...ops.panels.data,...databasePanels(states.databases,evidence)],workloads:workloadPanels(states.workloads,coverage.readout,evidence)}
        if(active)setSnapshot({...base,capturedAt:new Date().toISOString(),evidence,layerPanels})}).catch(e=>{if(active)setError(String(e))})
      return ()=>{active=false}
    }
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
  if(snapshot&&new URLSearchParams(location.search).get('scenario') === 'controlled') return <ControlledPreview snapshot={snapshot}/>
  if(snapshot&&new URLSearchParams(location.search).get('scenario') === 'village') return <RuntimeWorld snapshot={snapshot}/>
  return <>{error && <p role="alert">Read failed: {error}</p>}{snapshot ? <ObservationSurface snapshot={snapshot} initialSelection={snapshot.provenance === 'fixture' ? [] : ['utilities']} layers={[anomalyLayer, { id: 'communication', label: 'Communication', unavailable: snapshot.layerPanels?.communication ? undefined : 'Live communication is not connected in this slice.', present: s => s.layerPanels?.communication ?? [] }]} /> : <p>Reading the existing platform telemetry contract…</p>}</>
}
function ControlledPreview({snapshot}: {snapshot: ObservationSnapshot}) {
  const observation = useObservationContext(snapshot)
  const [visible, show] = useState(true)
  return <>
    <div style={{position:'fixed',bottom:8,left:8,zIndex:1000,background:'#fff',color:'#111',padding:10}}>
      <button onClick={()=>show(v=>!v)}>Change test place</button>
      <span> Host: {observation.snapshot.id} · {observation.pinned?'Pinned':'Live'} · {observation.context.selected.join('/') || 'No selection'}</span>
    </div>
    {visible ? <RuntimeWorld observation={observation}/> : <main><h1>Other test place</h1><p>The same host retains the complete snapshot, selection and time.</p><p>{observation.context.at}</p><button onClick={observation.resume}>Resume host live</button></main>}
  </>
}
createRoot(document.getElementById('root')!).render(<Preview />)
