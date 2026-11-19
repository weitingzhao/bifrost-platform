import { describe, expect, it } from 'vitest'
import { rulesBroken } from '../../frame/rules'
import type { Place } from '../../frame/places'
import { hardwareDistrict, hardwareRulesBroken, type HardwareReads } from '../hardware'
import fixture from './hardware.fixture.json'

// 2026-11-19, the Owner: the hardware district is the first domain to come in through the entry rules
// (RULES.md §2). The fixture is one real read of the platform's topology and monitoring (that day,
// 20:01Z, with the NAS's RAID arrays): 13 devices; gpu-server asleep on purpose; the two Windows TWS hosts and the MacBook with
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
  // 2026-11-19, the Owner, on the first try: tiny boxes huddled in the middle, the ground wasted. Sizes
  // grew so what a device shows reads at a glance, and the ground is laid out by what things are for.
  it('sizes by weight: the GPU tower and the NAS park 4×8; servers and the desks 4×4; the rest small', () => {
    expect(dev('gpu-server').shape).toBe('4x8')
    expect(dev('UGREEN-NAS').shape).toBe('4x8')
    expect(dev('ubt-k3s-07').shape).toBe('4x4')
    expect(dev('win11-host').shape).toBe('4x4')
    expect(dev('mac-mini-2').shape).toBe('2x2')
  })
  it('laid out in zones: the machine room west, trading and storage north-east, the office south-east', () => {
    const at = d.arrangement!.at
    for (const id of ['gpu-server', 'mini-pc-a', 'mini-pc-c', 'ubt-k3s-04', 'ubt-k3s-05', 'ubt-k3s-07']) expect(at[id].startsWith('a')).toBe(true)
    expect(at['win11-host'].startsWith('baa')).toBe(true)
    expect(at['UGREEN-NAS'].startsWith('bab')).toBe(true)
    for (const id of ['mac-mini-1', 'mac-mini-2', 'macbook', 'ubt-k3s-06']) expect(at[id].startsWith('bb')).toBe(true)
  })
  it('looks by kind', () => {
    expect([dev('win11-host').look, dev('macbook').look, dev('UGREEN-NAS').look, dev('ubt-k3s-05').look]).toEqual(['windows', 'mac', 'storage', 'linux'])
  })
  it('a computer’s rooms are CPU, Memory, then its disk; the NAS shows its volumes; the GPU box a GPU room', () => {
    expect(dev('ubt-k3s-04').children.map(r => r.id)).toEqual(['cpu', 'memory', 'disk'])
    expect(dev('UGREEN-NAS').children.map(r => r.id)).toEqual(['cpu', 'memory', 'volume-1', 'volume-2'])
    expect(dev('gpu-server').children.map(r => r.id)).toContain('gpu')
  })
  // 2026-11-19, the Owner: the NAS has physical disks, show them. Read from its RAID arrays (md1: three
  // disks, 24 TB; md2: two, 984 GB), matched to the volumes by size, not by the numbers in their names.
  it('a NAS volume carries the disks of its array and their states', () => {
    const vols = dev('UGREEN-NAS').children.filter(r => r.id.startsWith('volume-'))
    expect(vols.map(v => v.units)).toEqual([['online', 'online', 'online'], ['online', 'online']])
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
