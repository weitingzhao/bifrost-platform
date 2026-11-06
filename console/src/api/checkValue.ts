import { serverFetch } from '@/api/serverContact'
import { parseError } from './client'

export type CheckValueCell = {
  week: string
  ran: number | null
  caught: number | null
  unique: number | null
  noise: number | null
  minutes: number | null
  owner_minutes: number | null
  unknown: Record<string, string>
}
export type CheckValueResponse = {
  as_of: string
  weeks: string[]
  rows: {
    key: string
    kind: 'check' | 'retry'
    label: string
    order: number
    cells: CheckValueCell[]
    mark: null | { kind: 'delete'; since: string; mission?: string }
  }[]
}
export type LeakValueCell = {
  week: string
  stg: number | null
  prod: number | null
  unknown: Record<string, string>
}
export type LeakValueResponse = {
  as_of: string
  weeks: string[]
  rows: {
    place: string
    label: string
    cells: LeakValueCell[]
    mark: null | { kind: 'add_back'; since: string; mission?: string }
  }[]
}
export class CheckValueNotDeployed extends Error {}

async function read<T>(route: string): Promise<T> {
  const response = await serverFetch(`/api/v1/agentrt/${route}`)
  if (response.status === 404) throw new CheckValueNotDeployed(`${route} route is not deployed yet.`)
  if (!response.ok) throw await parseError(route, response)
  return response.json() as Promise<T>
}
export const fetchCheckValue = () => read<CheckValueResponse>('check-value')
export const fetchLeakValue = () => read<LeakValueResponse>('leak-value')
