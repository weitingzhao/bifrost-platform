export type HusbandryLaneView = {
  id: string
  label: string
  verdict: string
  detail: string
  source?: string
}

export type DataHusbandrySnapshot = {
  generated_at: string
  overall: string
  detail: string
  lanes: HusbandryLaneView[]
  note?: string
}

export async function fetchDataHusbandry(): Promise<DataHusbandrySnapshot> {
  const r = await fetch('/api/v1/data-husbandry')
  if (!r.ok) throw new Error(`data-husbandry: HTTP ${r.status}`)
  return r.json() as Promise<DataHusbandrySnapshot>
}
