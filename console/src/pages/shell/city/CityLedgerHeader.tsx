import type { ComponentProps } from 'react'
import { ConsoleHeader } from '@/components/ConsoleHeader'
import { crumbsOf } from '@/domains/owner/city/gen2/content/frame'
import { readCityQuery } from '@/domains/owner/city/gen2/content/looks'
import { useCityContent } from './useCityContent'

/**
 * The Console's frame over the City's table (World's gap table item 3, 2026-11-23: the frame said Console while the
 * rows were the City's). The crumbs are the City's own (crumbsOf: place names from the map, not ids) and end in
 * Table, so it reads "City › Delivery › Table". Same reads as CityLedger (one query cache), loaded with the City.
 */
export default function CityLedgerHeader(props: Omit<ComponentProps<typeof ConsoleHeader>, 'crumbs'>) {
  const content = useCityContent()
  const path = readCityQuery(window.location.hash.split('?')[1] ?? '').path
  return <ConsoleHeader {...props} crumbs={[...crumbsOf(content.tree, path), { label: 'Table' }]} />
}
