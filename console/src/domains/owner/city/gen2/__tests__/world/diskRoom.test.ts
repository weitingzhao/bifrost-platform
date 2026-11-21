import { describe, expect, it } from 'vitest'
import { rowsDrawn, setRuler } from '@bifrost/ui/city/library/dictionary'
import { baysOf, LOW_RACK, WARN_FROM, diskRoom, diskRoomShape, diskSummaryOf, volumeName, type DiskRoomReadings } from '@bifrost/ui/city/units/diskRoom'
import { diskRoomReadings } from '../../content/units/disk'

// 2026-11-19, the disk unit's room, the warehouse (shelves the Owner's word the same day), drawn against the room standards. The readings are one
// real read of that day (a cluster node, the NAS, a Mac mini).
const GB = 1e9, TB = 1e12
const ssd = { kind: 'ssd' as const, model: 'G-BONG X300 512GB', rpm: null, read: 819, write: 136e3, busy: 0.021, ops: 20.5, latency: 0.00136, temp: 44.9 }
const node: DiskRoomReadings = { drives: [{ device: 'nvme0n1', ...ssd }], volumes: [{ name: '/', size: 500 * GB, used: 226 * GB, readonly: false, errors: null }], arrays: [] }
const hdd = (device: string) => ({ device, kind: 'hdd' as const, model: 'ST12000NM0127', rpm: 7200, read: 28.9e3, write: 179e3, busy: 0.03, ops: 11.4, latency: 0.0103, temp: null })
const nas: DiskRoomReadings = {
  drives: [hdd('sda'), hdd('sdb'), hdd('sdc'), { device: 'nvme0n1', ...ssd, model: 'KINGSTON SNV3S1000G', busy: 0 }, { device: 'nvme1n1', ...ssd, model: 'KINGSTON SNV3S1000G', busy: 0 }],
  volumes: [{ name: '/volume1', size: 24 * TB, used: 668 * GB, readonly: false, errors: 0 }, { name: '/volume2', size: 984 * GB, used: 464 * GB, readonly: false, errors: 0 }],
  arrays: [{ name: 'md1', state: 'active', active: 3, failed: 0, spare: 0, sync: 1 }, { name: 'md2', state: 'active', active: 2, failed: 0, spare: 0, sync: 1 }],
}
const mac: DiskRoomReadings = { drives: [{ device: 'disk0', kind: null, model: null, rpm: null, read: 19.2e3, write: 247e3, busy: null, ops: 18.1, latency: 0.00007, temp: null }], volumes: [{ name: '/System/Volumes/Data', size: 245 * GB, used: 128 * GB, readonly: false, errors: null }], arrays: [] }

