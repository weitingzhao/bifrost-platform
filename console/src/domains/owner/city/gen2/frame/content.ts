import type { ReactNode } from 'react'
import type { MenuItem, PanelSize } from './parts'
import type { Place } from './places'
import type { Link } from './links'

/**
 * The contract between the frame and its content (the Owner, 2026-11-19: the frame is a framework,
 * the places and their business are plug-ins; each upgrades on its own). The frame knows only this:
 *
 *   tree    the places: town → districts → buildings → rooms, each with a name, a question and a size
 *   panels  the tables and charts beside a place (its dashboard sections), each docked left or right
 *   picked  the card of a picked room
 *   functions  the content's functions, for the menu along the bottom: one way into the system,
 *           beside the levels (by domain); picking one lights up what serves it
 *
 * Content never reaches into the frame's insides, and the frame never imports content: a test in
 * frame/__tests__ holds that line.
 */

/** A table, chart or card beside the middle. */
export type Panel = {
  id: string
  title: string
  /** Which bottom corner it docks in on a wide screen: charts on the left, tables and cards on the right. */
  dock: 'left' | 'right'
  /**
   * What it shows. A function gets the size it is drawn at, so it can give its compact form in a card
   * (tabs as a Select, fewer rows a page) and its full form in the middle. Use the frame's parts
   * (parts.tsx) for tabs, drop-downs, buttons, dialogs, paging and scrolling.
   */
  body: ReactNode | ((at: { size: PanelSize }) => ReactNode)
  /** Secondary actions, in the ⋯ menu at the right of the panel's header. */
  menu?: MenuItem[]
}

export type CityContent = {
  tree: Place
  panels: (place: Place, open: (child: Place) => void) => Panel[]
  picked: (item: Place) => Panel
  functions?: { id: string; label: string }[]
  /** How its places talk to one another, and by which way (links.ts): the frame draws it at each level's grain. */
  links?: Link[]
}
