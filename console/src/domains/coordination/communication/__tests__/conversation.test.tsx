// 事故 2026-11-24, on a phone: coming from Signals pinned at 21:12, Agents headlined
// "Requested communication message:m_01M85… is not in the loaded evidence", drew one person and no line, and its
// timeline cards led with "Inbox delivery" / "Sent". It could not say who was talking to whom (箭10 asked for the fix).
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { AgentWorld, exchangesOf, type CommunicationFact } from '@bifrost/ui/communication'
afterEach(cleanup)
const ev=(id:string,at:string)=>({id,source:'agentrt/events',at})
const facts: CommunicationFact[] = [
  { id:'s1', communication:'m_early', channel:'message', stage:'sent', from:'thread:a', to:'thread:b', label:'note', evidence:ev('1','2026-11-23T21:00:00Z') },
  { id:'s2', communication:'m_late', channel:'message', stage:'sent', from:'thread:a', to:'thread:b', label:'ask', evidence:ev('2','2026-11-23T21:40:00Z') },
  { id:'s3', communication:'m_nobody', channel:'message', stage:'sent', from:'thread:a', label:'note', evidence:ev('3','2026-11-23T21:05:00Z') },
]
const snapshot={version:1 as const,coverage:{state:'ready' as const,note:'',truncated:false,updatedAt:'2026-11-23T22:00:00Z'},
  members:[{id:'thread:a',label:'Alice',memberships:[]},{id:'thread:b',label:'Bob',memberships:[]}],exchanges:exchangesOf(facts)}
const world=(props:Partial<Parameters<typeof AgentWorld>[0]>)=>render(<AgentWorld snapshot={snapshot} stationImage="" cityHref="#city" consoleHref="#console" {...props}/>)

it('says in plain words that the requested letter comes after the pinned time, and jumps there', () => {
  const onThrough=vi.fn()
  const {container}=world({selectedId:'message:m_late',through:'2026-11-23T21:12:00Z',onThrough})
  expect(screen.getByRole('status').textContent).toBe('This letter was sent after the time you are looking at')
  // No internal id as the headline.
  expect(container.querySelector('.agent-world-caption')?.textContent).not.toContain('m_late')
  // Both parties are drawn even though the letter is not at this time yet.
  expect([...container.querySelectorAll('.agent-city-station strong')].map(e=>e.textContent)).toEqual(['Alice','Bob'])
  expect(container.querySelector('.agent-scene-links')).not.toBeNull()
  fireEvent.click(screen.getByText("Jump to this letter's time"))
  expect(onThrough).toHaveBeenCalledWith('2026-11-23T21:40:00Z')
})

it('draws an unnamed recipient as an unknown seat instead of leaving one person alone', () => {
  const {container}=world({selectedId:'message:m_nobody'})
  const seats=[...container.querySelectorAll('.agent-city-station')]
  expect(seats.map(e=>e.querySelector('strong')?.textContent)).toEqual(['Alice','Unknown recipient'])
  expect(seats[1].classList.contains('is-unknown')).toBe(true)
})

it('leads each timeline card with who wrote to whom, the delivery stage in small print', () => {
  const {container}=world({})
  const card=[...container.querySelectorAll('.agent-time-events button')].find(b=>b.querySelector('span')?.textContent==='ask')!
  expect(card.querySelector('strong')?.textContent).toBe('Alice → Bob')
  expect(card.querySelector('small')?.textContent).toBe('Sent')
})

it('says a letter older than the loaded history is out of reach, never with its id (control: no request, no message)', () => {
  const {container}=world({selectedId:'message:m_gone'})
  expect(screen.getByRole('status').textContent).toBe('This letter is older than the history loaded here')
  expect(container.querySelector('.agent-world-caption')?.textContent).not.toContain('m_gone')
  cleanup()
  world({})
  expect(screen.queryByText(/older than the history/)).toBeNull()
})

// 2026-11-24: with the platform's letter summaries, a card says what the letter says (control: no summary → its kind).
it('puts the letter’s own line on its card when the source has one', () => {
  const withSummary={...snapshot,exchanges:exchangesOf([...facts,{ id:'s4', communication:'m_said', channel:'message', stage:'sent', from:'thread:b', to:'thread:a', label:'ask', summary:'Who owns counter.windows?', evidence:ev('4','2026-11-23T21:50:00Z') }])}
  const {container}=render(<AgentWorld snapshot={withSummary} stationImage="" cityHref="#city" consoleHref="#console"/>)
  const spans=[...container.querySelectorAll('.agent-time-events button span')].map(s=>s.textContent)
  expect(spans).toContain('Who owns counter.windows?')
  expect(spans).toContain('ask')
})
