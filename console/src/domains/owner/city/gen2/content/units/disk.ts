import type { UnitSpec } from '../../frame/units/process'

/**
 * The disk unit (the third through the unit process, 2026-11-19). City metaphor, proposed: storage by how
 * far from the machine it is — a local SSD the line-side racks, a spinning disk a high-bay store, the NAS a
 * logistics park across the road; arrays laid out as they are built (RULES.md, 存放分层). Settled at Inside.
 *
 * Details: what node exporter reports about a machine's disks, after a read of every family it has
 * (2026-11-19 22:36Z). Three scopes: each physical drive (its kind, traffic, how busy, errors), each volume
 * a machine stores on (its size and how full), each RAID array (its state, its disks, its sync). The cluster
 * nodes report one NVMe drive and their volumes; the NAS three spinning drives and two SSDs in two arrays,
 * with btrfs error counts and the spinning drives' speed; the Mac minis only bytes read and written, and
 * their volumes. Physical drives only (no partitions, device-mapper or md devices, no boot flash).
 */
const DRIVE = 'device=~"nvme[0-9]+n[0-9]+|sd[a-z]+|vd[a-z]+|disk[0-9]+"'
// A volume: Linux root, the NAS's data volumes, the Mac's data volume (its "/" is a sealed snapshot of the
// same APFS container, so counting both counted the disk twice).
const vol = (metric: string) => `(${metric}{mountpoint=~"/volume[0-9]+|/System/Volumes/Data"} or ${metric}{mountpoint="/",fstype!="apfs"})`

