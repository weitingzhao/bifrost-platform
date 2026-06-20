import type { LocalAgentStore, LocalAgentStoreAgents, LocalAgentStoreCheckpoints, LocalAgentStoreRunEvents, LocalAgentStoreRuns } from "./local-agent-store.js";
/** JSONL filenames under the directory passed to {@link JsonlLocalAgentStore}. */
export declare const JSONL_LOCAL_AGENT_STORE_FILES: {
    readonly agents: "agents.ndjson";
    readonly runs: "runs.ndjson";
    readonly runEvents: "run_events.ndjson";
    readonly checkpoints: "checkpoints.ndjson";
};
/**
 * File-backed {@link LocalAgentStore} using newline-delimited JSON (JSONL / NDJSON).
 *
 * Under `rootDir` the store reads and writes four files:
 * {@link JSONL_LOCAL_AGENT_STORE_FILES}. Writes are serialized process-wide so
 * concurrent SDK callbacks do not interleave partial lines. Listing rewrites whole
 * files on delete/update (simple, portable; not optimized for huge catalogs).
 *
 * Pass the same instance (or the same `Cursor.configure` default) on
 * `Agent.create`, `Agent.resume`, and local list/get APIs.
 *
 * @example
 * ```typescript
 * const store = new JsonlLocalAgentStore("/path/to/store");
 * Cursor.configure({ local: { store } });
 * ```
 */
export declare class JsonlLocalAgentStore implements LocalAgentStore {
    private readonly rootDir;
    readonly agents: LocalAgentStoreAgents;
    readonly checkpoints: LocalAgentStoreCheckpoints;
    readonly runs: LocalAgentStoreRuns;
    readonly runEvents: LocalAgentStoreRunEvents;
    constructor(rootDir: string);
}
//# sourceMappingURL=jsonl-local-agent-store.d.ts.map