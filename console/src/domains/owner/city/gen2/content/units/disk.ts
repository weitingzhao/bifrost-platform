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
  confirmed: {},
}
