/**
 * GET /market/docs/{slug} — the blueprint and the calibration, served from the
 * plugin package so the repo, the API and this panel are the same file.
 * Read-only; bare fetch through the platform-api proxy like the other GETs.
 */
export type MarketDataDocSlug = 'blueprint' | 'calibration'

export type MarketDataDoc = {
  slug: MarketDataDocSlug
  title: string
  version: string | null
  updated: string | null
  status: string | null
  markdown: string
  path: string
}

export async function fetchMarketDataDoc(slug: MarketDataDocSlug): Promise<MarketDataDoc> {
  const r = await fetch(`/api/v1/plugins/market-data/api/market/docs/${slug}`)
  if (!r.ok) throw new Error(`market docs ${slug}: HTTP ${r.status}`)
  const body = (await r.json()) as { ok: boolean; data: MarketDataDoc }
  if (!body.ok) throw new Error(`market docs ${slug}: not ok`)
  return body.data
}
