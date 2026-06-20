import type { LocalAgentStore, LocalAgentStoreAgents, LocalAgentStoreCheckpoints, LocalAgentStoreRunEvents, LocalAgentStoreRuns } from "./local-agent-store.js";
export interface SqliteLocalAgentStoreOptions {
    /** Workspace path used for default state root and agent `cwd` scoping. */
    readonly workspaceRef: string;
    /**
     * Root directory for `index.db`, per-agent checkpoint dirs, etc.
     * Defaults to {@link getDefaultSdkStateRoot} for `workspaceRef`.
     */
    readonly stateRoot?: string;
}
/**
 * On-disk {@link LocalAgentStore} backed by the SDK's default SQLite layout
 * (`index.db`, per-agent checkpoint blob stores, `run_events` table).
 *
 * This is the persistence surface used when no custom `local.store` is passed.
 * Open once per workspace/state root and reuse across `Agent.create` / `resume`.
 */
export declare class SqliteLocalAgentStore implements LocalAgentStore {
    readonly stateRoot: string;
    readonly workspaceRef: string;
    readonly agents: LocalAgentStoreAgents;
    readonly checkpoints: LocalAgentStoreCheckpoints;
    readonly runs: LocalAgentStoreRuns;
    readonly runEvents: LocalAgentStoreRunEvents;
    private readonly deps;
    static open(options: SqliteLocalAgentStoreOptions): Promise<SqliteLocalAgentStore>;
    private constructor();
    dispose(): Promise<void>;
}
//# sourceMappingURL=sqlite-local-agent-store.d.ts.map