import express from 'express'
import path from 'node:path'
import { createRequire } from 'node:module'
import { pathToFileURL } from 'node:url'
import { SkillRegistry } from './skills.js'
import { ExecutionStore } from './executions.js'
import { Scheduler } from './scheduler.js'
import { gatewayHealth } from './health.js'
import type { ScheduleView } from './types.js'
import { bindRefusal, requireBearer, RUNNER_TOKEN_ENV } from './routeAuth.js'

const require = createRequire(import.meta.url)
const pkg = require('../package.json') as { version: string }
const VERSION = pkg.version

export type HermesAppOptions = {
  runnerToken: string
  skillsYaml?: string
  dataDir?: string
}

export function createHermesApp(options: HermesAppOptions): {
  app: express.Express
  scheduler: Scheduler
  skillCount: number
  scriptsDir: string
} {
  const app = express()
  app.use(express.json())
  app.use(requireBearer(options.runnerToken))

  const skillsYaml =
    options.skillsYaml ??
    (process.env.HERMES_SKILLS_YAML?.trim() || path.join(import.meta.dirname, '..', 'skills.yaml'))

  const dataDir =
    options.dataDir ??
    (process.env.HERMES_DATA_DIR?.trim() || path.join(process.env.HOME ?? '/tmp', 'bifrost-agent', 'hermes'))

  const startTime = Date.now()

  const registry = new SkillRegistry(skillsYaml)
  const execStore = new ExecutionStore(dataDir)
  const scheduler = new Scheduler(registry, execStore)

// --- Health ---

app.get('/health', (_req, res) => {
  // HTTP 200 while the process serves (liveness); `status` is the skills verdict.
  const health = gatewayHealth(registry, execStore)
  res.json({
    status: health.status,
    failing_skills: health.failing_skills,
    service: 'bifrost-hermes-gateway',
    version: VERSION,
    skill_count: registry.count(),
    scripts_dir: registry.scriptsDir(),
    uptime_seconds: Math.floor((Date.now() - startTime) / 1000),
  })
})

// --- Skills ---

app.get('/skills', (_req, res) => {
  const skills = registry.toViews(execStore)
  res.json({
    gateway_status: gatewayHealth(registry, execStore).status,
    skills,
    generated_at: new Date().toISOString(),
  })
})

// --- Schedules ---

app.get('/schedules', (_req, res) => {
  const skills = registry.all()
  const schedules: ScheduleView[] = skills
    .filter(s => s.trigger === 'cron' && s.schedule != null)
    .map(s => ({
      skill_id: s.id,
      cron: s.schedule!,
      enabled: s.status === 'enabled',
      timezone: 'America/Chicago',
    }))
  res.json({
    schedules,
    generated_at: new Date().toISOString(),
  })
})

// --- Executions ---

app.get('/executions', (req, res) => {
  const limit = Number(req.query.limit) || 50
  const data = execStore.list(limit)
  res.json({
    ...data,
    generated_at: new Date().toISOString(),
  })
})

// --- Manual trigger ---

app.post('/skills/:id/trigger', async (req, res) => {
  const ok = await scheduler.triggerManual(req.params.id)
  if (!ok) {
    res.status(404).json({ error: 'skill not found' })
    return
  }
  const last = execStore.lastForSkill(req.params.id)
  res.json({ ok: true, execution: last })
})

// --- Reload skills from YAML ---

app.post('/reload', (_req, res) => {
  scheduler.reload()
  res.json({
    ok: true,
    skill_count: registry.count(),
  })
})

  return { app, scheduler, skillCount: registry.count(), scriptsDir: registry.scriptsDir() }
}

function main(): void {
  const port = Number(process.env.HERMES_GATEWAY_PORT ?? 8782)
  const bindHost = process.env.HERMES_GATEWAY_BIND?.trim() || '127.0.0.1'
  const runnerToken = process.env[RUNNER_TOKEN_ENV]?.trim() ?? ''
  const refusal = bindRefusal(bindHost, runnerToken)
  if (refusal != null) {
    console.error(`[hermes-gateway] ${refusal}`)
    process.exit(1)
  }
  const { app, scheduler, skillCount, scriptsDir } = createHermesApp({ runnerToken })
  scheduler.start()
  app.listen(port, bindHost, () => {
    console.log(
      `hermes gateway v${VERSION} listening on http://${bindHost}:${port} — ${skillCount} skills loaded, scripts from ${scriptsDir}`,
    )
  })
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main()
}
