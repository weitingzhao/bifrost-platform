import { useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { AgentWorld, WorkingStudio, exchangesOf, type Member, type CommunicationFact, type Stage } from '@bifrost/ui/communication'
import '@fontsource-variable/geist'
const members: Member[] = [
  ['orion','Orion','Coordination','owner'],['lyra','Lyra','Coordination','temporary'],
  ['atlas','Atlas','Compute','owner'],['vega','Vega','Compute','temporary'],
  ['nova','Nova','Audit','owner'],['vale','Vale','Audit','temporary'],
].map(([id,label,domain,role])=>({id,label,memberships:[{seat:id,organization:'Platform',domain,role:role as 'owner'|'temporary'}]}))
const times=['22:42:00','22:42:04','22:42:08','22:42:12','22:42:16']
function Demo(){
  const [look,setLook]=useState(()=>window.location.hash.includes('console')?'console':'agents')
  const [selected,setSelected]=useState<string>()
  useEffect(()=>{const on=()=>setLook(window.location.hash.includes('console')?'console':'agents');window.addEventListener('hashchange',on);return()=>window.removeEventListener('hashchange',on)},[])
  const [step,setStep]=useState(0),[playing,setPlaying]=useState(false),[run,setRun]=useState(0)
  useEffect(()=>{if(!playing)return;const timer=setTimeout(()=>{setStep(s=>{if(s>=2)setPlaying(false);return Math.min(s+1,3)})},3200);return()=>clearTimeout(timer)},[playing,step])
  const stages:Stage[]=['sent',...(step>=1?['delivered' as const]:[]),...(step>=2?['acknowledged' as const]:[]),...(step===4?['completed' as const]:[])]
  const facts:CommunicationFact[]=stages.map((stage,i)=>({id:`demo-${run}-${stage}`,communication:'demo-m-014',channel:'message',stage,from:'orion',to:'atlas',label:'Prepare a build worker',evidence:{id:`demo-${run}-${stage}`,source:'Illustrative scenario',at:`2026-11-22T${times[stage==='completed'?4:i]}Z`}}))
  const exchanges=exchangesOf(facts)
  if(step===3)exchanges[0].issue={label:'Worker unavailable · this request is blocked',evidence:{id:'demo-blocked',source:'Illustrative scenario',at:'2026-11-22T22:42:12Z'}}
  const active=members.map(m=>({...m,activity:{label:m.id==='atlas'?(step===3?'Other work continues':step===4?'Result returned':'Preparing resources'):m.id==='orion'?'Working on integration':'Reviewing records',evidence:{id:'demo-work-'+m.id,source:'Illustrative scenario',at:'2026-11-22T22:42:00Z'}}}))
  const snapshot={version:1 as const,members:active,exchanges,coverage:{state:'ready' as const,updatedAt:`2026-11-22T${times[step]}Z`,note:'Illustrative scenario only. No platform requests. Work status, delivery and issue evidence are demonstration data.',truncated:false}}
  const controls=<><button onClick={()=>{setStep(0);setRun(r=>r+1);setPlaying(true)}}>{playing?'Restart':'Play sequence'}</button><button onClick={()=>setPlaying(p=>!p)} disabled={step>=3}>{playing?'Pause':'Resume'}</button><button onClick={()=>{setPlaying(false);setStep(4)}}>Show recovery</button><small>{['Request recorded · delivery unknown','Delivery recorded','ACK recorded','Request blocked · other work continues','Completion recorded'][step]}</small></>
  if(look==='console')return <main style={{fontFamily:'Arial,sans-serif',maxWidth:1400,margin:'0 auto',padding:24}}><header style={{display:'flex',justifyContent:'space-between'}}><h2>Communication records</h2><nav aria-label="Observation mode"><a href="#agents" style={button}>Agents</a> <a href="/#city" style={button}>City</a> <span>Console</span></nav></header><p>Illustrative scenario · same data, record view</p><WorkingStudio snapshot={snapshot} stationImage="/communication/station.png" ledgerOnly selectedId={selected} onSelected={setSelected}/></main>
  return <AgentWorld snapshot={snapshot} stationImage="/communication/station.png" cityHref="/#city" consoleHref="#console" selectedId={selected} onSelected={setSelected} notice="Design preview · illustrative data" controls={controls}/>

}
const button={background:'#31464c',color:'#e2e9e5',border:'1px solid #496067',borderRadius:8,padding:'10px 14px',cursor:'pointer'}
if(import.meta.env.DEV)createRoot(document.getElementById('root')!).render(<Demo />)
