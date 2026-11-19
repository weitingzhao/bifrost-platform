import type { UnitSpec } from '../../frame/units/process'

/**
 * The CPU unit (the first through the unit process, 2026-11-19). City metaphor: the generator hall
 * of a power plant. Details: what node exporter reports about a machine's CPU, listed after a read of
 * every metric family it has (2026-11-19 20:40Z): the Linux nodes report all of these; the Mac minis
 * only the time per core and the load; the NAS has no thermal zone.
 */
const CORES = 'count by (instance) (count by (instance, cpu) (node_cpu_seconds_total))'

export const CPU_UNIT: UnitSpec = {
  id: 'cpu',
  name: 'CPU',
  what: 'The processor of a machine: its cores and how hard they work. In the city, the generator hall of a power plant.',
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
  confirmed: {},
}

export const UNITS: UnitSpec[] = [CPU_UNIT]

/** The units the hardware district is made of, in the order they are made (RULES.md, 由内向外). */
export const UNIT_PLAN = [
  { id: 'cpu', name: 'CPU', metaphor: 'generator hall' },
  { id: 'memory', name: 'Memory', metaphor: 'cooling water tank' },
  { id: 'disk', name: 'Disk', metaphor: 'fuel tank' },
  { id: 'volume', name: 'Volume', metaphor: 'warehouse store' },
  { id: 'gpu', name: 'GPU', metaphor: 'cooling tower' },
] as const
