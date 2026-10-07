import fs from 'node:fs'
import path from 'node:path'
import { parse as parseYaml } from 'yaml'
import type { SkillDefinition, SkillView } from './types.js'
import type { ExecutionStore } from './executions.js'

/**
 * Where skill scripts live. On a Mac Mini deploy_hermes_gateway.sh rsyncs
 * scripts/agent to ~/bifrost-agent/hermes-scripts and the plist sets
 * HERMES_SCRIPTS_DIR. In a repo checkout the default is scripts/agent beside
 * agent/hermes-gateway. TD-228: the deploy copied only the gateway directory
 * and every scheduled run failed with "No such file or directory".
 */
export function defaultScriptsDir(skillsDir: string): string {
  const fromEnv = process.env.HERMES_SCRIPTS_DIR?.trim()
  if (fromEnv) return fromEnv
  return path.resolve(skillsDir, '..', '..', 'scripts', 'agent')
}

export class SkillRegistry {
  private skills: Map<string, SkillDefinition> = new Map()
  private yamlPath: string
  private scriptsDirOverride: string | undefined

  constructor(yamlPath: string, scriptsDir?: string) {
    this.yamlPath = yamlPath
    this.scriptsDirOverride = scriptsDir
    this.reload()
  }

  reload(): void {
    if (!fs.existsSync(this.yamlPath)) return
    const raw = fs.readFileSync(this.yamlPath, 'utf8')
    const doc = parseYaml(raw) as { skills?: SkillDefinition[] }
    this.skills.clear()
    for (const s of doc.skills ?? []) {
      this.skills.set(s.id, s)
    }
  }

  get(id: string): SkillDefinition | undefined {
    return this.skills.get(id)
  }

  all(): SkillDefinition[] {
    return [...this.skills.values()]
  }

  count(): number {
    return this.skills.size
  }

  skillsDir(): string {
    return path.dirname(this.yamlPath)
  }

  scriptsDir(): string {
    return this.scriptsDirOverride ?? defaultScriptsDir(this.skillsDir())
  }

  /** Absolute script path for a skill (bare names resolve against scriptsDir). */
  resolveScript(skill: SkillDefinition): string | undefined {
    if (skill.script == null || skill.script.trim() === '') return undefined
    return path.isAbsolute(skill.script) ? skill.script : path.resolve(this.scriptsDir(), skill.script)
  }

  /**
   * Why an enabled skill cannot run, or undefined when it can. A skill whose
   * script is missing is reported as errored and is not scheduled.
   */
  problem(skill: SkillDefinition): string | undefined {
    if (skill.status !== 'enabled') return undefined
    const script = this.resolveScript(skill)
    if (script == null) return undefined
    if (!fs.existsSync(script)) return `script not found: ${script}`
    return undefined
  }

  /** Enabled skills that cannot run, keyed by id. */
  problems(): Map<string, string> {
    const out = new Map<string, string>()
    for (const s of this.all()) {
      const p = this.problem(s)
      if (p != null) out.set(s.id, p)
    }
    return out
  }

  toViews(execStore: ExecutionStore): SkillView[] {
    return this.all().map(s => {
      const last = execStore.lastForSkill(s.id)
      const problem = this.problem(s)
      return {
        id: s.id,
        label: s.label,
        description: s.description,
        trigger: s.trigger,
        schedule: s.schedule,
        actuation_level: s.actuation_level,
        status: problem != null ? 'error' : s.status,
        error: problem,
        last_run_at: last?.started_at,
        last_result: last?.result,
        tags: s.tags,
      }
    })
  }
}
