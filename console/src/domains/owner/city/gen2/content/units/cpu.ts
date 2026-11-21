import type { UnitSpec } from '@bifrost/ui/city/units/process'
import CONFIRMED from './confirmed.json'
import { MEMORY_UNIT } from './memory'
import { DISK_UNIT } from './disk'

/**
 * The CPU unit (the first through the unit process, 2026-11-19). City metaphor (the Owner, 2026-11-19): the workshop of a
 * factory — a computer is a factory, its CPU the machine tools, memory the workbench, disk the warehouse. Details: what node exporter reports about a machine's CPU, listed after a read of
 * every metric family it has (2026-11-19 20:40Z): the Linux nodes report all of these; the Mac minis
 * only the time per core and the load; the NAS has no thermal zone.
 */
const CORES = 'count by (instance) (count by (instance, cpu) (node_cpu_seconds_total))'

export const CPU_UNIT: UnitSpec = {
  id: 'cpu',
  name: 'CPU',
  what: 'The processor of a machine: its cores and how hard they work. In the city, the workshop of a factory.',
  details: [
    { id: 'cores', label: 'Cores', meaning: 'How many cores it has.', query: CORES, unit: 'count', per: 'unit', known: 'hardware' },
    { id: 'busy.core', label: 'Busy, each core', meaning: 'How much of the last 5 minutes each core was not idle.', query: '1 - avg by (instance, cpu) (rate(node_cpu_seconds_total{mode="idle"}[5m]))', unit: 'share', per: 'core', known: 'hardware' },
    { id: 'busy.mode', label: 'Busy, by what', meaning: 'What the time went to, as a share of all cores: user programs, the system, waiting on disk (iowait), interrupts, taken by the host (steal).', query: `sum by (instance, mode) (rate(node_cpu_seconds_total{mode!="idle"}[5m])) / on (instance) group_left ${CORES}`, unit: 'share', per: 'mode', known: 'system' },
    { id: 'load1', label: 'Load, 1 minute', meaning: 'How many tasks were running or waiting to run, on average over the last minute.', query: 'node_load1', unit: 'load', per: 'unit', known: 'system' },
    { id: 'load5', label: 'Load, 5 minutes', meaning: 'The same, over 5 minutes.', query: 'node_load5', unit: 'load', per: 'unit', known: 'system' },
    { id: 'load15', label: 'Load, 15 minutes', meaning: 'The same, over 15 minutes.', query: 'node_load15', unit: 'load', per: 'unit', known: 'system' },
    { id: 'running', label: 'Running now', meaning: 'Tasks running at this moment.', query: 'node_procs_running', unit: 'count', per: 'unit', known: 'system' },
    { id: 'blocked', label: 'Waiting on I/O', meaning: 'Tasks stuck waiting on input or output at this moment.', query: 'node_procs_blocked', unit: 'count', per: 'unit', known: 'system' },
    { id: 'pressure', label: 'Waiting for a core', meaning: 'How much of the time some task was ready but had to wait for a core (pressure).', query: 'rate(node_pressure_cpu_waiting_seconds_total[5m])', unit: 'share', per: 'unit', known: 'system' },
    { id: 'freq', label: 'Clock, each core', meaning: 'How fast each core runs now.', query: 'node_cpu_scaling_frequency_hertz', unit: 'Hz', per: 'core', known: 'hardware' },
    { id: 'freq.max', label: 'Clock, top', meaning: 'The fastest each core can run.', query: 'node_cpu_scaling_frequency_max_hertz', unit: 'Hz', per: 'core', known: 'hardware' },
    // Measured 2026-11-19: every machine reports one, AMD's Tctl, for the whole chip; no machine has a
    // sensor per core (Intel's would be labelled "Core 0", "Core 1"…, and drawn on each machine).
    // The CPU's own sensors only (the chip: k10temp on AMD, coretemp on Intel, cpu_thermal on ARM). On
    // 2026-11-19 the room hung all seven a node reports; one was the CPU — three were its NVMe disk, one
    // the integrated graphics, two the board. Those belong to their own units (the hardware shows itself).
    { id: 'temp', label: 'CPU temperature', meaning: 'What the CPU chip’s own sensors read.', query: 'node_hwmon_temp_celsius * on (instance, chip) group_left (chip_name) node_hwmon_chip_names{chip_name=~"k10temp|coretemp|cpu_thermal|zenpower"} * on (instance, chip, sensor) group_left (label) node_hwmon_sensor_label', unit: '°C', per: 'sensor', known: 'hardware' },
    { id: 'switches', label: 'Context switches', meaning: 'How often the cores switch from one task to another.', query: 'rate(node_context_switches_total[5m])', unit: 'per second', per: 'unit', known: 'system' },
    { id: 'interrupts', label: 'Interrupts', meaning: 'How often devices interrupt the cores.', query: 'rate(node_intr_total[5m])', unit: 'per second', per: 'unit', known: 'system' },
    { id: 'guest', label: 'Lent to virtual machines', meaning: 'Time the cores spent running virtual machines.', query: 'sum by (instance) (rate(node_cpu_guest_seconds_total[5m])) / on (instance) ' + CORES, unit: 'share', per: 'unit', known: 'system' },
  ],
  // Details confirmed twice on 2026-11-19: first all 15 ("at this depth, show it all"), then, the same
  // day, narrowed to the hardware: a plant knows how much it generates, not what the power is used for.
  // What the work is (time by mode, tasks, switches, interrupts, guests) belongs to the districts that
  // use it, not to the plant.
  // Narrowed again the same day: load and waiting (pressure) are counted by the operating system's
  // scheduler — tasks asking for the CPU — not known by the CPU as hardware. The room is CPU only.
  chosen: ['cores', 'busy.core', 'freq', 'freq.max', 'temp'],
  // Inside, drafted for the Owner to see before it is drawn (a computer is a factory, the Owner
  // 2026-11-19: CPU the workshop, memory the workbench, disk the warehouse). The CPU's room is the
  // workshop: the 16×16 floor holds the machine tools to the west, the order office to the east,
  // the thermometers on the walls.
  inside: [
    { detail: 'cores', thing: 'Machine tools', shows: 'One machine tool per core, in pairs; 16 cores, 16 machines.', where: 'workshop floor' },
    { detail: 'busy.core', thing: 'Each machine’s load bar', shows: 'A bar across each machine’s top, filled as far as that core is busy.', where: 'workshop floor' },
    { detail: 'busy.core', thing: 'Lit folds on the console desk', shows: 'All the cores together: the average, in the same folds as each lathe, with the percent on the plate.', where: 'CPU console' },
    { detail: 'freq', thing: 'CLOCK on the console', shows: 'How fast the cores run (their average), in GHz.', where: 'CPU console' },
    { detail: 'freq.max', thing: 'The CLOCK dial’s end', shows: 'The dial runs up to the fastest the cores can run.', where: 'CPU console' },
    { detail: 'temp', thing: 'CHIP TEMP on the console', shows: 'The chip’s own temperature (one sensor for the whole chip), in °C; red from 80 °C. A sensor per core would stand on its machine.', where: 'CPU console' },
  ],
  // The summary (stage 3): what the console shows, and what the CPU passes up to its building. Each item
  // from chosen details only, how it is made, and the dictionary row it shows through; computed by
  // cpuSummaryOf (frame/units/cpuRoom.ts), the same computation the console draws from.
  summary: [
    { id: 'cores', label: 'Cores', from: ['cores'], how: 'count', row: 'core.machines' },
    { id: 'busy', label: 'Busy, all cores', from: ['busy.core'], how: 'average', row: 'core.busy.all' },
    { id: 'clock', label: 'Clock, against its top', from: ['freq', 'freq.max'], how: 'average', row: 'core.freq' },
    { id: 'temp', label: 'Chip temperature', from: ['temp'], how: 'maximum', row: 'sensor.temp' },
  ],
  // When the Owner confirmed each stage, in his words: a record, kept as data (confirmed.json), not in the source.
  confirmed: CONFIRMED.cpu,
}

