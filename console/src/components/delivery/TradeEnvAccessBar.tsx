import { useState } from 'react'
import { Check, Copy, ExternalLink } from 'lucide-react'
import { cn } from '@bifrost/ui'
import {
  TRADE_ENV_ACCESS,
  TRADE_INGRESS_USES_VIP,
  type TradeEnvAccess,
  type TradeEnvTier,
} from '@/lib/delivery/tradeEnvAccess'

const ENV_COLOR: Record<TradeEnvTier, string> = {
  DEV: 'text-env-dev',
  STG: 'text-env-stg',
  PROD: 'text-env-prod',
}

function TradeEnvAccessLink({ item }: { item: TradeEnvAccess }) {
  const [copied, setCopied] = useState(false)
  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(item.gateway)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      /* clipboard unavailable — link is still clickable */
    }
  }
  return (
    <span className="inline-flex items-center gap-1.5 text-dense-meta">
      <span className={cn('text-dense-micro font-bold uppercase tracking-wider', ENV_COLOR[item.env])}>
        {item.env}
      </span>
      <a
        href={item.gateway}
        target="_blank"
        rel="noreferrer"
        className="inline-flex items-center gap-0.5 text-muted-foreground hover:text-primary hover:underline"
      >
        {item.gateway.replace(/^https?:\/\//, '').replace(/\/$/, '')}
        <ExternalLink className="h-2.5 w-2.5" />
      </a>
      <button
        type="button"
        onClick={() => void handleCopy()}
        title="Copy gateway URL"
        className="inline-flex h-4 w-4 items-center justify-center rounded text-muted-foreground/50 hover:text-foreground"
      >
        {copied ? <Check className="h-2.5 w-2.5 text-success" /> : <Copy className="h-2.5 w-2.5" />}
      </button>
    </span>
  )
}

/** Always-visible Trade gateway entrypoints (DEV / STG / PROD), mirroring Platform Release's EnvAccessBar. */
export function TradeEnvAccessBar() {
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-dense-meta text-muted-foreground">
      <span className="text-dense-micro font-semibold uppercase tracking-wider text-muted-foreground/70">
        Entrypoints
      </span>
      {TRADE_ENV_ACCESS.map(item => (
        <TradeEnvAccessLink key={item.env} item={item} />
      ))}
      {TRADE_INGRESS_USES_VIP && (
        <span className="text-dense-micro font-semibold uppercase tracking-wider text-primary">VIP</span>
      )}
    </div>
  )
}
