import { useMemo, useState } from 'react'
import { Button, DenseTag } from '@bifrost/ui'
import { Command, Copy, Check } from 'lucide-react'
import type { MatrixResponse, OpsContextResponse } from '@/api/types'
import type { BriefingUrlState } from '@/lib/briefing/briefingUrlState'
import type { TrackId } from '@/lib/briefing/workTracks'
import { buildCommandIntentStripModel, type CommandIntentChip } from '@/lib/control-room/commandIntent'
import type { MissionSnapshot } from '@/lib/control-room/missionSignals'

interface CommandIntentStripProps {
  snapshot: MissionSnapshot
  matrices: MatrixResponse[]
  context?: OpsContextResponse
  onOpenAgentDesk: (opts?: { prefill: string }) => void
  onOpenBriefing?: (opts?: BriefingUrlState) => void
  onOpenDelivery?: () => void
  onOpenPromote?: () => void
}

async function copyText(text: string): Promise<void> {
  await navigator.clipboard.writeText(text)
}

export function CommandIntentStrip({
  snapshot,
  matrices,
  context,
  onOpenAgentDesk,
  onOpenBriefing,
  onOpenDelivery,
  onOpenPromote,
}: CommandIntentStripProps) {
  const model = useMemo(
    () => buildCommandIntentStripModel({ snapshot, matrices, context }),
    [snapshot, matrices, context],
  )
  const [copiedKey, setCopiedKey] = useState<string | null>(null)

  async function handleCopy(key: string, text: string) {
    await copyText(text)
    setCopiedKey(key)
    window.setTimeout(() => setCopiedKey(null), 2000)
  }

  function runChip(chip: CommandIntentChip) {
    const { action } = chip
    switch (action.type) {
      case 'agent_prefill':
        onOpenAgentDesk({ prefill: action.prefill })
        break
      case 'copy_text':
        void handleCopy(chip.id, action.text)
        break
      case 'open_briefing':
        onOpenBriefing?.({ track: action.track as TrackId })
        break
      case 'open_delivery':
        onOpenDelivery?.()
        break
      case 'open_promote':
        onOpenPromote?.()
        break
    }
  }

  return (
    <section className="command-intent-strip" aria-label="Command intent">
      <div className="command-intent-strip__head">
        <Command size={16} className="command-intent-strip__icon" />
        <div className="command-intent-strip__titles">
          <h4 className="command-intent-strip__title">Command intent</h4>
          <p className="command-intent-strip__desc">
            Mission-scoped actions — send to Agent Desk, copy governance packs, or open Briefing.
          </p>
        </div>
        <DenseTag variant="category">Mode · {model.suggestedMode}</DenseTag>
      </div>

      {model.focusHeadline != null && (
        <p className="command-intent-strip__focus m-0">
          <span className="text-[var(--muted-foreground)]">Spine focus:</span> {model.focusHeadline}
        </p>
      )}

      <div className="command-intent-strip__chips">
        {model.primaryChips.map(chip => (
          <button
            key={chip.id}
            type="button"
            className={[
              'command-intent-chip',
              chip.emphasis === 'primary' ? 'command-intent-chip--primary' : '',
            ]
              .filter(Boolean)
              .join(' ')}
            title={chip.detail}
            onClick={() => runChip(chip)}
          >
            <span className="command-intent-chip__label">{chip.label}</span>
            <span className="command-intent-chip__detail">{chip.detail}</span>
          </button>
        ))}
      </div>

      <div className="command-intent-strip__copy-row">
        <span className="command-intent-strip__copy-label">Copy pack</span>
        {model.copyPacks.map(pack => {
          const key = pack.mode
          const active = copiedKey === key
          return (
            <Button
              key={key}
              variant="ghost"
              size="xs"
              disabled={context == null && pack.mode !== 'Product'}
              onClick={() => void handleCopy(key, pack.text)}
            >
              {active ? <Check size={12} /> : <Copy size={12} />}
              {active ? 'Copied' : pack.label}
            </Button>
          )
        })}
        <Button variant="outline" size="xs" onClick={() => onOpenAgentDesk()}>
          Agent Desk
        </Button>
      </div>
    </section>
  )
}
