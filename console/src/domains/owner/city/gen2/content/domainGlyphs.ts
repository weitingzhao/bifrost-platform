/**
 * The one table of which world glyph stands for which domain (箭10 2026-11-23, «one world», brand layer item 6:
 * the same domain wears the same icon on every view). The glyphs are the engine's (@bifrost/ui/city/worldGlyphs);
 * this content only says which stands for which, as a player picks a character. The tech tree reads it; Signals and
 * the Console read the same table, never a second one.
 *
 * Each choice says what the domain is for (default, the Owner may veto):
 */
export const DOMAIN_GLYPH: Record<string, string> = {
  foundation: 'layers', // what everything stands on
  plan: 'blueprint', // the plan
  lineage: 'branch', // which commit came from where
  verification: 'shield', // CI: what is let in
  delivery: 'rocket', // releases
  approvals: 'gavel', // the Owner's rulings
  compute: 'chip', // the machines
  payload: 'plug', // the tenants plugged in
  coordination: 'signal', // the bus, the letters, the metrics
  crew: 'people', // who sits which seat
  station: 'wrench', // the workshop
  audit: 'gauge', // measuring the work
  triage: 'funnel', // sorting what comes in
  assembly: 'gears', // the layer that wires the domains together
}
