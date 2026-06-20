import type { UniversalClientFn, UniversalClientRequest } from "@connectrpc/connect/protocol";
import { type NodeHttpClientOptions } from "@connectrpc/connect-node";
/**
 * connect-node only attaches a swallowing `.catch()` on its internal sentinel
 * promise when `req.signal` is set. Ensure every universal client request has a
 * signal so late stream/transport failures do not surface as unhandled rejections
 * at the process level.
 */
export declare function withRequestAbortSignal(req: UniversalClientRequest): UniversalClientRequest;
export declare function createSafeNodeHttpClient(options: NodeHttpClientOptions): UniversalClientFn;
//# sourceMappingURL=safe-node-http-client.d.ts.map