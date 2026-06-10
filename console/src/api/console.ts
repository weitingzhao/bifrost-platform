export type ConsoleHost = {
  id: string
  label: string
  host: string
  user: string
  port: number
  group: string
  reachable: boolean
}

export async function fetchConsoleHosts(): Promise<ConsoleHost[]> {
  const r = await fetch('/api/v1/console/hosts')
  if (!r.ok) throw new Error(`console hosts: HTTP ${r.status}`)
  const data = (await r.json()) as { hosts: ConsoleHost[] }
  return data.hosts ?? []
}

export function consoleWebSocketUrl(host: ConsoleHost): string {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const params = new URLSearchParams({ node: host.id, host: host.host })
  return `${proto}//${window.location.host}/api/v1/console/ws?${params.toString()}`
}
