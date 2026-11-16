import { Button } from '@bifrost/ui'
import type { RuntimeMapSelection } from '@/domains/foundation/runtime-map/contracts'
import type { OpsContextResponse } from '@/api/opsContextTypes'

const TRADE_APP_URL = import.meta.env.VITE_TRADE_FRONTEND_URL ?? 'http://127.0.0.1:5173'

export function TenantRuntimeLink({ selection }: { selection: RuntimeMapSelection }) {
  const showTradeReactor = selection?.kind === 'target' && (selection.id.startsWith('api-') || selection.id.includes('socket'))
  return <>
        {showTradeReactor && (
          <Button size="sm" asChild>
            <a
              href={TRADE_APP_URL}
              target="_blank"
              rel="noreferrer"
            >
              Open Trade Reactor
            </a>
          </Button>
        )}
  </>
}

export function TenantProbeHint({ hint }: { hint: OpsContextResponse['probe_hints'][number] | undefined }) {
  return <>
      {hint != null && (
        <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Trade route: <code className="font-mono-tabular">{hint.trade_route}</code> — {hint.hint}
        </p>
      )}
  </>
}
