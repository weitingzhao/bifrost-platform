import { shellNavEntry } from '@/lib/shell/consoleRoutes'

function ShellQuestion({ question }: { question: string }) {
  return <p className="m-0 max-w-2xl text-sm text-muted-foreground">{question}</p>
}

/** Placeholder shells. S3–S5 replace the body of the page they own. */
export function StatusPage() {
  return <ShellQuestion question={shellNavEntry('status').question} />
}

export { DataPage } from './data/DataPage'

export { IbPage } from './ib/IbPage'

export function MaintenancePage() {
  return <ShellQuestion question={shellNavEntry('maintenance').question} />
}

export function ReleasesPage() {
  return <ShellQuestion question={shellNavEntry('releases').question} />
}

export { InfrastructurePage } from './infrastructure/InfrastructurePage'

export function ProgressPage() {
  return <ShellQuestion question={shellNavEntry('progress').question} />
}
