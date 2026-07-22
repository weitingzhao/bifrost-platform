import { parseError } from './client'

export type ChecklistItemSignalDto = {
  item_id: string
  signal: string
  detail?: string
  env?: string
}

export type ChecklistDispatchActionDto = {
  item_id: string
  gate: string
  fix_scope?: string
  job_id?: string
  queue_id?: string
  detail?: string
  skipped_d10?: boolean
  at?: string
}

export type ChecklistSignalsResponse = {
  updated_at?: string
  last_run_id?: string
  source?: string
  signals: ChecklistItemSignalDto[]
  last_dispatch?: ChecklistDispatchActionDto[]
  quiet_success_streak?: number
  last_fail_at?: string
  last_all_ok_at?: string
  new_failures?: string[]
}

export type ChecklistKPIsResponse = {
  quiet_success_streak: number
  last_run_id?: string
  updated_at?: string
  last_fail_at?: string
  last_all_ok_at?: string
  new_fail_hint?: string
  last_counts?: {
    at: string
    ok: number
    fail: number
    unknown: number
    all_ok: boolean
  }
}

export async function fetchChecklistSignals(): Promise<ChecklistSignalsResponse> {
  const r = await fetch('/api/v1/checklist/signals')
  if (!r.ok) throw await parseError('checklist signals', r)
  return r.json() as Promise<ChecklistSignalsResponse>
}

export async function fetchChecklistKPIs(): Promise<ChecklistKPIsResponse> {
  const r = await fetch('/api/v1/checklist/kpis')
  if (!r.ok) throw await parseError('checklist kpis', r)
  return r.json() as Promise<ChecklistKPIsResponse>
}

