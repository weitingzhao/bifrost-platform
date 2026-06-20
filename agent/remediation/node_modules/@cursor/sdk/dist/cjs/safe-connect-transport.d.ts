import type { Transport } from "@connectrpc/connect";
import { type ConnectTransportOptions } from "@connectrpc/connect-node";
/**
 * Drop-in replacement for `createConnectTransport` that routes HTTP through
 * {@link createSafeNodeHttpClient} so connect-node always receives a request
 * `AbortSignal` (activating sentinel orphan-rejection swallowing).
 */
export declare function createSafeConnectTransport(options: ConnectTransportOptions): Transport;
//# sourceMappingURL=safe-connect-transport.d.ts.map