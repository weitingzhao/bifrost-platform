/**
 * Remediation custom tools — public entry. Tool families live under ./tools/.
 * No-drift: tool names, schemas, and approval behavior unchanged.
 */
import type { SDKCustomTool } from '@cursor/sdk'
import { buildDeliveryTools } from './tools/deliveryTools.js'
import { buildGitTools } from './tools/gitTools.js'
import { buildKubectlTools } from './tools/kubectlTools.js'
import { buildOperatorTools } from './tools/operatorTools.js'
import { buildPeerTools } from './tools/peerTools.js'
import { buildPlatformTools } from './tools/platformTools.js'
import { buildReleaseFixTools } from './tools/releaseFixTools.js'

export function buildCustomTools(jobId: string): Record<string, SDKCustomTool> {
  return {
    ...buildOperatorTools(jobId),
    ...buildKubectlTools(),
    ...buildPlatformTools(jobId),
    ...buildReleaseFixTools(jobId),
    ...buildPeerTools(),
    ...buildGitTools(),
    ...buildDeliveryTools(),
  }
}
