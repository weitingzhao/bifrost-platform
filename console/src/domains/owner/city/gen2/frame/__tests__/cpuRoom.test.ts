import { describe, expect, it } from 'vitest'
import { rowsDrawn, setRuler } from '../library/dictionary'
import { cpuRoom, cpuRoomKey, cpuRoomShape, type CpuRoomReadings } from '../units/cpuRoom'

// 2026-11-19, the CPU unit's room (the workshop). On the first draw a Mac mini, which reports no clock,
// got empty speed dials and their key rows, and the asleep GPU box got a waiting line in its key with
// nothing waiting read: a row looked up is a row shown, so a part not read is not drawn or looked up.
const core = (busy: number | null, freq: number | null = null, max: number | null = null) => ({ busy, freq, max })
const linux: CpuRoomReadings = { cores: Array.from({ length: 16 }, () => core(0.2, 2e9, 4e9)), temps: [{ name: 'k10temp Tctl', c: 69 }], load: [0.6, 0.7, 1.7], pressure: 0.21 }
const mac: CpuRoomReadings = { cores: Array.from({ length: 10 }, () => core(0.1)), temps: [], load: [1.1, 1.3, 1.3], pressure: null }
const asleep: CpuRoomReadings = { cores: [], temps: [], load: [null, null, null], pressure: null }

describe('the CPU room', () => {
  it('a Linux node shows every part of the workshop', () => {
    expect(rowsDrawn(() => cpuRoom(linux)).ids.sort()).toEqual(['core.busy', 'core.freq', 'core.freq.max', 'core.machines', 'load', 'pressure', 'sensor.temp'])
  })
  it('a Mac mini: no clock read, no speed dials; no sensors, no thermometers; nothing waited read, nobody in line', () => {
    expect(rowsDrawn(() => cpuRoom(mac)).ids.sort()).toEqual(['core.busy', 'core.machines', 'load'])
  })
  it('a machine nothing reads: no machines, only the empty office', () => {
    const { out, ids } = rowsDrawn(() => cpuRoom(asleep))
    expect(ids).toEqual(['load'])
    expect(out.svg).toContain('CORES NOT READ')
  })
  it('more busy, more light: the picture changes with the reading', () => {
    const busier = { ...linux, cores: linux.cores.map(() => core(0.9, 2e9, 4e9)) }
    expect(cpuRoom(busier).svg).not.toBe(cpuRoom(linux).svg)
  })
  // The Owner, 2026-11-19: the ruler is adjustable; another ruler, another picture of the same machine.
  it('a ruler of two cores a machine draws half the machines, under another key; the readings are untouched', () => {
    const machines = (svg: string) => (svg.match(/fill="#5b5f6b"/g) ?? []).length
    const one = cpuRoom(linux), k1 = cpuRoomKey(linux)
    setRuler('core.machines', 2)
    try {
      expect(machines(cpuRoom(linux).svg)).toBe(machines(one.svg) / 2)
      expect(cpuRoomKey(linux)).not.toBe(k1)
      expect(linux.cores).toHaveLength(16)
    } finally { setRuler('core.machines', null) }
  })
  // The Owner, 2026-11-19: more cores, a bigger room; one design for every CPU, scaled out by its parts.
  it('a machine keeps its size; the room grows with the machines, by the allowed shapes', () => {
    // The first try had 8, 10 and 16 cores all in 32-cell rooms (4×8 and 8×4): the size said nothing.
    expect(cpuRoomShape(8)).toEqual([4, 4])
    expect(cpuRoomShape(10)).toEqual([4, 4])
    expect(cpuRoomShape(16)).toEqual([4, 8])
    expect(cpuRoomShape(32)).toEqual([8, 8])
    expect(cpuRoomShape(64)).toEqual([8, 8])
    const of = (n: number): CpuRoomReadings => ({ ...linux, cores: Array.from({ length: n }, () => core(0.2, 2e9, 4e9)) })
    expect(cpuRoom(of(32)).width).toBeGreaterThan(cpuRoom(of(8)).width)
  })
  it('more machines than the largest room holds: it says so, and the ruler makes them fit', () => {
    const big: CpuRoomReadings = { ...linux, cores: Array.from({ length: 128 }, () => core(0.2, 2e9, 4e9)) }
    expect(cpuRoom(big).svg).toMatch(/OF 128 MACHINES FIT/)
    setRuler('core.machines', 2)
    try { expect(cpuRoom(big).svg).not.toMatch(/MACHINES FIT/) } finally { setRuler('core.machines', null) }
  })
})
