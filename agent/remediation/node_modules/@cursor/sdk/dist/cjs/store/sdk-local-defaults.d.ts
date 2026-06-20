import type { LocalAgentStore } from "./local-agent-store.js";
export interface SdkLocalDefaultsSource {
    readonly local?: {
        readonly store?: LocalAgentStore;
    };
    readonly store?: LocalAgentStore;
}
/**
 * Module-level default `LocalAgentStore` for static `Agent.*` local routes.
 * Per-call `store` / `local.store` overrides this value.
 */
export declare function resolveLocalAgentStore(options?: SdkLocalDefaultsSource): LocalAgentStore | undefined;
export declare function setDefaultLocalAgentStore(store: LocalAgentStore | undefined): void;
export declare function __clearDefaultLocalAgentStoreForTests(): void;
//# sourceMappingURL=sdk-local-defaults.d.ts.map