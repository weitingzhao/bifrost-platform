// One read of every unit's details, kept for the unit workshop page (which, published, cannot reach the
// LAN), labelled with when it was read. Reads only, through the platform; the queries are the specs' own.
//   npx tsx bench/export-units.ts [http://192.168.10.100:30876]  → bench/units.local.json (not committed)
import fs from 'node:fs'
import path from 'node:path'
import { UNITS } from '../src/domains/owner/city/gen2/content/units/cpu'

const base = (process.argv[2] ?? 'http://192.168.10.100:30876').replace(/\/$/, '')
const get = async (p: string) => { const r = await fetch(base + p); if (!r.ok) throw new Error(`${p}: HTTP ${r.status}`); return r.json() }
const topology = await get('/api/v1/topology?env=prod').then(v => v.nodes ?? null, () => null)
const reads: Record<string, Record<string, unknown>> = {}
for (const u of UNITS) {
  reads[u.id] = {}
  for (const d of u.details) reads[u.id][d.id] = await get(`/api/v1/telemetry/promql?q=${encodeURIComponent(d.query)}`).then(v => v.points ?? null, () => null)
}
const out = { read_at: new Date().toISOString(), topology, reads }
fs.writeFileSync(path.join(import.meta.dirname, 'units.local.json'), JSON.stringify(out))
console.log(`units.local.json at ${out.read_at}:`, Object.fromEntries(Object.entries(reads.cpu).map(([k, v]) => [k, Array.isArray(v) ? v.length : 'not read'])))
