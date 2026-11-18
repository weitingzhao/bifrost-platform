import '@/api/agentRuntime'
import '@/lib/architecture/opsToolRackCatalog'
import '@/lib/shell/consoleRoutes'

/** Snapshot carried by the ordinary approval/question read, never persisted in its material. */
export type AssessmentSnapshot = { events?: unknown; error?: string }
