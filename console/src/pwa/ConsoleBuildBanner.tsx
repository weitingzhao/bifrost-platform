import { Button } from '@bifrost/ui'
import { NEWER_CONSOLE_BANNER } from '@/pwa/consoleBuild'

/** Stays until the owner reloads. The page never reloads itself. */
export function ConsoleBuildBanner({ stale }: { stale: boolean }) {
  if (!stale) return null
  return (
    <div
      role="status"
      data-console-build="stale"
      className="flex w-full min-w-0 flex-col gap-2 rounded-[var(--control-radius)] border border-[var(--card-border)] bg-[var(--field-fill)] px-3 py-2"
    >
      <p id="console-build-reason" className="m-0 text-sm">
        {NEWER_CONSOLE_BANNER}
      </p>
      <Button type="button" className="h-11 w-full sm:h-auto sm:w-auto" onClick={() => window.location.reload()}>
        Reload
      </Button>
    </div>
  )
}
