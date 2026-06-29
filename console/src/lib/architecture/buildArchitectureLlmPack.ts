/**
 * Unified governance LLM pack — merges Architecture (Blueprint, Environments)
 * and Standards (Platform, Agent Protocol, Design System) sub-packs into a
 * single copyable text.
 */

import type { OpsContextResponse } from '@/api/types'
import { buildEnvironmentsLlmContext } from '@/lib/environments-catalog'
import { buildDesignSystemLlmPack } from '@/lib/standards/designSystemCatalog'
import { buildMcpContractLlmPack } from '@/lib/standards/mcpContractCatalog'
import { buildAgentProtocolLlmPack } from './agentProtocolCatalog'
import { buildCicdBootstrapLlmPack } from './cicdBootstrapCatalog'
import { buildDataLayerLlmPack } from './dataLayerCatalog'
import { buildBlueprintLlmPack } from './blueprintCatalog'
import { buildDualFlywheelVisionLlmPack } from './dualFlywheelVisionCatalog'
import { buildK3sArchitectureLlmPack } from './k3sArchitectureCatalog'
import { buildDeployMainlineLlmPack } from './deployMainlineCatalog'
import { buildK3sBootstrapLlmPack } from './k3sBootstrapCatalog'
import { buildRoadmapLlmPack } from './roadmapCatalog'
import { buildTradeK8sNativeLlmPack } from './tradeK8sNativeCatalog'
import { buildStandardsLlmPack } from './standardsCatalog'

/**
 * Build a full governance LLM pack combining Architecture + Standards pages.
 * Each sub-pack is separated by a horizontal rule for readability.
 */
export function buildFullArchitectureLlmPack(spine?: OpsContextResponse): string {
  const sections = [
    buildBlueprintLlmPack(spine),
    buildDualFlywheelVisionLlmPack(),
    buildEnvironmentsLlmContext(spine),
    buildRoadmapLlmPack(),
    buildK3sArchitectureLlmPack(),
    buildK3sBootstrapLlmPack(),
    buildDataLayerLlmPack(),
    buildTradeK8sNativeLlmPack(),
    buildCicdBootstrapLlmPack(),
    buildDeployMainlineLlmPack(),
    buildStandardsLlmPack(),
    buildAgentProtocolLlmPack(),
    buildMcpContractLlmPack(),
    buildDesignSystemLlmPack(),
  ]
  return sections.join('\n\n---\n\n')
}
