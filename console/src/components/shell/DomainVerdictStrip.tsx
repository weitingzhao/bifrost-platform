import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import { useDomainStatusObservation } from '@/domains/foundation/observation-access'
import { domainVerdict } from './domainVerdict'
import { TAG_VARIANT, type StatusWord } from '@/lib/shell/stateWords'

const LAMP: Record<StatusWord, 'ok' | 'degraded' | 'fail' | 'unknown'> = {
  ok: 'ok', warn: 'degraded', fail: 'fail', boundary: 'unknown', unknown: 'unknown', pending: 'unknown', info: 'unknown',
}

export function DomainVerdictStrip({ title, domain }: { title: string; domain: string }) {
  const q = useDomainStatusObservation()
  const error = q.error ? (q.error instanceof Error ? q.error.message : 'request failed') : null
  const v = domainVerdict(title, domain, q.data?.domains, error)
  return (
    <OpsVerdictStrip
      compact
      title={title.toUpperCase()}
      ariaLabel={`${title} verdict`}
      lamp={LAMP[v.word]}
      tagLabel={v.word === 'pending' ? '…' : v.word}
      tagVariant={TAG_VARIANT[v.word]}
      summary={v.sentence}
    />
  )
}
