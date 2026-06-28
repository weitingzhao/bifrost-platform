import express from 'express'
import path from 'node:path'
import { createRequire } from 'node:module'
import { SkillRegistry } from './skills.js'
import { ExecutionStore } from './executions.js'
import { Scheduler } from './scheduler.js'
import type { ScheduleView } from './types.js'

const require = createRequire(import.meta.url)
const pkg = require('../package.json') as { version: string }
const VERSION = pkg.version

const app = express()
app.use(express.json())

const port = Number(process.env.HERMES_GATEWAY_PORT ?? 8782)
const bindHost = process.env.HERMES_GATEWAY_BIND?.trim() || '127.0.0.1'

const skillsYaml =
  process.env.HERMES_SKILLS_YAML?.trim() ||
  path.join(import.meta.dirname, '..', 'skills.yaml')

const dataDir =
  process.env.HERMES_DATA_DIR?.trim() ||
  path.join(process.env.HOME ?? '/tmp', 'bifrost-agent', 'hermes')

const startTime = Date.now()

const registry = new SkillRegistry(skillsYaml)
const execStore = new ExecutionStore(dataDir)
const scheduler = new Scheduler(registry, execStore)

scheduler.start()

// --- Health ---

app.get('/health', (_req, res) => {
  res.json({
    status: 'ok',
    service: 'bifrost-hermes-gateway',
    version: VERSION,
    skill_count: registry.count(),
    uptime_seconds: Math.floor((Date.now() - startTime) / 1000),
  })
})

// --- Skills ---

app.get('/skills', (_req, res) => {
  const skills = registry.toViews(execStore)
  res.json({
    gateway_status: 'ok',
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

app.listen(port, bindHost, () => {
  console.log(
    `hermes gateway v${VERSION} listening on http://${bindHost}:${port} — ${registry.count()} skills loaded`,
  )
})
