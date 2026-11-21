import type { Panel } from '@bifrost/ui/city/content'
import { LIGHTS, type DomainLight, type DomainStatusResponse } from '@/domains/foundation/catalog/api/domainStatus'

/**
 * The domains as the map line sees them (GET /api/v1/catalog/status, 2026-11-20): each domain's four
 * lights and its overall light, as the map's workers kept them after their last scan round. City is
 * the map's first reader (perceive first: the reading must be right even when it reads badly); the
 * districts themselves are not changed by it until the Owner says how districts and domains meet.
 *
 * A failed read says the lights are not known; it never shows an empty table as if all were well.
 */
export type DomainsRead = { data?: DomainStatusResponse; error?: unknown }

const toneOf = (value: string) => value === 'bad' ? 'g2-warn' : value === 'good' ? '' : value === 'problem' ? 'g2-warn' : 'g2-unknown'

function Light({ light }: { light: DomainLight | undefined }) {
  const value = light?.value || 'unknown'
  return <td className={toneOf(value)} title={light?.why || undefined}>{value}</td>
}

function DomainsTable({ read }: { read: DomainsRead }) {
  const d = read.data
  if (!d) {
    const why = read.error instanceof Error ? read.error.message : read.error ? String(read.error) : 'reading'
    return <p className="g2-unknown">The map's lights are not known: {why}</p>
  }
  return <>
    <table className="g2-table">
      <thead><tr><th>Domain</th><th>Overall</th>{LIGHTS.map(([k, label]) => <th key={k}>{label}</th>)}</tr></thead>
      <tbody>
        {d.domains.map(row => (
          <tr key={row.id}>
            <td>{row.id}</td>
            <td className={toneOf(row.overall || 'unknown')}>{row.overall || 'unknown'}</td>
            {LIGHTS.map(([k]) => <Light key={k} light={row[k]} />)}
          </tr>
        ))}
      </tbody>
    </table>
    <p className="g2-unknown" title={d.note || undefined}>
      From the map's scan{d.at ? ` at ${new Date(d.at).toLocaleString()}` : ''}{d.commit ? ` (${d.commit.slice(0, 8)})` : ''}. Hover a light for what it was read from.
    </p>
  </>
}

/** The city level's panel of domains; it rides with the town's own panels. */
export const domainsPanel = (read: DomainsRead): Panel => ({
  id: 'domains', title: 'Domains · from the map', dock: 'right', body: <DomainsTable read={read} />,
})
