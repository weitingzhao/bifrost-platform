import type { AgentRecord, RunRecord } from "../../run-store-public-types.js";
import type { LocalAgentDocument, LocalAgentRunDocument } from "./local-agent-store.js";
export declare function localAgentDocumentFromAgentRecord(record: AgentRecord): LocalAgentDocument;
export declare function localAgentRunDocumentFromRunRecord(record: RunRecord): LocalAgentRunDocument;
export declare function agentRecordFromLocalAgentDocument(document: LocalAgentDocument): AgentRecord;
export declare function runRecordFromLocalAgentRunDocument(run: LocalAgentRunDocument): RunRecord;
//# sourceMappingURL=local-agent-record-conversion.d.ts.map