import { Component, type ReactNode } from 'react'

/**
 * One Bus page block that fails shows why, alone: the rest of the page and
 * the Console go on (2026-11-17: one unknown roster status in one table
 * turned the whole Console white on STG).
 */
export class BusSectionBoundary extends Component<{ name: string; children: ReactNode }, { error: Error | null }> {
  state: { error: Error | null } = { error: null }

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  render() {
    if (this.state.error) {
      return (
        <div role="alert" className="rounded-sm border border-destructive/40 p-2 text-sm text-destructive">
          {`${this.props.name} could not be drawn: ${this.state.error.message}. The rest of the page still works.`}
          <button type="button" className="ml-2 cursor-pointer underline" onClick={() => this.setState({ error: null })}>Try again</button>
        </div>
      )
    }
    return this.props.children
  }
}
