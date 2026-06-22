import express from 'express'
import fs from 'node:fs'
import path from 'node:path'
import { spawn } from 'node:child_process'
import {
  cancelJob,
  createJob,
  getJob,
  listJobs,
  subscribe,
} from './jobs.js'
import { submitOperatorResponse } from './approvals.js'
import { buildOperatorInitBrief } from './prompt.js'
import { runRemediationJob } from './runner.js'
import type { StartRunRequest } from './types.js'

const app = express()
app.use(express.json({ limit: '2mb' }))

const port = Number(process.env.REMEDIATION_RUNNER_PORT ?? 8781)
const bindHost = process.env.REMEDIATION_RUNNER_BIND?.trim() || '127.0.0.1'

app.get('/health', (_req, res) => {
  res.json({
    status: 'ok',
    service: 'bifrost-remediation-runner',
    cursor_api_key: Boolean(process.env.CURSOR_API_KEY?.trim()),
  })
})

function nightlyReportsDir(): string {
  const root = process.env.BIFROST_AGENT_ROOT?.trim()
  if (root) return path.join(root.replace(/^~/, process.env.HOME ?? ''), 'reports')
  const projectRoot = process.env.PLATFORM_PROJECT_ROOT?.trim()
  if (projectRoot) return path.join(projectRoot, 'agent', 'reports')
  return path.join(process.env.HOME ?? '', 'bifrost-agent', 'reports')
}

function resolveNightlyScript(): string | null {
  const agentRoot = process.env.BIFROST_AGENT_ROOT?.trim()
  if (agentRoot) {
    const expanded = agentRoot.replace(/^~/, process.env.HOME ?? '')
    const deployed = path.join(expanded, 'nightly_drift.sh')
    if (fs.existsSync(deployed)) return deployed
  }
  const projectRoot = process.env.PLATFORM_PROJECT_ROOT?.trim()
  if (projectRoot) {
    const script = path.join(projectRoot, 'scripts', 'agent', 'nightly_drift.sh')
    if (fs.existsSync(script)) return script
  }
  return null
}

let nightlyScanRunning = false

app.post('/nightly/run', (_req, res) => {
  if (nightlyScanRunning) {
    res.status(409).json({ error: 'nightly scan already running', status: 'running' })
    return
  }
  const script = resolveNightlyScript()
  if (script == null) {
    res.status(500).json({
      error: 'nightly_drift.sh not found — set PLATFORM_PROJECT_ROOT or BIFROST_AGENT_ROOT',
      status: 'error',
    })
    return
  }

  const reportsDir = nightlyReportsDir()
  fs.mkdirSync(reportsDir, { recursive: true })
  const logPath = path.join(reportsDir, `nightly-manual-${Date.now()}.log`)

  nightlyScanRunning = true
  const child = spawn('bash', [script], {
    env: process.env,
    cwd: path.dirname(script),
    detached: false,
  })

  const logStream = fs.createWriteStream(logPath, { flags: 'a' })
  child.stdout?.pipe(logStream)
  child.stderr?.pipe(logStream)

  child.on('close', code => {
    nightlyScanRunning = false
    logStream.end()
    fs.appendFileSync(logPath, `\n--- exit ${code} ---\n`)
  })
  child.on('error', err => {
    nightlyScanRunning = false
    logStream.end()
    fs.appendFileSync(logPath, `\n--- spawn error: ${err.message} ---\n`)
  })

  res.status(202).json({
    status: 'started',
    script,
    log_path: logPath,
    reports_dir: reportsDir,
    hint: 'Refresh Agent Briefing in ~1–2 min for report and drift proposals',
  })
})

app.get('/nightly/status', (_req, res) => {
  res.json({
    running: nightlyScanRunning,
    script: resolveNightlyScript(),
    reports_dir: nightlyReportsDir(),
  })
})

app.get('/reports/latest', (_req, res) => {
  const latestPath = path.join(nightlyReportsDir(), 'latest.md')
  if (!fs.existsSync(latestPath)) {
    res.status(404).json({ error: 'no nightly report', path: latestPath })
    return
  }
  const content = fs.readFileSync(latestPath, 'utf8')
  const stat = fs.statSync(latestPath)
  res.json({
    content,
    source: latestPath,
    updated_at: stat.mtime.toISOString(),
  })
})

app.get('/run', (_req, res) => {
  res.json({ jobs: listJobs() })
})

app.post('/run', (req, res) => {
  const body = req.body as StartRunRequest
  const initBrief = buildOperatorInitBrief(body)
  const job = createJob(body.scope, body.actor, initBrief)
  void runRemediationJob(job.id, body)
  const { listeners: _l, abort: _a, ...publicJob } = job
  res.status(202).json(publicJob)
})

app.get('/run/:id', (req, res) => {
  const job = getJob(req.params.id)
  if (job == null) {
    res.status(404).json({ error: 'job not found' })
    return
  }
  res.json(job)
})

app.post('/run/:id/cancel', (req, res) => {
  const ok = cancelJob(req.params.id)
  if (!ok) {
    res.status(409).json({ error: 'job not running' })
    return
  }
  res.json(getJob(req.params.id))
})

app.post('/run/:id/respond', (req, res) => {
  const body = req.body as { option_id?: string; note?: string }
  const optionId = body.option_id?.trim()
  if (optionId == null || optionId === '') {
    res.status(400).json({ error: 'option_id required' })
    return
  }
  const ok = submitOperatorResponse(req.params.id, {
    option_id: optionId,
    note: body.note?.trim() || undefined,
  })
  if (!ok) {
    res.status(409).json({ error: 'no pending approval for this job' })
    return
  }
  res.json({ ok: true, job: getJob(req.params.id) })
})

app.get('/run/:id/stream', (req, res) => {
  const job = getJob(req.params.id)
  if (job == null) {
    res.status(404).json({ error: 'job not found' })
    return
  }

  res.setHeader('Content-Type', 'text/event-stream')
  res.setHeader('Cache-Control', 'no-cache')
  res.setHeader('Connection', 'keep-alive')
  res.flushHeaders?.()

  const send = (payload: unknown) => {
    res.write(`data: ${JSON.stringify(payload)}\n\n`)
  }

  send({ type: 'job', job })

  const unsubscribe = subscribe(req.params.id, event => {
    send({ type: 'event', event })
    const current = getJob(req.params.id)
    if (current != null && current.status !== 'running') {
      send({ type: 'job', job: current })
      cleanup()
    }
  })

  const heartbeat = setInterval(() => {
    res.write(': ping\n\n')
  }, 15_000)

  function cleanup() {
    clearInterval(heartbeat)
    unsubscribe()
    res.end()
  }

  req.on('close', cleanup)

  const current = getJob(req.params.id)
  if (current != null && current.status !== 'running') {
    send({ type: 'job', job: current })
    cleanup()
  }
})

app.listen(port, bindHost, () => {
  console.log(`remediation runner listening on http://${bindHost}:${port}`)
})
