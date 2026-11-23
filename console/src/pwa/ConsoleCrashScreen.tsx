import { Component, useState, type ErrorInfo, type ReactNode } from 'react'
import { crashReport, crashReportText, type CrashReport } from '@/pwa/consoleCrash'

/** The page that replaces a white screen; why it exists is in consoleCrash.ts. */
export function ConsoleCrashScreen({ report }: { report: CrashReport }) {
  const [copied, setCopied] = useState(false)
  const text = crashReportText(report)
  return (
    <main role="alert" data-console-crash={report.chunk ? 'chunk' : 'render'} className="mx-auto flex min-h-dvh w-full max-w-2xl flex-col gap-3 bg-background p-4 text-foreground">
      <h1 className="m-0 text-lg font-semibold">The Console stopped drawing this page</h1>
      <p className="m-0 text-sm">
        {report.chunk
          ? 'Part of this page’s code could not be loaded. Usually a newer Console was deployed after this page opened, and the old files are gone. Reload to get the new one.'
          : 'An error stopped the page. The details below are what a diagnosis needs: take a screenshot before reloading.'}
      </p>
      <pre className="m-0 overflow-x-auto whitespace-pre-wrap break-words rounded-[var(--control-radius)] border border-[var(--card-border)] bg-[var(--field-fill)] p-3 text-xs">{text}</pre>
      <div className="flex flex-wrap gap-2">
        <button type="button" className="h-11 cursor-pointer rounded-[var(--control-radius)] border border-[var(--card-border)] px-4 text-sm" onClick={() => window.location.reload()}>
          Reload
        </button>
        <button
          type="button"
          className="h-11 cursor-pointer rounded-[var(--control-radius)] border border-[var(--card-border)] px-4 text-sm"
          onClick={() => {
            void navigator.clipboard?.writeText(text).then(() => setCopied(true), () => setCopied(false))
          }}
        >
          {copied ? 'Copied' : 'Copy details'}
        </button>
      </div>
    </main>
  )
}

/** Outermost boundary: whatever throws below it shows the crash screen instead of a white page. */
export class ConsoleRootBoundary extends Component<{ children: ReactNode }, { report: CrashReport | null }> {
  state: { report: CrashReport | null } = { report: null }

  static getDerivedStateFromError(error: unknown) {
    return { report: crashReport(error, null) }
  }

  componentDidCatch(error: unknown, info: ErrorInfo) {
    this.setState({ report: crashReport(error, info) })
  }

  render() {
    if (this.state.report) return <ConsoleCrashScreen report={this.state.report} />
    return this.props.children
  }
}

/** Visible while a full-screen look's chunk loads, so a slow chunk is not a blank screen. */
export function RouteLoading({ place, full = false }: { place: string; full?: boolean }) {
  return (
    <div role="status" data-route-loading={place} className={`flex ${full ? 'min-h-dvh' : 'min-h-48'} w-full items-center justify-center bg-background text-sm text-muted-foreground`}>
      {`Loading ${place}…`}
    </div>
  )
}
