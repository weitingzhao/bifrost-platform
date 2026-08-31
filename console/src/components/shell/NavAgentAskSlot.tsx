import { createContext, useContext, useState, type MouseEvent, type ReactNode } from 'react'
import { cn, Popover, PopoverContent, PopoverTrigger } from '@bifrost/ui'
import { Check, Loader2, Sparkles } from 'lucide-react'
import { signalColor, type Signal } from '@/lib/control-room/missionSignals'
import {
  gatherNavAgentPack,
  isNavAgentCapable,
  navAgentNeedsAsk,
} from '@/lib/nav/navAgentCapability'

type AskCtx = {
  signalFor: (tabId: string) => Signal | null
}

const NavAgentAskContext = createContext<AskCtx | null>(null)

export function NavAgentAskProvider({
  signalFor,
  children,
}: {
  signalFor: (tabId: string) => Signal | null
  children: ReactNode
}) {
  return <NavAgentAskContext.Provider value={{ signalFor }}>{children}</NavAgentAskContext.Provider>
}

type CopyState = 'idle' | 'busy' | 'copied' | 'error'

async function writeClipboard(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text)
    return
  } catch {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.setAttribute('readonly', '')
    ta.style.position = 'fixed'
    ta.style.left = '-9999px'
    document.body.appendChild(ta)
    ta.focus()
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    if (!ok) throw new Error('clipboard write failed')
  }
}

/**
 * Trailing Sparkles on nav rows that have a page-independent Ask-for-Agent pack.
 * Always visible (capability mark). Colored + louder when the lamp is not green.
 * Must sit as a sibling of the row link — never nested inside SidebarMenuSubButton.
 */
export function NavAgentAskSlot({
  itemId,
  collapsed = false,
}: {
  itemId: string
  collapsed?: boolean
}) {
  const ctx = useContext(NavAgentAskContext)
  const [copyState, setCopyState] = useState<CopyState>('idle')
  const [fallbackText, setFallbackText] = useState<string | null>(null)

  if (ctx == null || !isNavAgentCapable(itemId)) return null

  const signal = ctx.signalFor(itemId)
  const needsAsk = navAgentNeedsAsk(signal)
  const busy = copyState === 'busy'

  async function handleAsk(event: MouseEvent) {
    event.preventDefault()
    event.stopPropagation()
    if (busy) return
    setCopyState('busy')
    setFallbackText(null)
    try {
      const text = await gatherNavAgentPack(itemId)
      try {
        await writeClipboard(text)
        setCopyState('copied')
        window.setTimeout(() => setCopyState('idle'), 2000)
      } catch {
        setFallbackText(text)
        setCopyState('error')
      }
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 3000)
    }
  }

  const title =
    copyState === 'copied'
      ? 'Copied diagnose pack — paste into Agent IDE'
      : copyState === 'error' && fallbackText != null
        ? 'Clipboard blocked — pack is in the popover, copy from there'
        : copyState === 'error'
          ? 'Copy failed'
          : copyState === 'busy'
            ? 'Gathering diagnose pack…'
            : needsAsk
              ? 'Ask for Agent — copy diagnose pack (same as the page button)'
              : 'Ask for Agent available — copy diagnose pack'

  const color =
    copyState === 'copied'
      ? 'var(--color-lamp-green)'
      : copyState === 'error'
        ? 'var(--color-lamp-red)'
        : needsAsk && signal != null
          ? signalColor(signal)
          : undefined

  const trigger = (
    <button
      type="button"
      data-nav-agent-ask={itemId}
      data-nav-agent-needs-ask={needsAsk ? 'true' : 'false'}
      className={cn(
        'inline-flex shrink-0 items-center justify-center rounded-sm border-0 bg-transparent p-0',
        'hover:bg-sidebar-accent/60',
        collapsed ? 'absolute -right-0.5 -top-0.5 size-3' : 'size-4',
        !needsAsk && copyState === 'idle' && 'opacity-60',
      )}
      style={color != null ? { color } : undefined}
      title={title}
      aria-label={title}
      onClick={e => void handleAsk(e)}
    >
      {copyState === 'busy' ? (
        <Loader2 className={cn('animate-spin', collapsed ? 'size-2.5' : 'size-3')} aria-hidden />
      ) : copyState === 'copied' ? (
        <Check className={collapsed ? 'size-2.5' : 'size-3'} aria-hidden />
      ) : (
        <Sparkles
          className={cn(collapsed ? 'size-2.5' : 'size-3', !needsAsk && 'text-muted-foreground')}
          aria-hidden
        />
      )}
    </button>
  )

  if (fallbackText == null) return trigger

  return (
    <Popover open onOpenChange={open => { if (!open) { setFallbackText(null); setCopyState('idle') } }}>
      <PopoverTrigger asChild>{trigger}</PopoverTrigger>
      <PopoverContent
        side="right"
        align="start"
        className="w-[min(28rem,calc(100vw-4rem))] p-2"
        onClick={e => e.stopPropagation()}
      >
        <p className="m-0 mb-1.5 text-[var(--text-dense-caption)] text-muted-foreground">
          Clipboard blocked. Select and copy this diagnose pack into Agent IDE.
        </p>
        <textarea
          readOnly
          value={fallbackText}
          className="h-48 w-full resize-y rounded-sm border border-border bg-background p-1.5 font-mono text-[var(--text-dense-caption)]"
          onFocus={e => e.currentTarget.select()}
        />
      </PopoverContent>
    </Popover>
  )
}
