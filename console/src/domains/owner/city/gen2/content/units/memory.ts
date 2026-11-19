import type { UnitSpec } from '../../frame/units/process'

/**
 * The memory unit (the second through the unit process, 2026-11-19). City metaphor: the factory's cold store, a row of
 * chest freezers (the Owner, 2026-11-19) — goods kept for now, lost when the power goes (memory empties
 * as a freezer spoils); some can be cleared out at need (the cache); what does not fit goes to the
 * overflow corner (swap), slow to fetch.
 *
 * Details: what node exporter reports about a machine's memory, after a read of every family it has
 * (2026-11-19 22:07Z): the Linux nodes report /proc/meminfo whole (about fifty), the pressure and paging
 * counters; the Mac minis twelve figures of their own (total, free, active, inactive, wired, compressed,
 * purgeable, swap). Where both have the same thing, one query reads both (`or`). `known` is the advice
 * for room standard 1: the memory's own state, or an event the operating system counts.
 */
const TOTAL = '(node_memory_MemTotal_bytes or node_memory_total_bytes)'

export const MEMORY_UNIT: UnitSpec = {
  id: 'memory',
  name: 'Memory',
  what: 'The working memory of a machine: how much it has and how full it is. In the city, the factory’s cold store: chest freezers.',
  details: [
    { id: 'total', label: 'Installed', meaning: 'How much memory it has.', query: TOTAL, unit: 'bytes', per: 'unit', known: 'hardware' },
    { id: 'used', label: 'In use', meaning: 'Taken by what is being worked on and cannot be cleared. Linux: installed less available; macOS: active, wired and compressed.', query: '(node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes) or (node_memory_active_bytes + node_memory_wired_bytes + node_memory_compressed_bytes)', unit: 'bytes', per: 'unit', known: 'hardware' },
    { id: 'available', label: 'Available', meaning: 'What could be given to work now without pushing anything out: empty, plus what can be cleared. Linux: MemAvailable; macOS: installed less in use.', query: 'node_memory_MemAvailable_bytes or (node_memory_total_bytes - (node_memory_active_bytes + node_memory_wired_bytes + node_memory_compressed_bytes))', unit: 'bytes', per: 'unit', known: 'hardware' },
    { id: 'free', label: 'Empty', meaning: 'Not holding anything at all.', query: 'node_memory_MemFree_bytes or node_memory_free_bytes', unit: 'bytes', per: 'unit', known: 'hardware' },
    { id: 'cache', label: 'Cache (can be cleared)', meaning: 'Holding copies of what is on disk, kept while there is room and cleared when the room is needed. Linux: page cache, buffers and reclaimable kernel caches; macOS: inactive and purgeable.', query: '(node_memory_Cached_bytes + node_memory_Buffers_bytes + node_memory_SReclaimable_bytes) or (node_memory_inactive_bytes + node_memory_purgeable_bytes)', unit: 'bytes', per: 'unit', known: 'hardware' },
    { id: 'compressed', label: 'Compressed', meaning: 'Packed tighter to make room (macOS compressor, Linux zswap).', query: 'node_memory_compressed_bytes or node_memory_Zswap_bytes', unit: 'bytes', per: 'unit', known: 'hardware' },
    { id: 'kernel', label: 'Kept by the system', meaning: 'Held by the operating system itself and never cleared. Linux: unreclaimable slab, kernel stacks, page tables; macOS: wired.', query: '(node_memory_SUnreclaim_bytes + node_memory_KernelStack_bytes + node_memory_PageTables_bytes) or node_memory_wired_bytes', unit: 'bytes', per: 'unit', known: 'hardware' },
    { id: 'dirty', label: 'Waiting to be written', meaning: 'Changed in memory, not yet written back to disk.', query: 'node_memory_Dirty_bytes + node_memory_Writeback_bytes', unit: 'bytes', per: 'unit', known: 'hardware' },
    { id: 'swap.total', label: 'Overflow room', meaning: 'Disk set aside for what does not fit in memory (swap).', query: 'node_memory_SwapTotal_bytes or node_memory_swap_total_bytes', unit: 'bytes', per: 'unit', known: 'hardware' },
    { id: 'swap.used', label: 'Overflowed', meaning: 'How much of it is pushed out to disk now.', query: '(node_memory_SwapTotal_bytes - node_memory_SwapFree_bytes) or node_memory_swap_used_bytes', unit: 'bytes', per: 'unit', known: 'hardware' },
    { id: 'swap.io', label: 'Overflow traffic', meaning: 'How fast it is moved to and from the overflow on disk, in and out together.', query: '(rate(node_vmstat_pswpin[5m]) + rate(node_vmstat_pswpout[5m])) * 4096 or (rate(node_memory_swapped_in_bytes_total[5m]) + rate(node_memory_swapped_out_bytes_total[5m]))', unit: 'bytes per second', per: 'unit', known: 'hardware' },
    { id: 'corrupted', label: 'Hardware errors', meaning: 'Memory the hardware reports as damaged and taken out of use.', query: 'node_memory_HardwareCorrupted_bytes', unit: 'bytes', per: 'unit', known: 'hardware' },
    { id: 'faults', label: 'Major page faults', meaning: 'How often a program asked for something that had to be fetched from disk.', query: 'rate(node_vmstat_pgmajfault[5m])', unit: 'per second', per: 'unit', known: 'system' },
    { id: 'pressure', label: 'Waiting for memory', meaning: 'How much of the time some task waited because memory was short (pressure).', query: 'rate(node_pressure_memory_waiting_seconds_total[5m])', unit: 'share', per: 'unit', known: 'system' },
    { id: 'oom', label: 'Programs killed for memory', meaning: 'How many programs the system stopped because memory ran out, since it started.', query: 'node_vmstat_oom_kill', unit: 'count', per: 'unit', known: 'system' },
  ],
  // Details confirmed 2026-11-19, then narrowed the same day (the Owner: seven colours on the bench are
  // noise unless each can be read apart and keyed; better plain). Three that add up to what is installed,
  // read on Linux and macOS alike: in use (installed − available), can be cleared (available − empty),
  // empty. Kernel, compressed and waiting-to-be-written are splits inside those three: out. Swap and
  // hardware errors stay. Counts since boot are history (room standard 12): out.
  // Then two back (the Owner, same day: if the kinds in use can be told apart, show them — on the console):
  // what the system keeps and what is compressed can be read exactly on both systems; the rest of what is
  // in use is programs and the rest, not split further.
  chosen: ['total', 'available', 'free', 'kernel', 'compressed', 'swap.total', 'swap.used', 'swap.io', 'corrupted'],
  // Inside: the cold store (the Owner, 2026-11-19: a chest freezer, not a wooden bench — memory empties
  // when the power goes, as a freezer spoils). One chest freezer per 4 GB (the standard part), in pairs,
  // low, its glass lid on top showing what is inside now: goods in use, goods that can be cleared, the
  // empty cold floor. An overflow corner on the floor for swap; the room console.
  inside: [
    { detail: 'total', thing: 'Chest freezers', shows: 'One freezer per 4 GB (the standard part); 24 GB, six freezers, in pairs; the last as long as its part.', where: 'cold store floor' },
    { detail: 'available', thing: 'Goods in use / goods that can be cleared', shows: 'Through the glass lid, from the left: goods in use (amber), as much as installed less available; then goods that can be cleared (pale), as much as available less empty.', where: 'cold store floor' },
    { detail: 'free', thing: 'Empty cold floor', shows: 'The rest of the freezer: nothing in it.', where: 'cold store floor' },
    { detail: 'corrupted', thing: 'Taped-off lid', shows: 'Damaged and out of use: red tape across the last freezers’ lids; none when nothing is damaged.', where: 'cold store floor' },
    { detail: 'swap.total', thing: 'Overflow corner', shows: 'Floor marked off at the front for what does not fit in the freezers, as long as the swap in the same 4 GB parts.', where: 'overflow corner' },
    { detail: 'swap.used', thing: 'Crates in the corner', shows: 'What is pushed out there now: crates filling the marked floor.', where: 'overflow corner' },
    { detail: 'swap.io', thing: 'TRAFFIC on the console', shows: 'How fast things move between the freezers and the corner, on the standard instrument.', where: 'memory console' },
    { detail: 'kernel', thing: 'IN USE BY KIND on the console: system', shows: 'The darkest amber of the console’s split bar: what the system keeps and never clears.', where: 'memory console' },
    { detail: 'compressed', thing: 'IN USE BY KIND on the console: compressed', shows: 'The middle amber: packed tighter to make room. The lightest is programs and the rest of what is in use.', where: 'memory console' },
    { detail: 'available', thing: 'IN USE on the console desk', shows: 'How full the freezers are with goods in use, as a share: the same lit folds as the CPU console, the percent on its plate.', where: 'memory console' },
  ],
  confirmed: {
    details: { on: '2026-11-19', words: '这里有个图例…不然的话，你这光显示这东西，你这不知道什么玩意儿，什么是空的，什么用到的都不知道啊…就噪音' },
    inside: { on: '2026-11-19', words: '可以，做硬盘' },
  },
}

/** One machine's memory readings, from the details read across the district; nothing read is null. */
export function memoryRoomReadings(reads: Record<string, { labels: Record<string, string>; value: number }[] | null> | undefined, host: string | undefined) {
  const one = (id: string) => host ? (reads?.[id] ?? []).find(p => p.labels.instance?.split(':')[0] === host)?.value ?? null : null
  return {
    total: one('total'), available: one('available'), free: one('free'), kernel: one('kernel'), compressed: one('compressed'),
    swapTotal: one('swap.total'), swapUsed: one('swap.used'), swapIO: one('swap.io'), corrupted: one('corrupted'),
  }
}
