import { SHELL_NAV, formatShellHash } from '@/lib/shell/consoleRoutes'
import { shellDetailItems } from '@/lib/shell/shellNavItems'

/**
 * Every detail page by name, with the question it answers: More on a phone or a tablet, and the
 * foot of System everywhere. Each row is a 44 px link.
 */
export function DetailPageLinks({ label = 'Detail pages' }: { label?: string }) {
  const questions = new Map<string, string>(SHELL_NAV.map(item => [item.id, item.question]))
  return (
    <nav aria-label={label} className="flex flex-col gap-2">
      <h2 className="m-0 text-base font-semibold">{label}</h2>
      <ul className="m-0 flex list-none flex-col gap-1.5 p-0">
        {shellDetailItems().map(item => {
          const Icon = item.icon
          return (
            <li key={item.id}>
              <a
                href={formatShellHash(item.id)}
                className="flex min-h-11 items-center gap-3 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2 text-foreground no-underline"
              >
                {Icon != null && <Icon className="size-5 shrink-0 text-muted-foreground" aria-hidden />}
                <span className="flex min-w-0 flex-col">
                  <span className="font-semibold">{item.label}</span>
                  <span className="text-sm text-muted-foreground">{questions.get(item.id)}</span>
                </span>
              </a>
            </li>
          )
        })}
      </ul>
    </nav>
  )
}
