import type { UnitSpec } from '../../frame/units/process'

/**
 * The memory unit (the second through the unit process, 2026-11-19). City metaphor: the workbench of a
 * factory — room for what is being worked on now, cleared at the end of the day (memory empties when the
 * power goes); what can be cleared off at need is the cache; what does not fit goes to the corner of the
 * warehouse (swap), slow to fetch.
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
  what: 'The working memory of a machine: how much it has and how full it is. In the city, the workbench of a factory.',
  details: [
    { id: 'total', label: 'Installed', meaning: 'How much memory it has.', query: TOTAL, unit: 'bytes', per: 'unit', known: 'hardware' },
    { id: 'used', label: 'In use', meaning: 'Taken by what is being worked on and cannot be cleared. Linux: installed less available; macOS: active, wired and compressed.', query: '(node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes) or (node_memory_active_bytes + node_memory_wired_bytes + node_memory_compressed_bytes)', unit: 'bytes', per: 'unit', known: 'hardware' },
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
  // Details confirmed 2026-11-19: the eleven the memory itself knows, as it is now. The three the system
  // counts are out (room standard 1); the programs killed for memory is a count since boot, history,
  // which belongs outside the room (room standard 12).
  chosen: ['total', 'used', 'free', 'cache', 'compressed', 'kernel', 'dirty', 'swap.total', 'swap.used', 'swap.io', 'corrupted'],
  // Inside, drafted for the Owner to see before it is drawn: the workbench room. The bench is laid out in
  // standard parts (a section per 4 GB), its top showing what is on it now, side by side from the left:
  // the system's own fixtures, the work in hand, the packed boxes, the copies that can be cleared off, the
  // parts waiting to go to the warehouse, and the bare top. Flat on the tops, nothing hides anything.
  inside: [
    { detail: 'total', thing: 'Bench sections', shows: 'One section per 4 GB (the standard part); 24 GB, six sections, in pairs.', where: 'workbench floor' },
    { detail: 'kernel', thing: 'Fixtures bolted to the bench', shows: 'Kept by the system and never cleared: grey vices and clamps, first from the left.', where: 'workbench floor' },
    { detail: 'used', thing: 'Work in hand', shows: 'What is being worked on: amber workpieces, next along the top.', where: 'workbench floor' },
    { detail: 'compressed', thing: 'Packed boxes', shows: 'Packed tighter to make room: small blue boxes, next.', where: 'workbench floor' },
    { detail: 'cache', thing: 'Copies that can be swept off', shows: 'Copies of what is in the warehouse, kept while there is room: pale sheets, next; they go first when room is needed.', where: 'workbench floor' },
    { detail: 'dirty', thing: 'Parts waiting for the warehouse', shows: 'Changed and not yet put away: a few on a tray at the bench’s end.', where: 'workbench floor' },
    { detail: 'free', thing: 'Bare bench top', shows: 'Nothing on it: the rest of the top, empty.', where: 'workbench floor' },
    { detail: 'corrupted', thing: 'Taped-off section', shows: 'Damaged and out of use: a section crossed with red tape; none when nothing is damaged.', where: 'workbench floor' },
    { detail: 'swap.total', thing: 'Overflow corner', shows: 'Floor marked off by the wall for what does not fit on the bench, as big as the swap (in the same 4 GB parts).', where: 'overflow corner' },
    { detail: 'swap.used', thing: 'Crates in the corner', shows: 'What is pushed out there now: crates filling the marked floor.', where: 'overflow corner' },
    { detail: 'swap.io', thing: 'TRAFFIC on the console', shows: 'How fast things move between the bench and the corner, on the standard instrument.', where: 'memory console' },
    { detail: 'used', thing: 'IN USE on the console desk', shows: 'How full the bench is with work in hand, as a share: the same lit folds as the CPU console, the percent on its plate.', where: 'memory console' },
  ],
  confirmed: {
    details: { on: '2026-11-19', words: '我们这个房间的话，就是来去记录现在瞬时的内存的状态反应就可以了' },
  },
}
