import fs from 'node:fs'
import path from 'node:path'
import type { ExecutionRecord, ExecutionResult, SkillTrigger } from './types.js'

const MAX_MEMORY = 500

export class ExecutionStore {
  private records: ExecutionRecord[] = []
  private persistPath: string | null = null

  constructor(persistDir?: string) {
    if (persistDir != null) {
      fs.mkdirSync(persistDir, { recursive: true })
      this.persistPath = path.join(persistDir, 'executions.json')
      this.load()
    }
  }

  record(params: {
    skillId: string
    skillLabel: string
    trigger: SkillTrigger
    result: ExecutionResult
    startedAt: Date
    finishedAt?: Date
    summary?: string
    error?: string
  }): ExecutionRecord {
    const rec: ExecutionRecord = {
      id: `exec-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
      skill_id: params.skillId,
      skill_label: params.skillLabel,
      trigger: params.trigger,
      result: params.result,
      started_at: params.startedAt.toISOString(),
      finished_at: params.finishedAt?.toISOString(),
      duration_ms:
        params.finishedAt != null
          ? params.finishedAt.getTime() - params.startedAt.getTime()
          : undefined,
      summary: params.summary,
      error: params.error,
    }
    this.records.unshift(rec)
    if (this.records.length > MAX_MEMORY) {
      this.records.length = MAX_MEMORY
    }
    this.persist()
    return rec
  }

  list(limit = 50): { executions: ExecutionRecord[]; total: number } {
    return {
      executions: this.records.slice(0, limit),
      total: this.records.length,
    }
  }

  lastForSkill(skillId: string): ExecutionRecord | undefined {
    return this.records.find(r => r.skill_id === skillId)
  }

  private persist() {
    if (this.persistPath == null) return
    try {
      fs.writeFileSync(this.persistPath, JSON.stringify(this.records, null, 2))
    } catch {
      // non-critical
    }
  }

  private load() {
    if (this.persistPath == null || !fs.existsSync(this.persistPath)) return
    try {
      const raw = fs.readFileSync(this.persistPath, 'utf8')
      this.records = JSON.parse(raw) as ExecutionRecord[]
    } catch {
      this.records = []
    }
  }
}