export const UNITS: UnitSpec[] = [CPU_UNIT, MEMORY_UNIT, DISK_UNIT]

/** The units the hardware district is made of, in the order they are made (RULES.md, 由内向外). */
export const UNIT_PLAN = [
  { id: 'cpu', name: 'CPU', metaphor: 'workshop: a machine per core' },
  { id: 'memory', name: 'Memory', metaphor: 'cold store: chest freezers' },
  { id: 'disk', name: 'Disk', metaphor: 'warehouse: high-bay racks (HDD), line-side racks (SSD)' },
  { id: 'gpu', name: 'GPU', metaphor: 'heavy production line (proposed)' },
] as const

/** A read point, as the platform's PromQL proxy returns it. */
export type ReadPoint = { labels: Record<string, string>; value: number }

/**
 * One machine's workshop readings, from the CPU details read across the district: its cores in
 * order with how busy, how fast and how fast at most; the CPU's own sensors.
 * A machine nothing reads has no cores and no needles — nothing is filled in.
 */
export function cpuRoomReadings(reads: Record<string, ReadPoint[] | null> | undefined, host: string | undefined) {
  const of = (id: string) => host ? (reads?.[id] ?? []).filter(p => p.labels.instance?.split(':')[0] === host) : []
  const byCore = (id: string) => new Map(of(id).map(p => [p.labels.cpu, p.value]))
  const busy = byCore('busy.core'), freq = byCore('freq'), max = byCore('freq.max')
  const ids = [...busy.keys()].sort((a, b) => Number(a) - Number(b))
  return {
    cores: ids.map(c => ({ busy: busy.get(c) ?? null, freq: freq.get(c) ?? null, max: max.get(c) ?? null })),
    temps: of('temp').map(p => {
      const core = /^Core (\d+)$/i.exec(p.labels.label ?? '')
      return { name: p.labels.label ?? `${p.labels.chip_name ?? ''} ${p.labels.sensor ?? ''}`.trim(), c: p.value, core: core ? Number(core[1]) : undefined }
    }),
  }
}
