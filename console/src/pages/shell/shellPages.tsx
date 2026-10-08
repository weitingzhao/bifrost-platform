import { shellNavEntry } from '@/lib/shell/consoleRoutes'

function ShellQuestion({ question }: { question: string }) {
  return <p className="m-0 max-w-2xl text-sm text-muted-foreground">{question}</p>
}

/** Placeholder shells. S3–S5 replace the body of the page they own. */
export { StatusPage } from './status/StatusPage'

export { DataPage } from './data/DataPage'

export { IbPage } from './ib/IbPage'

export { MaintenancePage } from './maintenance/MaintenancePage'

export function ReleasesPage() {
  return <ShellQuestion question={shellNavEntry('releases').question} />
}

export { InfrastructurePage } from './infrastructure/InfrastructurePage'

export { ProgressPage } from './progress/ProgressPage'
