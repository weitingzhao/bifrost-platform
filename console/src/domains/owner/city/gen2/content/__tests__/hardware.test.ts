import { describe, expect, it } from 'vitest'
import { rulesBroken } from '../../frame/rules'
import type { Place } from '../../frame/places'
import { hardwareDistrict, hardwareRulesBroken, type HardwareReads } from '../hardware'
import fixture from './hardware.fixture.json'

// 2026-11-19, the Owner: the hardware district is the first domain to come in through the entry rules
// (RULES.md §2). The fixture is one real read of the platform's topology and monitoring (that day,
// 19:27Z): 13 devices; gpu-server asleep on purpose; the two Windows TWS hosts and the MacBook with
// nothing reading them. Before this, the plan said every machine outside the cluster was unread;
// the read showed the Mac minis, ubt-k3s-06 and the NAS report through node exporters.
const reads = fixture as unknown as HardwareReads
const d = hardwareDistrict(reads)
const dev = (id: string) => d.children.find(b => b.id === id)!

describe('the hardware district, from one real read', () => {
  it('one building per device the topology lists, and nothing else', () => {
    expect(d.children.map(b => b.id).sort()).toEqual(reads.topology!.map(n => n.id).sort())
  })
  it('states come from the readings: ready, asleep on purpose, nothing reading it', () => {
    expect(dev('mini-pc-a').state).toBe('online')
    expect(dev('gpu-server').state).toBe('standby')
    expect(dev('win11-host').state).toBe('unread')
    expect(dev('mac-mini-1').state).toBe('online')
    expect(dev('UGREEN-NAS').state).toBe('online')
  })
  it('sizes by weight: the GPU box largest, 16-core servers and the NAS long, the rest small', () => {
    expect(dev('gpu-server').shape).toBe('4x4')
    expect(dev('ubt-k3s-07').shape).toBe('2x4')
    expect(dev('UGREEN-NAS').shape).toBe('2x4')
    expect(dev('mac-mini-2').shape).toBe('2x2')
  })
  it('looks by kind', () => {
    expect([dev('win11-host').look, dev('macbook').look, dev('UGREEN-NAS').look, dev('ubt-k3s-05').look]).toEqual(['windows', 'mac', 'storage', 'linux'])
  })
  it('a computer’s rooms are CPU, Memory, then its disk; the NAS shows its volumes; the GPU box a GPU room', () => {
    expect(dev('ubt-k3s-04').children.map(r => r.id)).toEqual(['cpu', 'memory', 'disk'])
    expect(dev('UGREEN-NAS').children.map(r => r.id)).toEqual(['cpu', 'memory', 'volume-1', 'volume-2'])
    expect(dev('gpu-server').children.map(r => r.id)).toContain('gpu')
  })
  it('unread is not zero: nothing reading it means no number; asleep shows its size but no use', () => {
    for (const r of dev('win11-host').children) expect([r.meter?.used, r.meter?.of]).toEqual([null, null])
    const cpu = dev('gpu-server').children.find(r => r.id === 'cpu')!
    expect(cpu.meter?.used).toBeNull()
    expect(cpu.meter?.of).toBe(32)
  })
  it('a failed read shows unread, never zero', () => {
    const broken = hardwareDistrict({ ...reads, prom: { ...reads.prom, busy: null } })
    expect(broken.children.find(b => b.id === 'ubt-k3s-04')!.children[0].meter?.used).toBeNull()
  })
  it('keeps the frame’s rules and its own', () => {
    expect(hardwareRulesBroken(d)).toEqual([])
    const town: Place = { id: 'town', kind: 'town', shape: '8x8', label: 'City', question: '', dashboard: null, children: [d] }
    expect(rulesBroken({ tree: town, functions: [{ id: 'system' }] })).toEqual([])
  })
  it('its own rules catch a computer without its rooms in order', () => {
    const bad = { ...d, children: [{ ...dev('ubt-k3s-04'), children: [...dev('ubt-k3s-04').children].reverse() }] }
    expect(hardwareRulesBroken(bad).join()).toMatch(/CPU, Memory, then its disk/)
  })
})
