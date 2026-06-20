import { type McpExecutor, type NamedMcpToolDefinition } from "@anysphere/agent-exec";
import type { SDKCustomTool } from "./options.js";
/**
 * Convert SDK-provided custom tools into MCP tool definitions for a synthetic
 * "custom-user-tools" server. The model discovers and invokes them through the
 * same MCP meta-tool path as other servers (GetMcpTools / CallMcpTool). The
 * map key is the tool name. The `name` field is the MCP wire identifier sent on
 * AgentRunRequest; the model calls tools via GetMcpTools/CallMcpTool using
 * server `custom-user-tools` and the bare `toolName`.
 */
export declare function createCustomUserToolDefinitions(customTools: Record<string, SDKCustomTool> | undefined): NamedMcpToolDefinition[];
export declare function hasCustomTools(customTools: Record<string, SDKCustomTool> | undefined): customTools is Record<string, SDKCustomTool>;
/**
 * True when an MCP invocation targets the synthetic SDK custom-tools server.
 * Uses providerIdentifier only; wire names like `custom-user-tools-*` on other
 * servers must still route through the lease-backed executor.
 */
export declare function isSdkCustomUserToolsMcpCall(args: {
    providerIdentifier: string;
    name: string;
}): boolean;
/**
 * Build the client-side MCP executor that handles calls to the synthetic
 * "custom-user-tools" server by invoking the user's in-process callbacks. The
 * agent loop (on the backend) calls `mcpExecutorResource.execute`, which
 * round-trips to this implementation registered on the local runtime.
 */
export declare function createSdkCustomToolMcpExecutor(customTools: Record<string, SDKCustomTool>): McpExecutor;
//# sourceMappingURL=custom-tools.d.ts.map