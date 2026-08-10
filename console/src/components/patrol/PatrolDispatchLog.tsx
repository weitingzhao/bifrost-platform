import { useState } from 'react'
import { Button, SegmentControl } from '@bifrost/ui'
import { Languages } from 'lucide-react'
import type { PatrolRun, PatrolSkill } from '@/api/patrol'
import { AGENT_DIALOGUE_LANGUAGE_OPTIONS } from '@/lib/briefing/agentDialogueLanguage'
import { buildPatrolAskAiPack } from '@/lib/patrol/askAiPack'
import { localizePatrolLog, type PatrolOutputLanguage } from '@/lib/patrol/logLanguage'
import { patrolRunLogText } from '@/lib/patrol/runLog'

type CopyState = 'idle' | 'copied' | 'error'

export function PatrolDispatchLog({
  run,
  skill,
  lang,
  onLangChange,
  emptyHint,
}: {
  run: PatrolRun
  skill?: PatrolSkill
  lang: PatrolOutputLanguage
  onLangChange?: (lang: PatrolOutputLanguage) => void
  emptyHint: string
}) {
  const [copyState, setCopyState] = useState<CopyState>('idle')
  const raw = patrolRunLogText(run)
  const display = localizePatrolLog(raw, lang)
  const empty = display.trim() === ''

  const copyForAi = async () => {
    const pack = buildPatrolAskAiPack({ skill, run, logDisplay: display, lang })
    try {
      await navigator.clipboard.writeText(pack)
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2000)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 2500)
    }
  }

  return (
    <div className="rounded-md border border-border bg-background px-2.5 py-2">
      <div className="mb-1.5 flex flex-wrap items-center gap-1.5">
        <span className="text-[var(--text-dense-caption)] font-medium text-muted-foreground">
          Dispatch log · {run.trigger} · {run.result}
        </span>
        {onLangChange != null && (
          <div
            className="inline-flex items-center gap-1 rounded-md border border-border/50 bg-muted/20 px-1 py-0.5"
            title="Log language"
          >
            <Languages className="h-3 w-3 shrink-0 text-muted-foreground" strokeWidth={2} aria-hidden />
            <SegmentControl
              ariaLabel="Patrol log language"
              value={lang}
              onChange={v => onLangChange(v as PatrolOutputLanguage)}
              options={AGENT_DIALOGUE_LANGUAGE_OPTIONS.map(opt => ({
                value: opt.id,
                label: opt.id === 'zh' ? '中文' : 'EN',
              }))}
              size="xs"
            />
          </div>
        )}
        <Button
          size="xs"
          variant="outline"
          className="ml-auto"
          disabled={empty}
          onClick={() => void copyForAi()}
          title="Copy content for AI to analyse"
        >
          {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy for AI'}
        </Button>
      </div>
      {empty ? (
        <p className="text-[var(--text-dense-meta)] text-muted-foreground">{emptyHint}</p>
      ) : (
        <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-words font-mono text-[var(--text-dense-meta)] leading-relaxed text-foreground">
          {display}
        </pre>
      )}
    </div>
  )
}
