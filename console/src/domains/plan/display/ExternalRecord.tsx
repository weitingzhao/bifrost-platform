import type { ExternalWork } from '@/api/planSnapshot'
import { WorkDeliveryStrips } from '@/domains/delivery/display/DeliveryStrip'

export function ExternalRecord({ work }: { work: ExternalWork }) {
  return <li className="[overflow-wrap:anywhere]">{work.description.task} · {work.description.executor} · {work.status}{work.backfilled && ' · Backfilled'}{work.verification && ` · ${work.verification}`}
    {work.report_digest && <span className="block">Report · {work.report_digest}</span>}
    {work.commit_shas?.map(sha => <span className="block" key={sha}>Commit · {sha}</span>)}
    <WorkDeliveryStrips commits={work.commit_shas} />
  </li>
}
