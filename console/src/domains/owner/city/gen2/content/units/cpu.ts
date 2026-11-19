import type { UnitSpec } from '../../frame/units/process'

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
  what: 'The processor of a machine: its cores and how hard they work. In the city, the workshop of a factory (proposed).',
  details: [
    { id: 'cores', label: 'Cores', meaning: 'How many cores it has.', query: CORES, unit: 'count', per: 'machine' },
    { id: 'busy.core', label: 'Busy, each core', meaning: 'How much of the last 5 minutes each core was not idle.', query: '1 - avg by (instance, cpu) (rate(node_cpu_seconds_total{mode="idle"}[5m]))', unit: 'share', per: 'core' },
    { id: 'busy.mode', label: 'Busy, by what', meaning: 'What the time went to, as a share of all cores: user programs, the system, waiting on disk (iowait), interrupts, taken by the host (steal).', query: `sum by (instance, mode) (rate(node_cpu_seconds_total{mode!="idle"}[5m])) / on (instance) group_left ${CORES}`, unit: 'share', per: 'mode' },
    { id: 'load1', label: 'Load, 1 minute', meaning: 'How many tasks were running or waiting to run, on average over the last minute.', query: 'node_load1', unit: 'load', per: 'machine' },
    { id: 'load5', label: 'Load, 5 minutes', meaning: 'The same, over 5 minutes.', query: 'node_load5', unit: 'load', per: 'machine' },
    { id: 'load15', label: 'Load, 15 minutes', meaning: 'The same, over 15 minutes.', query: 'node_load15', unit: 'load', per: 'machine' },
    { id: 'running', label: 'Running now', meaning: 'Tasks running at this moment.', query: 'node_procs_running', unit: 'count', per: 'machine' },
    { id: 'blocked', label: 'Blocked now', meaning: 'Tasks stuck waiting on input or output at this moment.', query: 'node_procs_blocked', unit: 'count', per: 'machine' },
    { id: 'pressure', label: 'Waiting for a core', meaning: 'How much of the time some task was ready but had to wait for a core (pressure).', query: 'rate(node_pressure_cpu_waiting_seconds_total[5m])', unit: 'share', per: 'machine' },
    { id: 'freq', label: 'Clock, each core', meaning: 'How fast each core runs now.', query: 'node_cpu_scaling_frequency_hertz', unit: 'Hz', per: 'core' },
    { id: 'freq.max', label: 'Clock, top', meaning: 'The fastest each core can run.', query: 'node_cpu_scaling_frequency_max_hertz', unit: 'Hz', per: 'core' },
    { id: 'temp', label: 'Temperature, each sensor', meaning: 'What its temperature sensors read (the chip, its cores, the board).', query: 'node_hwmon_temp_celsius', unit: '°C', per: 'sensor' },
    { id: 'switches', label: 'Context switches', meaning: 'How often the cores switch from one task to another.', query: 'rate(node_context_switches_total[5m])', unit: 'per second', per: 'machine' },
    { id: 'interrupts', label: 'Interrupts', meaning: 'How often devices interrupt the cores.', query: 'rate(node_intr_total[5m])', unit: 'per second', per: 'machine' },
    { id: 'guest', label: 'Lent to virtual machines', meaning: 'Time the cores spent running virtual machines.', query: 'sum by (instance) (rate(node_cpu_guest_seconds_total[5m])) / on (instance) ' + CORES, unit: 'share', per: 'machine' },
  ],
  // Details confirmed twice on 2026-11-19: first all 15 ("at this depth, show it all"), then, the same
  // day, narrowed to the hardware: a plant knows how much it generates, not what the power is used for.
  // What the work is (time by mode, tasks, switches, interrupts, guests) belongs to the districts that
  // use it, not to the plant.
  chosen: ['cores', 'busy.core', 'freq', 'freq.max', 'temp', 'load1', 'load5', 'load15', 'pressure'],
  // Inside, drafted for the Owner to see before it is drawn (a computer is a factory, the Owner
  // 2026-11-19: CPU the workshop, memory the workbench, disk the warehouse). The CPU's room is the
  // workshop: the 16×16 floor holds the machine tools to the west, the order office to the east,
  // the thermometers on the walls.
  inside: [
    { detail: 'cores', thing: 'Machine tools', shows: 'One machine tool per core, in rows; 16 cores, 16 machines.', where: 'workshop floor' },
    { detail: 'busy.core', thing: 'Each machine’s work light', shows: 'The light column on each machine stands as high as that core is busy; an idle machine is dark.', where: 'workshop floor' },
    { detail: 'freq', thing: 'Each machine’s speed dial', shows: 'A dial on each machine: the needle at how fast the core runs now.', where: 'workshop floor' },
    { detail: 'freq.max', thing: 'The red line on each speed dial', shows: 'The red mark on the same dial: the fastest it can run.', where: 'workshop floor' },
    { detail: 'temp', thing: 'Thermometers on the walls', shows: 'One thermometer per sensor, its column as high as it reads; red near its limit.', where: 'along the walls' },
    { detail: 'load1', thing: 'The order board, 1 minute', shows: 'Three dials of the work asked of the workshop; a needle past the number of machines means more is asked than it can do.', where: 'order office' },
    { detail: 'load5', thing: 'The order board, 5 minutes', shows: 'The second of the three dials.', where: 'order office' },
    { detail: 'load15', thing: 'The order board, 15 minutes', shows: 'The third of the three dials.', where: 'order office' },
    { detail: 'pressure', thing: 'The waiting line', shows: 'Work waiting for a free machine: the line is as long as the share of time something waited.', where: 'order office' },
  ],
  confirmed: {
    details: { on: '2026-11-19', words: '更多看到的是硬件层面相关的东西…你只是发电的…你不知道电是用来干什么的' },
  },
}

export const UNITS: UnitSpec[] = [CPU_UNIT]

/** The units the hardware district is made of, in the order they are made (RULES.md, 由内向外). */
export const UNIT_PLAN = [
  { id: 'cpu', name: 'CPU', metaphor: 'workshop: a machine per core' },
  { id: 'memory', name: 'Memory', metaphor: 'workbench' },
  { id: 'disk', name: 'Disk', metaphor: 'line-side racks (SSD), high-bay store (HDD), across the road (NAS) — proposed' },
  { id: 'gpu', name: 'GPU', metaphor: 'heavy production line (proposed)' },
] as const
