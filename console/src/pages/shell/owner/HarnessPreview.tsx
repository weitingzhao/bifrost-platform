import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Button } from '@bifrost/ui'
import { ExternalLink } from 'lucide-react'
import { serverFetch } from '@/api/serverContact'
import { shellSelfHealthQuery } from '@/domains/foundation/shell/useShellStatusLine'
import { Card } from './OwnerParts'

const localHosts = ['127.0.0.1', 'localhost', '[::1]']
const configPath = '/runtime/harness-previews.json'

function previewURL(value: string | undefined, consoleOrigin: string) {
  if (!value) return null
  try {
    const url = new URL(value)
    const localPreview = url.protocol === 'http:' && localHosts.includes(url.hostname) && localHosts.includes(new URL(consoleOrigin).hostname)
    if (url.origin === consoleOrigin || url.username || url.password || url.search || url.hash) return null
    return url.protocol === 'https:' || localPreview ? url.href : null
  } catch {
    return null
  }
}

const harnesses = [
  { id: 'opencode', name: 'OpenCode' },
  { id: 'deepseek', name: 'DeepSeek Harness' },
] as const

type Harness = (typeof harnesses)[number]['id']
type PreviewConfig = Partial<Record<Harness, string>>

const localConfig: PreviewConfig = {
  opencode: import.meta.env.VITE_STG_OPENCODE_URL,
  deepseek: import.meta.env.VITE_STG_DEEPSEEK_HARNESS_URL,
}

async function loadPreviewConfig(signal: AbortSignal): Promise<PreviewConfig> {
  const response = await serverFetch(configPath, { signal, cache: 'no-store' })
  if (!response.ok || !response.headers.get('content-type')?.startsWith('application/json')) return {}
  const value: unknown = await response.json()
  if (!value || typeof value !== 'object') return {}
  return {
    opencode: 'opencode' in value && typeof value.opencode === 'string' ? value.opencode : undefined,
    deepseek: 'deepseek' in value && typeof value.deepseek === 'string' ? value.deepseek : undefined,
  }
}

export function HarnessPreview() {
  const health = useQuery(shellSelfHealthQuery)
  const environment = health.data?.viewer_env
  const enabled = !health.isError && (environment === 'stg' || environment === 'prod')
  const local = environment === 'stg' && localHosts.includes(window.location.hostname)
  const config = useQuery({
    queryKey: ['harness-preview-config', environment],
    queryFn: ({ signal }) => loadPreviewConfig(signal),
    enabled: enabled && !local,
    staleTime: 60_000,
    retry: false,
  })
  const [selected, setSelected] = useState<Harness | null>(null)
  if (!enabled) return null

  const source = local ? localConfig : config.data
  const launchers = harnesses.flatMap(({ id, name }) => {
    const url = previewURL(source?.[id], window.location.origin)
    return url ? [{ id, name, url }] : []
  })
  if (launchers.length === 0) return null

  const active = launchers.find(launcher => launcher.id === selected)
  return <div id="local-harnesses"><Card label="Local harnesses" title="Local harnesses">
    <div className="flex flex-wrap items-center gap-2" role="group" aria-label="Choose a harness">
      {launchers.map(({ id, name }) => <Button key={id} type="button" className="min-h-11" variant={selected === id ? 'default' : 'outline'}
        aria-pressed={selected === id} onClick={() => setSelected(id)}>{name}</Button>)}
      {active && <Button type="button" variant="ghost" onClick={() => setSelected(null)}>Close preview</Button>}
    </div>
    {active && <>
      <a href={active.url} target="_blank" rel="noopener noreferrer"
        className="inline-flex min-h-10 w-fit items-center gap-1.5 text-sm font-medium">
        Open {active.name} in a new tab <ExternalLink aria-hidden="true" size={14} />
      </a>
      <iframe key={active.id} src={active.url} title={active.name}
        sandbox="allow-scripts allow-same-origin allow-forms allow-downloads allow-popups"
        referrerPolicy="no-referrer"
        className="h-[min(74dvh,56rem)] min-h-80 w-full rounded-[var(--control-radius)] border border-[var(--card-border)] bg-[var(--card-fill)]" />
    </>}
  </Card></div>
}
