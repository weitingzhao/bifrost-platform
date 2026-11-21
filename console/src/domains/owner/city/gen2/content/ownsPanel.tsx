import type { Panel } from '@bifrost/ui/city/content'
import { DomainOwns, type ReadDomain } from './domainOwns'

/** A domain district's second card: what it owns, read from the map when the district is entered. */
export const ownsPanel = (read: ReadDomain, id: string): Panel => ({
  id: `owns-${id}`, title: 'Owns · from the map', dock: 'right', body: <DomainOwns id={id} read={read} />,
})
