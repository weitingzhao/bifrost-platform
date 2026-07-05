import { cn } from '@bifrost/ui'
import { Check, Copy, ExternalLink } from 'lucide-react'
import { useState } from 'react'
import { PLATFORM_PROD_URLS, PLATFORM_STG_URLS } from '@/lib/delivery/deliverPlatformPhases'

interface EnvAccess {
  env: 'STG' | 'PROD'
  label: string
  console: string
  apiHealth: string
}

const ENV_ACCESS: EnvAccess[] = [
  {
    env: 'STG',
    label: 'Ops Console STG',
    console: PLATFORM_STG_URLS.console,
    apiHealth: PLATFORM_STG_URLS.apiHealth,
  },
  {
    env: 'PROD',
    label: 'Ops Console PROD',
    console: PLATFORM_PROD_URLS.console,
    apiHealth: PLATFORM_PROD_URLS.apiHealth,
  },
]

function EnvAccessLink({ item }: { item: EnvAccess }) {
  const [copied, setCopied] = useState(false)
  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(item.console)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      /* clipboard unavailable */
    }
  }
  return (
    <span className="inline-flex items-center gap-1.5 text-dense-meta">
      <span
        className={cn(
          'text-dense-micro font-bold uppercase tracking-wider',
          item.env === 'PROD' ? 'text-env-prod' : 'text-env-stg',
        )}
      >
        {item.env}
      </span>
      <a
        href={item.console}
        target="_blank"
        rel="noreferrer"
        className="inline-flex items-center gap-0.5 text-muted-foreground hover:text-primary hover:underline"
      >
        {item.console.replace(/^https?:\/\//, '')}
        <ExternalLink className="h-2.5 w-2.5" />
      </a>
      <button
        type="button"
        onClick={() => void handleCopy()}
        title="Copy console URL"
        className="inline-flex h-4 w-4 items-center justify-center rounded text-muted-foreground/50 hover:text-foreground"
      >
        {copied ? <Check className="h-2.5 w-2.5 text-success" /> : <Copy className="h-2.5 w-2.5" />}
      </button>
    </span>
  )
}

/** Platform Console STG/PROD entrypoints */
export function ReleaseEnvAccessBar() {
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-dense-meta text-muted-foreground">
      {ENV_ACCESS.map(item => (
        <EnvAccessLink key={item.env} item={item} />
      ))}
    </div>
  )
}
