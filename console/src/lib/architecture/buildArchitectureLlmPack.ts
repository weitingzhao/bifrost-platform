/**
 * Unified governance LLM pack — merges Architecture (Blueprint, environments-catalog)
 * and Standards (Platform, Agent Protocol, Design System) sub-packs into a
 * single copyable text.
 */

import type { OpsContextResponse } from '@/api/opsContextTypes'
import { buildEnvironmentsLlmContext } from '@/lib/environments-catalog'
import { buildDesignSystemLlmPack } from '@/lib/standards/designSystemCatalog'
import { buildMcpContractLlmPack } from '@/lib/standards/mcpContractCatalog'
import { buildAgentProtocolLlmPack } from './agentProtocolCatalog'
import { buildBriefingReconciliationLlmPack } from './briefingReconciliationCatalog'
import { buildCicdBootstrapLlmPack } from './cicdBootstrapCatalog'
import { buildDataLayerLlmPack } from './dataLayerCatalog'
import { buildBlueprintLlmPack } from './blueprintCatalog'
import { buildDualFlywheelVisionLlmPack } from './dualFlywheelVisionCatalog'
import { buildK3sArchitectureLlmPack } from './k3sArchitectureCatalog'
import { buildDeployMainlineLlmPack } from './deployMainlineCatalog'
import { buildK3sBootstrapLlmPack } from './k3sBootstrapCatalog'
import { buildNetworkApiContractLlmPack } from './networkApiContractCatalog'
import { buildNetworkUpgradeLlmPack } from './networkUpgradeCatalog'
import { buildIbGatewayPluginLlmPack } from './ibGatewayPluginCatalog'
import { buildMarketDataSubcontractorLlmPack } from './marketDataSubcontractorCatalog'
import { buildAnalyticsPipelineLlmPack } from './analyticsPipelineCatalog'
import { buildTradeIbClientMigrationLlmPack } from './tradeIbClientMigrationCatalog'
import { buildTradeDevInnerLoopLlmPack } from './tradeDevInnerLoopCatalog'
import { buildRoadmapLlmPack } from './roadmapCatalog'
import { buildTradeK8sNativeLlmPack } from './tradeK8sNativeCatalog'
import { buildObservabilityLlmPack } from './observabilityCatalog'
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
    buildNetworkUpgradeLlmPack(),
    buildNetworkApiContractLlmPack(),
    buildIbGatewayPluginLlmPack(),
    buildMarketDataSubcontractorLlmPack(),
    buildAnalyticsPipelineLlmPack(),
    buildTradeIbClientMigrationLlmPack(),
    buildTradeDevInnerLoopLlmPack(),
    buildStandardsLlmPack(),
    buildObservabilityLlmPack(),
    buildAgentProtocolLlmPack(),
    buildBriefingReconciliationLlmPack(spine),
    buildMcpContractLlmPack(),
    buildDesignSystemLlmPack(),
  ]
  return sections.join('\n\n---\n\n')
}
