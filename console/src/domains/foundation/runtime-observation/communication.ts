import { evidenceThrough, STAGES, STAGE_LABEL, type CommunicationSnapshot } from '@bifrost/ui/communication'
import type { ObservationPanel } from '@bifrost/ui/city/observation'

/** A projection of the shared ledger, not another collector or state machine. No hardware association is inferred. */
export function communicationPanels(snapshot: CommunicationSnapshot, at: string): ObservationPanel[] {
  return [{ id: 'communication-coverage', label: 'Communication coverage', blocks: [{ kind: 'facts', rows: [
    { label: 'Source state', value: snapshot.coverage.state },
    { label: 'Last successful read', value: snapshot.coverage.updatedAt ?? 'Unknown' },
    { label: 'Retention', value: snapshot.coverage.truncated ? 'Bounded — earlier evidence may be absent' : 'Retained evidence only' },
    { label: 'Scope', value: 'Shared ledger. No relationship to the selected hardware is asserted.' },
    { label: 'Source note', value: snapshot.coverage.note },
  ] }] }, ...evidenceThrough(snapshot.exchanges, at).map(exchange => ({
    id: exchange.channel + ':' + exchange.id, label: exchange.label,
    blocks: [{ kind: 'facts' as const, rows: [
      { label: 'Identity', value: exchange.channel + ':' + exchange.id },
      { label: 'From / to', value: `${exchange.from ?? 'Unknown'} / ${exchange.to ?? 'Unknown'}` },
      ...STAGES.map(stage => ({ label: STAGE_LABEL[stage], value: exchange.stages[stage]?.map(e => `${e.id} · ${e.source} · ${e.at}`).join('\n') || 'No evidence' })),
    ] }],
  }))]
}
