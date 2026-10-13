import { Button } from '@bifrost/ui'
import { NEWER_CONSOLE_BANNER } from '@/pwa/consoleBuild'

/** Shown instead of the reload prompt while a decision POST has not returned. */
export const DECISION_IN_FLIGHT_BANNER =
  'A newer Console has been deployed. Wait for this decision to finish before reloading.'

/** Stays until the owner reloads. The page never reloads itself. */
export function ConsoleBuildBanner({ stale, holdReload = false }: { stale: boolean; holdReload?: boolean }) {
  if (!stale) return null
  const waiting = holdReload
  return (
    <div
      role="status"
      data-console-build="stale"
      className="flex w-full min-w-0 flex-col gap-2 rounded-[var(--control-radius)] border border-[var(--card-border)] bg-[var(--field-fill)] px-3 py-2"
    >
      <p id="console-build-reason" className="m-0 text-sm">
        {waiting ? DECISION_IN_FLIGHT_BANNER : NEWER_CONSOLE_BANNER}
      </p>
      <Button
        type="button"
        className="h-11 w-full sm:h-auto sm:w-auto"
        disabled={waiting}
        onClick={() => {
          if (waiting) return
          window.location.reload()
        }}
      >
        Reload
      </Button>
    </div>
  )
}