describe('the disk room', () => {
  it('whole bays, one per 256 GB: 500 GB two, 245 GB one, 984 GB four, 24 TB ninety-four', () => {
    expect(nas.volumes.map(baysOf)).toEqual([94, 4])
    expect(baysOf(node.volumes[0])).toBe(2)
    expect(baysOf(mac.volumes[0])).toBe(1)
  })
  // Rooms grow with their parts: a node's 500 GB in the smallest room, the NAS's 25 TB in the largest.
  it('the room is the smallest that holds its shelves and bays', () => {
    expect(diskRoomShape(node)).toEqual([4, 4])
    expect(diskRoomShape(mac)).toEqual([4, 4])
    expect(diskRoomShape(nas)).toEqual([8, 8])
    expect(diskRoom(nas).svg).not.toContain('DO NOT FIT')
  })
  it('a rack per drive, its kind its form; the spinning speed on its plate; not read, not drawn', () => {
    const n = rowsDrawn(() => diskRoom(nas))
    expect(n.ids).toEqual(expect.arrayContaining(['drive.shelf', 'drive.rpm', 'drive.busy', 'drive.traffic', 'array.state', 'array.disks', 'array.sync', 'vol.used', 'disk.used.all', 'drive.busy.all']))
    expect(n.out.svg).toContain('7200 RPM')
    const m = rowsDrawn(() => diskRoom(mac))
    // The Mac reports no busy, no kind, no temperature: no folds, no BUSY instrument, no thermometer.
    expect(m.ids).not.toContain('drive.busy')
    expect(m.ids).not.toContain('drive.busy.all')
    expect(m.ids).not.toContain('drive.temp')
    expect(m.ids).not.toContain('drive.rpm')
  })
  it('every room has a console: how full the volumes are together, the drives’ average busy', () => {
    const s = diskSummaryOf(nas)
    expect(s.used).toBeCloseTo((668 + 464) / (24000 + 984))
    expect(diskRoom(node).svg).toContain('USED 45%')
    expect(diskRoom(node).svg).toContain('BUSY')
  })
  it('red only when something is wrong: a read-only volume chained, errors taped, an array not active in red', () => {
    expect(rowsDrawn(() => diskRoom(node)).ids).not.toContain('vol.readonly')
    const bad: DiskRoomReadings = { ...node, volumes: [{ ...node.volumes[0], readonly: true, errors: 3 }] }
    const b = rowsDrawn(() => diskRoom(bad))
    expect(b.ids).toEqual(expect.arrayContaining(['vol.readonly', 'vol.errors']))
    expect(b.out.svg).toContain('READ ONLY')
    expect(b.out.svg).toContain('3 ERRORS')
  })
  // 2026-11-19, the Owner: the stack must be countable, and the last places red, "警戒区…硬盘快满了".
  it('the last places past 85% of a volume are red; a nearly full disk has red cartons', () => {
    expect(WARN_FROM).toBe(0.85)
    expect(rowsDrawn(() => diskRoom(node)).ids).toContain('vol.redzone')
    const full: DiskRoomReadings = { ...node, volumes: [{ ...node.volumes[0], size: 4 * TB, used: 3.6 * TB }] }
    expect(diskRoom(full).svg).toContain('#e0533d')
    expect(diskRoom(node).svg).not.toContain('#e0533d')
  })
  it('nothing read, nothing drawn but the word', () => {
    const { ids, out } = rowsDrawn(() => diskRoom({ drives: [], volumes: [], arrays: [] }))
    expect(ids).toEqual([])
    expect(out.svg).toContain('DISK NOT READ')
  })
  // Room standard 5: no low rack rises above the line written beneath the rack behind it (the high-bay racks
  // stand against the wall, nothing behind them).
  it('nothing hides anything: no low rack higher than the gap between rows', () => {
    for (const k of ['line', 'plain'] as const) expect(1 + LOW_RACK[k].levels * LOW_RACK[k].level).toBeLessThanOrEqual((LOW_RACK.row - LOW_RACK.depth - LOW_RACK.label) * 16)
  })
  // 2026-11-19: which NAS volume lies on which drive is not reported (node_exporter has no md membership);
  // goods on a guessed shelf would be made up (room standard: nothing made up).
  it('one drive: its volumes on its shelf; several drives: the volumes on the staging floor, not a guessed shelf', () => {
    expect(diskRoom(node).svg).not.toContain('SHELF NOT REPORTED')
    expect(diskRoom(node).svg).toContain('ROOT')
    expect(diskRoom(nas).svg).toContain('SHELF NOT REPORTED')
    expect(diskRoom(nas).svg).toContain('VOLUME1')
  })
  it('the ruler counts the bays: 1 TB a bay, a quarter of the bays, the same reading', () => {
    setRuler('disk.bays', 1e12)
    try { expect(baysOf(nas.volumes[0])).toBe(24) } finally { setRuler('disk.bays', null) }
  })
  it('volumes by where they are mounted, short', () => {
    expect(['/', '/volume1', '/System/Volumes/Data'].map(volumeName)).toEqual(['ROOT', 'VOLUME1', 'DATA'])
  })
  // The Mac's "/" and its data volume are one APFS container; and an NVMe drive's temperature is its hwmon
  // chip nvme_nvmeN's first sensor.
  it('readings: drives joined across details by device, temperature from the drive’s own chip, errors to their volume', () => {
    const p = (instance: string, labels: Record<string, string>, value: number) => ({ labels: { instance, ...labels }, value })
    const h = '192.168.10.20', i = `${h}:9100`
    const r = diskRoomReadings({
      drives: [p(i, { device: 'nvme0n1', rotational: '0', model: 'K' }, 1), p(i, { device: 'sda', rotational: '1', model: 'S' }, 1)],
      rpm: [p(i, { device: 'sda' }, 7200)], busy: [p(i, { device: 'sda' }, 0.03)],
      temp: [p(i, { chip: 'nvme_nvme0', sensor: 'temp1' }, 43), p(i, { chip: 'nvme_nvme0', sensor: 'temp3' }, 69)],
      size: [p(i, { mountpoint: '/volume1', device: '/dev/mapper/x_pool1-volume1' }, 24e12)],
      used: [p(i, { mountpoint: '/volume1' }, 668e9)], errors: [p(i, { device: 'x_pool1-volume1' }, 0)],
      'array.state': [p(i, { device: 'md1', state: 'active' }, 1)], 'array.disks': [p(i, { device: 'md1', state: 'active' }, 3)], 'array.sync': [p(i, { device: 'md1' }, 1)],
    }, h)
    expect(r.drives.map(d => [d.device, d.kind, d.temp, d.rpm])).toEqual([['nvme0n1', 'ssd', 43, null], ['sda', 'hdd', null, 7200]])
    expect(r.volumes[0]).toMatchObject({ name: '/volume1', used: 668e9, errors: 0 })
    expect(r.arrays[0]).toMatchObject({ name: 'md1', state: 'active', active: 3, sync: 1 })
  })
})
