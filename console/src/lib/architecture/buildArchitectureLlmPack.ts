/**
 * Unified governance LLM pack — merges Architecture (Blueprint, Environments)
 * and Standards (Platform, Agent Protocol, Design System) sub-packs into a
 * single copyable text.
 */

import type { OpsContextResponse } from '@/api/types'
import { buildEnvironmentsLlmContext } from '@/lib/environments-catalog'
import { buildDesignSystemLlmPack } from '@/lib/standards/designSystemCatalog'
import { buildAgentProtocolLlmPack } from './agentProtocolCatalog'
import { buildBlueprintLlmPack } from './blueprintCatalog'
import { buildStandardsLlmPack } from './standardsCatalog'

/**
 * Build a full governance LLM pack combining Architecture + Standards pages.
 * Each sub-pack is separated by a horizontal rule for readability.
 */
export function buildFullArchitectureLlmPack(spine?: OpsContextResponse): string {
  const sections = [
    buildBlueprintLlmPack(spine),
    buildEnvironmentsLlmContext(spine),
    buildStandardsLlmPack(),
    buildAgentProtocolLlmPack(),
    buildDesignSystemLlmPack(),
  ]
  return sections.join('\n\n---\n\n')
}
