import { shellNavEntry } from '@/lib/shell/consoleRoutes'

function ShellQuestion({ question }: { question: string }) {
  return <p className="m-0 max-w-2xl text-sm text-muted-foreground">{question}</p>
}

/** Placeholder shells. S3–S5 replace the body of the page they own. */
export { StatusPage } from './status/StatusPage'

export function DataPage() {
  return <ShellQuestion question={shellNavEntry('data').question} />
}

export function IbPage() {
  return <ShellQuestion question={shellNavEntry('ib').question} />
}

export { MaintenancePage } from './maintenance/MaintenancePage'

export function ReleasesPage() {
  return <ShellQuestion question={shellNavEntry('releases').question} />
}

export function InfrastructurePage() {
  return <ShellQuestion question={shellNavEntry('infrastructure').question} />
}

export { ProgressPage } from './progress/ProgressPage'
