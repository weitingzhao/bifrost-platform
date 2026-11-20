import type { UnitSpec } from '../../frame/units/process'
import type { ArrayReading, DiskRoomReadings, DriveReading, VolumeReading } from '../../frame/units/diskRoom'

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
  // Inside: the warehouse. Volumes are storage areas of standard rack bays (256 GB a bay, eight to a rack);
  // the drives are the loading docks goods come in and go out by, one per drive along the front; an array a
  // board (which drives and volumes it holds is not reported, so no fence: drawn 2026-11-19). Per drive on
  // its dock, per volume on its area, the whole on the console.
  inside: [
    { detail: 'size', thing: 'Rack bays', shows: 'One bay per 256 GB (the standard part), eight to a low rack, four along and two back to back; a volume’s storage area is as many whole bays as it is big.', where: 'storage area' },
    { detail: 'used', thing: 'Pallets on the bays', shows: 'Bays holding goods (amber) from the left, as much as is taken; the rest empty shelf.', where: 'storage area' },
    { detail: 'readonly', thing: 'Chain across the area', shows: 'A volume that can only be read: a red chain across its entrance; none when it can be written.', where: 'storage area' },
    { detail: 'drives', thing: 'Loading dock', shows: 'One dock per drive, along the front. Solid state: a roller conveyor. Spinning: a turntable. Plain when the kind is not read. The model (or the device) on the dock’s plate.', where: 'dock wall' },
    { detail: 'rpm', thing: 'Turntable plate', shows: 'A spinning drive’s speed written on its turntable; solid state docks have none.', where: 'dock wall' },
    { detail: 'busy', thing: 'Dock folds', shows: 'How busy the drive is: the same lit folds as the lathes, along the dock’s edge.', where: 'dock wall' },
    { detail: 'read', thing: 'OUT on the dock', shows: 'Goods going out: how fast the drive is read, on the dock’s plate.', where: 'dock wall' },
    { detail: 'write', thing: 'IN on the dock', shows: 'Goods coming in: how fast the drive is written, on the dock’s plate.', where: 'dock wall' },
    { detail: 'ops', thing: 'Loads a second', shows: 'How many reads and writes a second, on the dock’s plate.', where: 'dock wall' },
    { detail: 'latency', thing: 'Time per load', shows: 'How long each read or write takes, on the dock’s plate.', where: 'dock wall' },
    { detail: 'temp', thing: 'Dock thermometer', shows: 'The drive’s own temperature, the small thermometer the lathes carry; none where the drive reports none.', where: 'dock wall' },
    { detail: 'errors', thing: 'Red tape on the area', shows: 'Errors the file system counted on a volume (btrfs counts them per volume): red tape across its area with the count; none when there are none.', where: 'storage area' },
    { detail: 'array.state', thing: 'Array board', shows: 'A low board per array at the back, its state written on it (ACTIVE; anything else in red). Which drives and volumes an array holds is not reported, so the board stands apart rather than fencing them.', where: 'array boards' },
    { detail: 'array.disks', thing: 'Lamps on the board', shows: 'A lamp per drive of the array: green active, red failed, grey spare.', where: 'array boards' },
    { detail: 'array.sync', thing: 'Strip on the board', shows: 'How far the array is in step: a strip filling to 100%.', where: 'array boards' },
    { detail: 'used', thing: 'USED on the console desk', shows: 'How full all the volumes are together, as a share: the same lit folds, the percent on its plate.', where: 'disk console' },
    { detail: 'busy', thing: 'BUSY on the console', shows: 'The drives’ average busy, on the standard instrument.', where: 'disk console' },
  ],
  confirmed: {
    details: { on: '2026-11-19', words: '推荐的 15 项' },
  },
}

type Point = { labels: Record<string, string>; value: number }

/** One machine's disk readings, from the details read across the district; nothing read is null. */
export function diskRoomReadings(reads: Record<string, Point[] | null> | undefined, host: string | undefined): DiskRoomReadings {
  const mine = (id: string) => host ? (reads?.[id] ?? []).filter(p => p.labels.instance?.split(':')[0] === host) : []
  const by = (id: string, key: string) => new Map(mine(id).map(p => [p.labels[key], p]))
  const info = by('drives', 'device'), rpm = by('rpm', 'device')
  const per = ['read', 'write', 'busy', 'ops', 'latency'].map(id => [id, by(id, 'device')] as const)
  // A drive's own temperature: an NVMe drive's controller reports as hwmon chip nvme_nvmeN (namespace
  // nvmeNn1); its first sensor is the composite the drive itself gives as its temperature.
  const temp = new Map(mine('temp').filter(p => p.labels.sensor === 'temp1').map(p => [p.labels.chip?.replace(/^nvme_(nvme\d+)$/, '$1n1'), p.value]))
  const devices = [...new Set([...info.keys(), ...per.flatMap(([, m]) => [...m.keys()])])].filter(Boolean).sort()
  const num = (v: number | undefined) => v == null || !Number.isFinite(v) ? null : v
  const drives: DriveReading[] = devices.map(d => {
    const i = info.get(d)?.labels
    const v = Object.fromEntries(per.map(([id, m]) => [id, num(m.get(d)?.value)]))
    return {
      device: d, kind: i ? (i.rotational === '1' ? 'hdd' : 'ssd') : null, model: i?.model || null, rpm: num(rpm.get(d)?.value),
      read: v.read, write: v.write, busy: v.busy, ops: v.ops, latency: v.latency, temp: temp.get(d) ?? null,
    }
  })
  const used = by('used', 'mountpoint'), ro = by('readonly', 'mountpoint'), errs = mine('errors')
  const volumes: VolumeReading[] = mine('size').map(p => {
    const mp = p.labels.mountpoint, dev = p.labels.device ?? ''
    const e = errs.find(x => x.labels.device && dev.endsWith(x.labels.device))
    return { name: mp, size: p.value, used: num(used.get(mp)?.value), readonly: ro.has(mp) ? ro.get(mp)!.value > 0 : null, errors: e ? e.value : null }
  }).sort((a, b) => b.size - a.size)
  const disks = mine('array.disks'), sync = by('array.sync', 'device')
  const arrays: ArrayReading[] = mine('array.state').map(p => {
    const n = p.labels.device, count = (s: string) => num(disks.find(x => x.labels.device === n && x.labels.state === s)?.value)
    return { name: n, state: p.labels.state ?? null, active: count('active'), failed: count('failed'), spare: count('spare'), sync: num(sync.get(n)?.value) }
  }).sort((a, b) => a.name.localeCompare(b.name))
  return { drives, volumes, arrays }
}
