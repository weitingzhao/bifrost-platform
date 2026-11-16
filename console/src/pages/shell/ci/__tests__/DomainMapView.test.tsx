import '@testing-library/jest-dom/vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { DomainMapView } from '../DomainMapView'
import { contract } from './contract'
afterEach(cleanup)
it('shows source-owned domain attribution and actual CI package tasks from the provider contract', () => {
  render(<DomainMapView config={contract.configDomains()} module="svc" />)
  expect(screen.getByRole('region', { name: 'Business domain map' })).toHaveTextContent('Planning · 1 packages')
  expect(screen.getByText(/svc\/a · assigned/)).toBeInTheDocument()
  expect(screen.getByText(/svc\/b · default-maintainer/)).toBeInTheDocument()
  expect(screen.getAllByText(/Package tasks: test/)).toHaveLength(2)
  expect(screen.getByText(/1 packages across the project remain mixed/)).toBeInTheDocument()
})
it('follows the global technical module choice without inventing frontend business ownership', () => {
  const { rerender } = render(<DomainMapView config={contract.configDomains()} module="svc" />)
  rerender(<DomainMapView config={contract.configDomains()} module="web" />)
  expect(screen.queryByText(/svc\/a · assigned/)).not.toBeInTheDocument()
  expect(screen.getByText(/No Go packages in this execution module/)).toBeInTheDocument()
})
it('keeps missing and unreadable ownership distinct', () => {
  const { rerender } = render(<DomainMapView config={contract.configDeclared()} module="" />)
  expect(screen.getByText('Business domains: mapping not connected')).toBeInTheDocument()
  rerender(<DomainMapView config={{ ...contract.configDeclared(), domain_note: 'invalid directory' }} module="" />)
  expect(screen.getByRole('alert')).toHaveTextContent('invalid directory')
})

it('shows provider-owned frontend domains with shared CI tasks and follows module selection', () => {
 const { rerender } = render(<DomainMapView config={contract.configDomains()} module="web" />)
 expect(screen.getByRole('region', { name: 'Frontend domain map' })).toHaveTextContent('Planning · 1 frontend groups')
 expect(screen.getByText(/web · assigned · 1 files/)).toBeInTheDocument()
 expect(screen.getByText(/Shared CI tasks: check/)).toBeInTheDocument()
 expect(screen.getByText(/not independently tested or deployed/)).toBeInTheDocument()
 rerender(<DomainMapView config={contract.configDomains()} module="svc" />)
 expect(screen.queryByRole('region', { name: 'Frontend domain map' })).not.toBeInTheDocument()
})
it('keeps unreadable frontend inventory separate from Go attribution', () => {
 const config=contract.configDomains()
 render(<DomainMapView config={{...config, domains:{...config.domains!, frontend:undefined, frontend_note:'stale source inventory'}}} module="" />)
 expect(screen.getByRole('alert')).toHaveTextContent('stale source inventory')
 expect(screen.getByText(/svc\/a · assigned/)).toBeInTheDocument()
})
