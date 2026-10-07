import { authedFetch } from './client'

export type ConsoleHost = {
  id: string
  label: string
  host: string
  user: string
  port: number
  group: string
  reachable: boolean
  jump_label?: string
}

export async function fetchConsoleHosts(): Promise<ConsoleHost[]> {
  const r = await fetch('/api/v1/console/hosts')
  if (!r.ok) throw new Error(`console hosts: HTTP ${r.status}`)
  const data = (await r.json()) as { hosts: ConsoleHost[] }
  return data.hosts ?? []
}

/** One-use, 30-second operator ticket for one host's shell (TD-203). */
export async function requestConsoleTicket(host: ConsoleHost): Promise<string> {
  const r = await authedFetch(
    'console ticket',
    `/api/v1/console/ws-ticket?${new URLSearchParams({ node: host.id }).toString()}`,
    { method: 'POST' },
  )
  const data = (await r.json()) as { ticket: string }
  return data.ticket
}

export function consoleWebSocketUrl(host: ConsoleHost, ticket: string): string {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const params = new URLSearchParams({ node: host.id, host: host.host, ticket })
  return `${proto}//${window.location.host}/api/v1/console/ws?${params.toString()}`
}
