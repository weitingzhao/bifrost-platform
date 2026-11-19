import { describe, expect, it } from 'vitest'
import { rowsDrawn, setRuler } from '../library/dictionary'
import { COL_GAP_PX, cpuRoom, cpuRoomKey, cpuRoomLayout, cpuRoomShape, LATHE, ROW_GAP_PX, type CpuRoomReadings } from '../units/cpuRoom'

// 2026-11-19, the CPU unit's room (the workshop). CPU only: load and waiting were drawn first and taken
// out the same day — the scheduler counts them, the CPU as hardware does not know them (the Owner). On the first draw a Mac mini, which reports no clock,
// got empty speed dials and their key rows, and the asleep GPU box got a waiting line in its key with
// nothing waiting read: a row looked up is a row shown, so a part not read is not drawn or looked up.
const core = (busy: number | null, freq: number | null = null, max: number | null = null) => ({ busy, freq, max })
const linux: CpuRoomReadings = { cores: Array.from({ length: 16 }, () => core(0.2, 2e9, 4e9)), temps: [{ name: 'Tctl', c: 69 }] }
const mac: CpuRoomReadings = { cores: Array.from({ length: 10 }, () => core(0.1)), temps: [] }
const asleep: CpuRoomReadings = { cores: [], temps: [] }

