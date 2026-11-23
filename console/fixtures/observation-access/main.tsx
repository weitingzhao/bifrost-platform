import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { CardView } from '@bifrost/ui/city/CardView'
import { readingView, statusView, pathKey, readQuality, type ObservationSnapshot } from '@bifrost/ui/city/observation'
import { useObservationContext } from '@bifrost/ui/city/observationContext'
import { useHardwareObservation, useDomainStatusObservation } from '@/domains/foundation/observation-access'
import '@bifrost/ui/styles'

const client = new QueryClient()
function Views({latest}:{latest:ObservationSnapshot}) {
  const state=useObservationContext(latest)
  const key=pathKey(state.context.selected)
  return <main style={{padding:24,color:'#20283d',background:'#fff',minHeight:'100vh'}}><h1>Shared observation context</h1>
    <p>Live platform reads. Two existing projections, one selected object and observation time.</p>
    <button onClick={state.pinned?state.resume:state.pin}>{state.pinned?'Resume live':'Pin both views'}</button>
    <p data-testid="shared-snapshot">Snapshot: {state.snapshot.id}; incoming: {state.latest.id}; at: {state.context.at}</p>
    <aside aria-label="Shared evidence"><p>Selected: {key||'(root)'}</p><p>{readQuality(state.snapshot.evidence[key]??[],state.context.at)}</p>
      {(state.snapshot.evidence[key]??[]).map((r,i)=><pre key={i} style={{whiteSpace:'pre-wrap',overflowWrap:'anywhere'}}>{JSON.stringify(r,null,2)}</pre>)}
    </aside>
    {[readingView,statusView].map(view=><section key={view.id} aria-label={view.label}><h2>{view.label}</h2>
      {view.present(state.snapshot,state.context).map(panel=><div key={panel.id}><CardView body={panel.blocks} open={id=>state.select(id.split('/').filter(Boolean).map(decodeURIComponent))}/></div>)}
    </section>)}
  </main>
}
function Preview(){const read=useHardwareObservation();return read.data?<Views latest={read.data}/>:<p role="status">{read.error?String(read.error):'Reading platform telemetry…'}</p>}
function DomainRead({name}: {name:string}) {
  const read=useDomainStatusObservation()
  return <section aria-label={name}><h2>{name}</h2><p>{read.evidence?readQuality([read.evidence],new Date().toISOString()):'Reading domain status…'}</p>
    <pre style={{whiteSpace:'pre-wrap'}}>{JSON.stringify(read.evidence,null,2)}</pre>
    <p>Source commit: {read.data?.commit}; domains: {read.data?.domains.length}</p>
    <pre style={{whiteSpace:'pre-wrap'}}>{JSON.stringify(read.data?.domains,null,2)}</pre>
  </section>
}
createRoot(document.getElementById('root')!).render(<QueryClientProvider client={client}>{location.search==='?domains'?<main style={{padding:24}}><h1>Independent domain reads</h1><DomainRead name="Console consumer"/><DomainRead name="Signals consumer"/></main>:<Preview/>}</QueryClientProvider>)
