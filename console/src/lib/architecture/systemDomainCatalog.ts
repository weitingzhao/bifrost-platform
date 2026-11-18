/**
 * System Domain catalog — Apollo eight-domain taxonomy (Console sidebar planes).
 *
 * Research is a first-class **domain** (D13 OLAP write boundary) and an
 * **instrument** on the Satellite vehicle (display-host = Trade) — see
 * payloadConstellationCatalog.ts. Domain identity ≠ top-level Ops sidebar group:
 * Launch/Ops nest Research under Satellite. Subcontractors supply data (Polygon / IB Flex → raw_*); Research consumes
 * raw_market.* and produces dw_stock.* / features.*.
 *
 * Authoritative for Ops Console → Governance → Blueprint / Agent Protocol,
 * and for Defects / Audit / Agent scope projection onto a shared mental model.
 *
 * Distinct from AgentTaskDomain (Operator|Platform|Trade|…) which classifies
 * remediation *task subjects*, not Console system domains.
 */

import type { DenseTagVariant } from '@bifrost/ui'
import type { LucideIcon } from 'lucide-react'
import {
  BookOpen,
  Bot,
  Building2,
  Gauge,
  Handshake,
  Microscope,
  Rocket,
  Satellite,
} from 'lucide-react'

/** Kebab ids — stable for filters, maps, and LLM packs. */
export type SystemDomainId =
  | 'mission-control'
  | 'rocket'
  | 'ground-systems'
  | 'satellite'
  | 'research'
  | 'subcontractors'
  | 'engineer'
  | 'governance'

/** Sidebar plane labels. The old consoleNavConfig module is gone; these strings are the catalog. */
export type ConsoleNavPlane =
  | 'Mission Control'
  | 'Rocket'
  | 'Ground Systems'
  | 'Satellite'
  | 'Research'
  | 'Subcontractors'
  | 'Plugin'
  | 'Engineer'
  | 'Governance'

export type SystemDomainRow = {
  id: SystemDomainId
  /** Display label — equals ConsoleNavPlane / sidebar group label. */
  label: ConsoleNavPlane
  purpose: string
}

/**
 * Apollo eight domains — label strings are ConsoleNavPlane.
 * Seven domains appear in the sidebar rail; Governance
 * is reached via the shell User menu (still a first-class plane for catalogs).
 */
export const SYSTEM_DOMAINS: SystemDomainRow[] = [
  {
    id: 'mission-control',
    label: 'Mission Control',
    purpose:
      'Cross-domain ops hub — Task CC (execute) → Control Room (posture) → Observability (health); Defects & Audit (collapsible records); topology drill-down sheet',
  },
  {
    id: 'rocket',
    label: 'Rocket',
    purpose: 'Ops Platform itself — K8s cluster (incl. Facility constraints), Rocket Health, Launch Rocket, platform CI/CD',
  },
  {
    id: 'ground-systems',
    label: 'Ground Systems',
    purpose: 'Infrastructure — network (UniFi); SSH via shell Operator Dock Console slot. K3s nodes → Rocket → Cluster',
  },
  {
    id: 'satellite',
    label: 'Satellite',
    purpose:
      'On-orbit vehicle — Trade display-host (bus, health, Deploy Satellite) + instrument Launch children (Research); constellation edges in payloadConstellationCatalog',
  },
  {
    id: 'research',
    label: 'Research',
    purpose:
      'OLAP instrument (decision) on Satellite — Golden Source, Feature Store, dbt marts, Research API + Copilot; published via Satellite · Research (bifrost-deliver-research); D13 domain id retained',
  },
  {
    id: 'subcontractors',
    label: 'Subcontractors',
    purpose:
      'External plugins — Plugin Gallery observes IB Gateway + Market Data (pipeline + optional readiness_rollup); publish via Launch Plugin',
  },
  {
    id: 'engineer',
    label: 'Engineer',
    purpose: 'AI Agent workspace — Desk, Operator Plane, trust (fate-isolated)',
  },
  {
    id: 'governance',
    label: 'Governance',
    purpose: 'Reference library — Blueprint, Agent Protocol, standards, AI strategy',
  },
]

/** Outline DenseTag colors for Domain chips (distinct from Origin filled chips). */
export const SYSTEM_DOMAIN_VARIANT: Record<SystemDomainId, DenseTagVariant> = {
  'mission-control': 'warning',
  rocket: 'info',
  'ground-systems': 'success',
  satellite: 'category',
  research: 'success',
  subcontractors: 'neutral',
  engineer: 'info',
  governance: 'neutral',
}

/** Same Lucide icons as CONSOLE_NAV_GROUPS — Domain chips stay visually distinct from kind tags. */
export const SYSTEM_DOMAIN_ICON: Record<SystemDomainId, LucideIcon> = {
  'mission-control': Gauge,
  rocket: Rocket,
  'ground-systems': Building2,
  satellite: Satellite,
  research: Microscope,
  subcontractors: Handshake,
  engineer: Bot,
  governance: BookOpen,
}

const DOMAIN_BY_ID = Object.fromEntries(SYSTEM_DOMAINS.map(d => [d.id, d])) as Record<
  SystemDomainId,
  SystemDomainRow
>

export function systemDomainLabel(id: SystemDomainId): string {
  return DOMAIN_BY_ID[id].label
}
