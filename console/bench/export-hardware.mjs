// One read of the hardware district's sources, kept for the self-contained page (which cannot reach
// the LAN) and labelled with when it was read. Reads only, through the platform:
//   node bench/export-hardware.mjs [http://192.168.10.100:30876]  → bench/hardware.local.json (not committed)
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const base = (process.argv[2] ?? 'http://192.168.10.100:30876').replace(/\/$/, '')
const src = fs.readFileSync(path.join(here, '../src/domains/owner/city/gen2/content/hardware.ts'), 'utf8')
// The queries are the content's own: read them from it, never copied here.
const block = src.slice(src.indexOf('HARDWARE_QUERIES = {'), src.indexOf('} as const'))
const queries = Object.fromEntries([...block.matchAll(/^\s+(\w+): '(.*)',$/gm)].map(m => [m[1], m[2].replace(/\\'/g, "'")]))
const get = async p => { const r = await fetch(base + p); if (!r.ok) throw new Error(`${p}: HTTP ${r.status}`); return r.json() }
const topology = await get('/api/v1/topology?env=prod').then(v => v.nodes ?? null, () => null)
const prom = {}
for (const [k, q] of Object.entries(queries)) prom[k] = await get(`/api/v1/telemetry/promql?q=${encodeURIComponent(q)}`).then(v => v.points ?? null, () => null)
const out = { read_at: new Date().toISOString(), topology, prom }
fs.writeFileSync(path.join(here, 'hardware.local.json'), JSON.stringify(out))
console.log(`hardware.local.json: ${topology?.length ?? 0} devices, ${Object.values(prom).filter(Boolean).length}/${Object.keys(queries).length} reads, at ${out.read_at}`)
