import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react'

type FoldMap = Record<string, boolean>

type FoldApi = {
  isOpen: (id: string, fallback: boolean) => boolean
  toggle: (id: string, fallback: boolean) => void
  open: (id: string) => void
}

const FoldContext = createContext<FoldApi | null>(null)

/** Fold state sits above the document, so a periodic refresh does not reset it. */
export function FoldProvider({ children }: { children: ReactNode }) {
  const [map, setMap] = useState<FoldMap>({})
  const isOpen = useCallback(
    (id: string, fallback: boolean) => (Object.prototype.hasOwnProperty.call(map, id) ? map[id] === true : fallback),
    [map],
  )
  const toggle = useCallback((id: string, fallback: boolean) => {
    setMap(prev => {
      const current = Object.prototype.hasOwnProperty.call(prev, id) ? prev[id] === true : fallback
      return { ...prev, [id]: !current }
    })
  }, [])
  const open = useCallback((id: string) => {
    setMap(prev => ({ ...prev, [id]: true }))
  }, [])
  const api = useMemo(() => ({ isOpen, toggle, open }), [isOpen, toggle, open])
  return <FoldContext.Provider value={api}>{children}</FoldContext.Provider>
}

export function useFolds(): FoldApi {
  const api = useContext(FoldContext)
  if (api == null) {
    return {
      isOpen: (_id, fallback) => fallback,
      toggle: () => undefined,
      open: () => undefined,
    }
  }
  return api
}

export function useOpenSection(): (id: string) => void {
  const folds = useFolds()
  return useCallback(
    (id: string) => {
      folds.open(id)
      document.getElementById(id)?.scrollIntoView({ block: 'start' })
    },
    [folds],
  )
}

/**
 * One section. Closed content is not mounted, so a clock tick cannot repaint it.
 * The summary line stays one short line.
 */
export function SectionFold({
  id,
  title,
  digest,
  defaultOpen,
  children,
}: {
  id: string
  title: string
  digest?: ReactNode
  defaultOpen: boolean
  children: ReactNode
}) {
  const folds = useFolds()
  const open = folds.isOpen(id, defaultOpen)
  return (
    <div id={id} data-fold={id} className="w-full min-w-0 max-w-full">
      <details open={open} className="min-w-0 max-w-full">
        <summary
          data-testid={`fold-${id}`}
          className="flex min-h-11 cursor-pointer flex-wrap items-center gap-x-2 gap-y-0.5 text-sm"
          onClick={event => {
            event.preventDefault()
            folds.toggle(id, defaultOpen)
          }}
        >
          <span className="font-medium text-foreground">{title}</span>
          {digest != null && digest !== '' ? <span className="text-muted-foreground">{digest}</span> : null}
        </summary>
        {open ? <div className="mt-2 min-w-0 max-w-full">{children}</div> : null}
      </details>
    </div>
  )
}
