import { shellNavEntry } from '@/lib/shell/consoleRoutes'

function ShellQuestion({ question }: { question: string }) {
  return <p className="m-0 max-w-2xl text-sm text-muted-foreground">{question}</p>
}

/** Placeholder shells. S3–S5 replace the body of the page they own. */
export function StatusPage() {
  return <ShellQuestion question={shellNavEntry('status').question} />
}

export function DataPage() {
  return <ShellQuestion question={shellNavEntry('data').question} />
}

export function IbPage() {
  return <ShellQuestion question={shellNavEntry('ib').question} />
}

export function MaintenancePage() {
  return <ShellQuestion question={shellNavEntry('maintenance').question} />
}

export { ReleasesPage } from './releases/ReleasesPage'

export function InfrastructurePage() {
  return <ShellQuestion question={shellNavEntry('infrastructure').question} />
}

export function ProgressPage() {
  return <ShellQuestion question={shellNavEntry('progress').question} />
}
