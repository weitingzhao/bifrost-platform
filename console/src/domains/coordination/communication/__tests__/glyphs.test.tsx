// 2026-11-23 World's gap table item 6: Agents' departments had no domain icon (0 of 2). They read the one table
// (DOMAIN_GLYPH), and a department the table cannot match is marked unknown, never given a guessed picture.
import { cleanup, render } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { AgentWorld } from '@bifrost/ui/communication'
import { DOMAIN_GLYPH } from '@/domains/owner/city/gen2/content/domainGlyphs'
afterEach(cleanup)
const seat=(domain:string|null)=>({seat:'s',organization:'Platform',domain,role:'owner' as const})
const snapshot={version:1 as const,coverage:{state:'ready' as const,note:'',truncated:false,updatedAt:'2026-11-23T12:00:00Z'},
 members:[{id:'a',label:'Alice',memberships:[seat('coordination')]},{id:'b',label:'Bob',memberships:[seat(null)]},{id:'c',label:'Cat',memberships:[]}],exchanges:[]}
const heading=(c:HTMLElement,text:string)=>[...c.querySelectorAll('.agent-directory h2')].find(h=>h.textContent?.includes(text))!

it('draws the domain glyph from the shared table and marks the departments it cannot match unknown', () => {
 const {container}=render(<AgentWorld snapshot={snapshot} stationImage="" cityHref="#city" consoleHref="#console" glyphOf={d=>DOMAIN_GLYPH[d]}/>)
 expect(heading(container,'coordination').querySelector('svg[aria-label="coordination domain"]')).not.toBeNull()
 expect(heading(container,'Outside domains').querySelector('.is-unknown')?.textContent).toBe('unknown')
 expect(heading(container,'Membership unresolved').querySelector('.is-unknown')).not.toBeNull()
})

it('draws no glyph or unknown mark when the host gives no table (control)', () => {
 const {container}=render(<AgentWorld snapshot={snapshot} stationImage="" cityHref="#city" consoleHref="#console"/>)
 expect(container.querySelector('.agent-domain-glyph')).toBeNull()
})
