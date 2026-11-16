import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { MonitoringCoverageStrip } from './MonitoringCoverageStrip'

afterEach(cleanup)
// 2026-11-16 domain extraction: preserve the former observability strip's status and navigation contract.
describe('Foundation monitoring coverage', () => {
  it('keeps unknown distinct from configured and preserves navigation callbacks', () => {
    const observability = vi.fn(), cluster = vi.fn()
    render(<MonitoringCoverageStrip layerB={undefined} prometheusConfigured={false} onOpenObservability={observability} onOpenCluster={cluster} />)
    expect(screen.getByText('Unknown')).toBeTruthy()
    expect(screen.getByText('not configured')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'View Observability' }))
    fireEvent.click(screen.getByRole('button', { name: 'Rocket → Cluster' }))
    expect(observability).toHaveBeenCalledOnce()
    expect(cluster).toHaveBeenCalledOnce()
  })
  it.each([['ready', 'Ready'], ['partial', 'Partial'], ['not_installed', 'Not installed']] as const)('retains %s wording', (layerB, label) => {
    render(<MonitoringCoverageStrip layerB={layerB} prometheusConfigured />)
    expect(screen.getByText(label)).toBeTruthy()
    expect(screen.getByText('configured')).toBeTruthy()
    expect(screen.queryByRole('button')).toBeNull()
  })
  it('keeps loading from presenting a completed reading', () => {
    render(<MonitoringCoverageStrip layerB="ready" prometheusConfigured loading />)
    expect(screen.getAllByText('…')).toHaveLength(2)
    expect(screen.queryByText('Ready')).toBeNull()
  })
})