export const DISK_UNIT: UnitSpec = {
  id: 'disk',
  name: 'Disk',
  what: 'The drives a machine stores on, and the volumes on them: how big, how full, how busy, how healthy. In the city, storage by how far it is from the machine (proposed).',
  details: [
    { id: 'drives', label: 'Drives', meaning: 'Each physical drive: solid state or spinning, and its model.', query: `node_disk_info{${DRIVE}}`, unit: 'kind', per: 'disk', known: 'hardware' },
    { id: 'rpm', label: 'Spinning speed', meaning: 'How fast a spinning drive turns (solid state drives do not turn).', query: 'node_disk_ata_rotation_rate_rpm', unit: 'rpm', per: 'disk', known: 'hardware' },
    { id: 'size', label: 'Volume size', meaning: 'How big each volume a machine stores on is.', query: vol('node_filesystem_size_bytes'), unit: 'bytes', per: 'volume', known: 'hardware' },
    { id: 'used', label: 'Volume used', meaning: 'How much of each volume is taken.', query: `${vol('node_filesystem_size_bytes')} - ${vol('node_filesystem_avail_bytes')}`, unit: 'bytes', per: 'volume', known: 'hardware' },
    { id: 'read', label: 'Reading', meaning: 'How fast each drive is read now.', query: `rate(node_disk_read_bytes_total{${DRIVE}}[5m])`, unit: 'bytes per second', per: 'disk', known: 'hardware' },
    { id: 'write', label: 'Writing', meaning: 'How fast each drive is written now.', query: `rate(node_disk_written_bytes_total{${DRIVE}}[5m])`, unit: 'bytes per second', per: 'disk', known: 'hardware' },
    { id: 'busy', label: 'Busy', meaning: 'How much of the time each drive was doing something.', query: `rate(node_disk_io_time_seconds_total{${DRIVE}}[5m])`, unit: 'share', per: 'disk', known: 'hardware' },
    { id: 'ops', label: 'Operations', meaning: 'How many reads and writes each drive does a second.', query: `rate(node_disk_reads_completed_total{${DRIVE}}[5m]) + rate(node_disk_writes_completed_total{${DRIVE}}[5m])`, unit: 'per second', per: 'disk', known: 'hardware' },
    { id: 'latency', label: 'Time per operation', meaning: 'How long each read or write took on average.', query: `(rate(node_disk_read_time_seconds_total{${DRIVE}}[5m]) + rate(node_disk_write_time_seconds_total{${DRIVE}}[5m])) / (rate(node_disk_reads_completed_total{${DRIVE}}[5m]) + rate(node_disk_writes_completed_total{${DRIVE}}[5m]))`, unit: 'seconds', per: 'disk', known: 'hardware' },
    { id: 'temp', label: 'Drive temperature', meaning: 'What a drive’s own sensors read (NVMe drives report theirs).', query: 'node_hwmon_temp_celsius * on (instance, chip) group_left (chip_name) node_hwmon_chip_names{chip_name=~"nvme|drivetemp"}', unit: '°C', per: 'sensor', known: 'hardware' },
    { id: 'errors', label: 'Drive errors', meaning: 'Read, write and corruption errors a drive has had, as the file system counts them (btrfs, on the NAS).', query: 'sum by (instance, device) (node_btrfs_device_errors_total)', unit: 'count', per: 'disk', known: 'hardware' },
    { id: 'array.state', label: 'Array state', meaning: 'Each RAID array: working, rebuilding, or degraded (missing a drive).', query: 'node_md_state == 1', unit: 'kind', per: 'array', known: 'hardware' },
    { id: 'array.disks', label: 'Array drives', meaning: 'Each array’s drives, by state: active, failed, spare.', query: 'node_md_disks', unit: 'count', per: 'array', known: 'hardware' },
    { id: 'array.sync', label: 'Array in step', meaning: 'How far an array is in step with itself (100% when nothing is rebuilding).', query: 'node_md_blocks_synced / node_md_blocks', unit: 'share', per: 'array', known: 'hardware' },
    { id: 'readonly', label: 'Read only', meaning: 'A volume the system could only mount read-only (often a sign of a failing drive).', query: vol('node_filesystem_readonly'), unit: 'count', per: 'volume', known: 'hardware' },
    { id: 'files', label: 'Files used', meaning: 'How many of a volume’s file slots are taken (a volume can be full of files before it is full of bytes).', query: `1 - ${vol('node_filesystem_files_free')} / ${vol('node_filesystem_files')}`, unit: 'share', per: 'volume', known: 'system' },
  ],
  // Details confirmed 2026-11-19: the fifteen the drives and volumes themselves know; file slots are the
  // file system's count (room standard 1), out.
  chosen: ['drives', 'rpm', 'size', 'used', 'read', 'write', 'busy', 'ops', 'latency', 'temp', 'errors', 'array.state', 'array.disks', 'array.sync', 'readonly'],
  // Inside (draft): the warehouse. Volumes are storage areas of standard rack bays (256 GB a bay, eight to a
  // rack); the drives are the loading docks goods come in and go out by, one dock per drive on the wall of
  // the area it serves; an array is the fence round its area and its docks. Per drive on its dock, per
  // volume on its area, the whole on the console.
  inside: [
    { detail: 'size', thing: 'Rack bays', shows: 'One bay per 256 GB (the standard part), eight bays to a rack, low; a volume’s storage area is as many bays as it is big, the last as long as its part.', where: 'storage area' },
    { detail: 'used', thing: 'Pallets on the bays', shows: 'Bays holding goods (amber) from the left, as much as is taken; the rest empty shelf.', where: 'storage area' },
    { detail: 'readonly', thing: 'Chain across the area', shows: 'A volume that can only be read: a red chain across its entrance; none when it can be written.', where: 'storage area' },
    { detail: 'drives', thing: 'Loading dock', shows: 'One dock per drive on its area’s wall. Solid state: a roller conveyor. Spinning: a turntable lift. The model on the dock’s plate.', where: 'dock wall' },
    { detail: 'rpm', thing: 'Turntable plate', shows: 'A spinning drive’s speed on its turntable’s plate (7200 rpm); solid state docks have none.', where: 'dock wall' },
    { detail: 'busy', thing: 'Dock folds', shows: 'How busy the drive is: the same lit folds as the lathes, along the dock’s edge.', where: 'dock wall' },
    { detail: 'read', thing: 'OUT on the dock', shows: 'Goods going out: how fast the drive is read, on the dock’s plate.', where: 'dock wall' },
    { detail: 'write', thing: 'IN on the dock', shows: 'Goods coming in: how fast the drive is written, on the dock’s plate.', where: 'dock wall' },
    { detail: 'ops', thing: 'Loads a second', shows: 'How many reads and writes a second, on the dock’s plate.', where: 'dock wall' },
    { detail: 'latency', thing: 'Time per load', shows: 'How long each read or write takes, on the dock’s plate (0.3 ms solid state, about 9 ms spinning).', where: 'dock wall' },
    { detail: 'temp', thing: 'Dock thermometer', shows: 'The drive’s own temperature, the small thermometer the lathes carry; none where the drive reports none.', where: 'dock wall' },
    { detail: 'errors', thing: 'Red tape on the dock', shows: 'Errors the drive has had: red tape across its dock with the count; none when there are none.', where: 'dock wall' },
    { detail: 'array.state', thing: 'Fence and its sign', shows: 'An array: a fence round its area and docks, its sign in the array’s state — working, rebuilding, degraded (in the key).', where: 'array fence' },
    { detail: 'array.disks', thing: 'Dock states', shows: 'Each dock in the fence by its drive’s state: working; failed (dark, crossed); spare (closed, SPARE).', where: 'array fence' },
    { detail: 'array.sync', thing: 'Fence sign strip', shows: 'How far the array is in step: a strip on the fence sign, full at 100%.', where: 'array fence' },
    { detail: 'used', thing: 'USED on the console desk', shows: 'How full all the volumes are together, as a share: the same lit folds, the percent on its plate.', where: 'disk console' },
    { detail: 'busy', thing: 'BUSY on the console', shows: 'The drives’ average busy, on the standard instrument.', where: 'disk console' },
  ],
  confirmed: {
    details: { on: '2026-11-19', words: '推荐的 15 项' },
  },
}
