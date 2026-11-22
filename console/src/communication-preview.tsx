import { useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { WorkingStudio, exchangesOf, type Member, type CommunicationFact, type Stage } from '@bifrost/ui/communication'
import '@fontsource-variable/geist'
const members: Member[] = [
  ['orion','Orion','Coordination','owner'],['lyra','Lyra','Coordination','temporary'],
  ['atlas','Atlas','Compute','owner'],['vega','Vega','Compute','temporary'],
  ['nova','Nova','Audit','owner'],['vale','Vale','Audit','temporary'],
].map(([id,label,domain,role])=>({id,label,memberships:[{seat:id,organization:'Platform',domain,role:role as 'owner'|'temporary'}]}))
const times=['22:42:00','22:42:04','22:42:08','22:42:12','22:42:16']
function Demo(){
  const [step,setStep]=useState(0),[playing,setPlaying]=useState(false),[run,setRun]=useState(0)
  useEffect(()=>{if(!playing)return;const timer=setTimeout(()=>{setStep(s=>{if(s>=2)setPlaying(false);return Math.min(s+1,3)})},3200);return()=>clearTimeout(timer)},[playing,step])
  const stages:Stage[]=['sent',...(step>=1?['delivered' as const]:[]),...(step>=2?['acknowledged' as const]:[]),...(step===4?['completed' as const]:[])]
  const facts:CommunicationFact[]=stages.map((stage,i)=>({id:`demo-${run}-${stage}`,communication:'demo-m-014',channel:'message',stage,from:'orion',to:'atlas',label:'Prepare a build worker',evidence:{id:`demo-${run}-${stage}`,source:'Illustrative scenario',at:`2026-11-22T${times[stage==='completed'?4:i]}Z`}}))
  const exchanges=exchangesOf(facts)
  if(step===3)exchanges[0].issue={label:'Worker unavailable · this request is blocked',evidence:{id:'demo-blocked',source:'Illustrative scenario',at:'2026-11-22T22:42:12Z'}}
  const active=members.map(m=>({...m,activity:{label:m.id==='atlas'?(step===3?'Other work continues':step===4?'Result returned':'Preparing resources'):m.id==='orion'?'Working on integration':'Reviewing records',evidence:{id:'demo-work-'+m.id,source:'Illustrative scenario',at:'2026-11-22T22:42:00Z'}}}))
  return <main style={{fontFamily:'Geist Variable, sans-serif',maxWidth:1440,margin:'0 auto',padding:'20px 0'}}>
    <header style={{padding:'0 20px',display:'flex',justifyContent:'space-between',gap:20,flexWrap:'wrap'}}><div><small style={{letterSpacing:3,color:'#a7bfc1'}}>BIFROST · DESIGN LAB</small><h1 style={{fontSize:28,margin:'10px 0'}}>A working world. A visible interruption.</h1><p style={{color:'#a6b4be',fontSize:13}}>Illustrative data only. No platform requests or agent actions.</p></div><div style={{alignSelf:'center',display:'flex',gap:8}}>
    <button onClick={()=>{setStep(0);setRun(r=>r+1);setPlaying(true)}} style={button}>{playing?'Restart sequence':'Play sequence'}</button>
    <button onClick={()=>setPlaying(p=>!p)} disabled={step>=3} style={button}>{playing?'Pause':'Resume'}</button>
    <button onClick={()=>{setPlaying(false);setStep(4)}} style={button}>Show recovery</button></div></header>
    <p role="status" style={{margin:'12px 20px 0',padding:'12px 16px',borderLeft:'3px solid '+(step===3?'#ebad77':'#799b9d'),background:step===3?'#392f28':'#20303a',fontSize:13}}>{['01 · Request recorded. Delivery is still unknown.','02 · Delivery evidence arrives. One parcel travels to the recipient.','03 · ACK recorded. This is not acceptance or completion.','04 · A source reports a blocked request. Other work continues.','05 · Recovery example: completion evidence is recorded.'][step]}</p>
    <WorkingStudio snapshot={{version:1,members:active,exchanges,coverage:{state:'ready',updatedAt:`2026-11-22T${times[step]}Z`,note:'Scenario playback. Work labels and the reported issue are illustrative evidence; production does not invent them. Motion accompanies new evidence, never an endless activity loop.',truncated:false}}} stationImage="/communication/station.png" />
  </main>
}
const button={background:'#31464c',color:'#e2e9e5',border:'1px solid #496067',borderRadius:8,padding:'10px 14px',cursor:'pointer'}
if(import.meta.env.DEV)createRoot(document.getElementById('root')!).render(<Demo />)
