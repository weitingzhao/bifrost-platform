import { Button, cn } from '@bifrost/ui'
import { Columns2, PanelLeftClose, PanelRightClose } from 'lucide-react'
import type { ReactNode } from 'react'
import { useEffect, useState } from 'react'

export type LaneOperateSplitMode = 'split' | 'primary' | 'support'

const DEFAULT_STORAGE_KEY = 'bifrost.console.laneOperateSplit'

function readStoredMode(key: string): LaneOperateSplitMode {
  try {
    const raw = localStorage.getItem(key)
    if (raw === 'split' || raw === 'primary' || raw === 'support') return raw
  } catch {
    /* ignore */
  }
  return 'split'
}

function LaneOperateSplitRail({
  mode,
  onModeChange,
}: {
  mode: Exclude<LaneOperateSplitMode, 'split'>
  onModeChange: (mode: LaneOperateSplitMode) => void
}) {
  const focusingPrimary = mode === 'primary'
  return (
    <div
      role="separator"
      aria-orientation="vertical"
      className="flex w-7 shrink-0 flex-col items-center justify-center gap-1.5 border-x border-border/50 bg-secondary/30 py-2"
    >
      <Button
        type="button"
        variant="ghost"
        size="xs"
        className="h-7 w-6 px-0"
        title="Restore 6 / 4 split"
        aria-label="Restore 6 / 4 split"
        onClick={() => onModeChange('split')}
      >
        <Columns2 className="h-3.5 w-3.5" aria-hidden />
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="xs"
        className="h-7 w-6 px-0"
        title={focusingPrimary ? 'Show support only' : 'Show release only'}
        aria-label={focusingPrimary ? 'Show support only' : 'Show release only'}
        onClick={() => onModeChange(focusingPrimary ? 'support' : 'primary')}
      >
        {focusingPrimary ? (
          <PanelLeftClose className="h-3.5 w-3.5" aria-hidden />
        ) : (
          <PanelRightClose className="h-3.5 w-3.5" aria-hidden />
        )}
      </Button>
      <span
        className="mt-1 max-w-[0.85rem] text-center text-[9px] font-semibold uppercase leading-tight tracking-wider text-muted-foreground/60 [writing-mode:vertical-rl]"
      >
        {focusingPrimary ? 'Support' : 'Release'}
      </span>
    </div>
  )
}

function LaneOperateSplitDivider({
  onFocusPrimary,
  onFocusSupport,
}: {
  onFocusPrimary: () => void
  onFocusSupport: () => void
}) {
  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label="Release / support split — click to focus one side"
      className="group flex w-7 shrink-0 flex-col items-center justify-center gap-1.5 border-x border-border/50 bg-secondary/20 py-2 transition-colors hover:bg-secondary/40"
    >
      <Button
        type="button"
        variant="ghost"
        size="xs"
        className="h-7 w-6 px-0 opacity-70 group-hover:opacity-100"
        title="Focus release + latest run (hide support)"
        aria-label="Focus release + latest run"
        onClick={onFocusPrimary}
      >
        <PanelRightClose className="h-3.5 w-3.5" aria-hidden />
      </Button>
      <div className="h-8 w-px bg-border/70" aria-hidden />
      <Button
        type="button"
        variant="ghost"
        size="xs"
        className="h-7 w-6 px-0 opacity-70 group-hover:opacity-100"
        title="Focus support (hide release)"
        aria-label="Focus support"
        onClick={onFocusSupport}
      >
        <PanelLeftClose className="h-3.5 w-3.5" aria-hidden />
      </Button>
    </div>
  )
}

/**
 * Fixed 6/4 lane operate layout: primary (Release + Active Run) | support.
 * Divider is not draggable — click icons to focus one pane or restore split.
 * Below `lg`, stacks vertically (no split chrome).
 */
export function LaneOperateSplit({
  primary,
  support,
  storageKey = DEFAULT_STORAGE_KEY,
}: {
  primary: ReactNode
  support: ReactNode
  storageKey?: string
}) {
  const [mode, setMode] = useState<LaneOperateSplitMode>(() => readStoredMode(storageKey))

  useEffect(() => {
    try {
      localStorage.setItem(storageKey, mode)
    } catch {
      /* ignore */
    }
  }, [mode, storageKey])

  return (
    <>
      <div
        className={cn(
          'hidden min-h-0 w-full min-w-0 lg:grid lg:items-start',
          mode === 'split' && 'lg:grid-cols-[minmax(0,6fr)_auto_minmax(0,4fr)]',
          mode === 'primary' && 'lg:grid-cols-[minmax(0,1fr)_auto]',
          mode === 'support' && 'lg:grid-cols-[auto_minmax(0,1fr)]',
        )}
      >
        {(mode === 'split' || mode === 'primary') && (
          <div className="flex min-w-0 flex-col gap-3 pr-1">{primary}</div>
        )}

        {mode === 'split' ? (
          <LaneOperateSplitDivider
            onFocusPrimary={() => setMode('primary')}
            onFocusSupport={() => setMode('support')}
          />
        ) : (
          <LaneOperateSplitRail mode={mode} onModeChange={setMode} />
        )}

        {(mode === 'split' || mode === 'support') && (
          <div className="flex min-w-0 flex-col gap-3 pl-1">{support}</div>
        )}
      </div>

      <div className="flex flex-col gap-3 lg:hidden">
        {primary}
        {support}
      </div>
    </>
  )
}