describe('the CPU room', () => {
  it('a Linux node shows every part of the workshop', () => {
    expect(rowsDrawn(() => cpuRoom(linux)).ids.sort()).toEqual(['core.busy', 'core.busy.all', 'core.freq', 'core.freq.max', 'core.machines', 'sensor.temp'])
  })
  it('a Mac mini: no clock read, no CLOCK; no sensor, no CHIP TEMP', () => {
    expect(rowsDrawn(() => cpuRoom(mac)).ids.sort()).toEqual(['core.busy', 'core.busy.all', 'core.machines'])
  })
  it('a machine nothing reads: no machines, no console', () => {
    const { out, ids } = rowsDrawn(() => cpuRoom(asleep))
    expect(ids).toEqual([])
    expect(out.svg).toContain('CORES NOT READ')
  })
  it('more busy, more light: the picture changes with the reading', () => {
    const busier = { ...linux, cores: linux.cores.map(() => core(0.9, 2e9, 4e9)) }
    expect(cpuRoom(busier).svg).not.toBe(cpuRoom(linux).svg)
  })
  // The Owner, 2026-11-19: the ruler is adjustable; another ruler, another picture of the same machine.
  it('a ruler of two cores a machine draws half the machines, under another key; the readings are untouched', () => {
    const machines = (svg: string) => (svg.match(/fill="#94b3a1"/g) ?? []).length
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
    expect(cpuRoomShape(10)).toEqual([4, 8])
    expect(cpuRoomShape(16)).toEqual([4, 8])
    expect(cpuRoomShape(32)).toEqual([8, 8])
    expect(cpuRoomShape(48)).toEqual([8, 8])
    const of = (n: number): CpuRoomReadings => ({ ...linux, cores: Array.from({ length: n }, () => core(0.2, 2e9, 4e9)) })
    expect(cpuRoom(of(32)).width).toBeGreaterThan(cpuRoom(of(8)).width)
  })
  it('more machines than the largest room holds: it says so, and the ruler makes them fit', () => {
    const big: CpuRoomReadings = { ...linux, cores: Array.from({ length: 64 }, () => core(0.2, 2e9, 4e9)) }
    expect(cpuRoom(big).svg).toMatch(/OF 64 MACHINES FIT/)
    setRuler('core.machines', 2)
    try { expect(cpuRoom(big).svg).not.toMatch(/MACHINES FIT/) } finally { setRuler('core.machines', null) }
  })
  // The Owner, 2026-11-19: cores come in pairs and powers of two; three to a row, one left over, looks wrong.
  it('machines stand in pairs: 2, 4 or 8 to a row, every row full for the cores machines have', () => {
    for (const n of [2, 4, 8, 10, 16, 32, 48]) {
      const { cols, rows } = cpuRoomLayout(n)
      expect([2, 4, 8]).toContain(cols)
      expect(rows * cols).toBe(n)
    }
  })
  // The Owner, 2026-11-19: each reading drawn where it is measured, or it is a concept drawing. Every
  // machine that day reported one CPU sensor for the whole chip (AMD Tctl); a per-core sensor (Intel's
  // "Core N") stands on its own machine instead.
  it('a whole-chip temperature hangs once on the wall; a per-core one stands on its machine', () => {
    const whole = cpuRoom(linux).svg
    expect(whole).toContain('69°C')
    const perCore: CpuRoomReadings = { ...linux, temps: linux.cores.map((_, k) => ({ name: `Core ${k}`, c: 50 + k, core: k })) }
    const svg = cpuRoom(perCore).svg
    expect(svg).not.toContain('CHIP TEMP')
    expect(svg).not.toBe(whole)
  })
  // The Owner, 2026-11-19: every colour stands for something. Machines were grey when idle and green
  // otherwise — the work light's reading said a second time, and not in the key.
  it('a machine is one colour whatever its core does; only its lit folds show how busy', () => {
    const idle = cpuRoom({ ...linux, cores: linux.cores.map(() => core(0, 2e9, 4e9)) }).svg
    const busy = cpuRoom({ ...linux, cores: linux.cores.map(() => core(0.5, 2e9, 4e9)) }).svg
    const bodies = (svg: string) => (svg.match(/fill="#94b3a1"/g) ?? []).length
    expect(bodies(idle)).toBe(16)
    expect(bodies(busy)).toBe(16)
    expect(idle).not.toContain('#adb3ad')
  })
  // The Owner, 2026-11-19: the tall work light hid the machines behind it; the load lies on the top.
  it('the folds light with the core: none when idle, orange above 85%', () => {
    const at = (b: number) => cpuRoom({ ...linux, cores: linux.cores.map(() => core(b, 2e9, 4e9)) }).svg
    expect(at(0)).not.toContain('#ffc94d')
    expect(at(0.5)).toContain('#ffc94d')
    expect(at(0.95)).toContain('#ff7a45')
  })
  // The Owner, 2026-11-19: the whole CPU's percent belongs on the console; what repeated the load bar was
  // the half dial on each machine. Each instrument the standard one, in its slot, inside the desk.
  it('a machine carries its load bar only; the console the whole CPU, one standard instrument each', () => {
    const svg = cpuRoom(linux).svg
    expect(svg).toContain('BUSY')
    expect(svg).toContain('CLOCK')
    expect(svg).toContain('2.0 GHz')
    expect(svg).toContain('20%')
    expect(cpuRoom(asleep).svg).not.toMatch(/CPU CONSOLE/)
  })
  // The Owner, 2026-11-19, three bounds: it looks like a machine tool; no machine hides another (2.5D);
  // the load reads at a glance. The tall headstock hid the bar of the machine behind it.
  // The first low lathe kept the rows clear but a pair's two lathes stood 0.08 cells apart: the right
  // one's headstock covered the left one's tailstock. The same bound holds side to side.
  it('no part of a lathe rises above its bed further than the gap to the row behind, or to its neighbour', () => {
    expect(Math.max(LATHE.head, LATHE.tail)).toBeLessThanOrEqual(ROW_GAP_PX)
    expect(Math.max(LATHE.head, LATHE.tail)).toBeLessThanOrEqual(COL_GAP_PX)
  })
  it('the load lights the folds: none idle, about half at 50%, all at 100% in orange', () => {
    const at = (b: number) => cpuRoom({ ...linux, cores: [core(b, 2e9, 4e9), core(b, 2e9, 4e9)] }).svg
    const amber = (svg: string) => (svg.match(/fill="#ffc94d"/g) ?? []).length
    expect(amber(at(0))).toBe(0)
    expect(amber(at(0.5))).toBe(2 * 2)
    expect((at(1).match(/fill="#ff7a45"/g) ?? []).length).toBe(2 * 4)
  })
})
