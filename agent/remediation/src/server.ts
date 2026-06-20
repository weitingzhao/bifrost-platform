import express from 'express'
import {
  cancelJob,
  createJob,
  getJob,
  listJobs,
  subscribe,
} from './jobs.js'
import { runRemediationJob } from './runner.js'
import type { StartRunRequest } from './types.js'

const app = express()
app.use(express.json({ limit: '2mb' }))

const port = Number(process.env.REMEDIATION_RUNNER_PORT ?? 8781)

app.get('/health', (_req, res) => {
  res.json({
    status: 'ok',
    service: 'bifrost-remediation-runner',
    cursor_api_key: Boolean(process.env.CURSOR_API_KEY?.trim()),
  })
})

app.get('/run', (_req, res) => {
  res.json({ jobs: listJobs() })
})

app.post('/run', (req, res) => {
  const body = req.body as StartRunRequest
  const job = createJob()
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

app.listen(port, '127.0.0.1', () => {
  console.log(`remediation runner listening on http://127.0.0.1:${port}`)
})
