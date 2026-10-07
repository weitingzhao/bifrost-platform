import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { ExecutionStore } from './executions.js'
import { gatewayHealth } from './health.js'
import { SkillRegistry } from './skills.js'

const GATEWAY_DIR = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const REPO_SKILLS_YAML = path.join(GATEWAY_DIR, 'skills.yaml')

/** The deploy layout: only the gateway directory, no scripts beside it. */
function deployedGatewayOnly(): { yaml: string; root: string } {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'hermes-gw-'))
  const gw = path.join(root, 'bifrost-agent', 'hermes-gateway')
  fs.mkdirSync(gw, { recursive: true })
  fs.copyFileSync(REPO_SKILLS_YAML, path.join(gw, 'skills.yaml'))
  return { yaml: path.join(gw, 'skills.yaml'), root }
}

test('TD-228: gateway dir without scripts fails validation and /health is degraded', () => {
  const { yaml, root } = deployedGatewayOnly()
  const registry = new SkillRegistry(yaml, path.join(root, 'bifrost-agent', 'hermes-scripts'))
  const enabled = registry.all().filter(s => s.status === 'enabled' && s.script != null)
  assert.ok(enabled.length > 0)
  const problems = registry.problems()
  for (const s of enabled) assert.match(problems.get(s.id) ?? '', /script not found/)

  const health = gatewayHealth(registry, new ExecutionStore())
  assert.equal(health.status, 'degraded')
  assert.deepEqual(
    health.failing_skills.map(f => f.id).sort(),
    enabled.map(s => s.id).sort(),
  )
  const views = registry.toViews(new ExecutionStore())
  for (const s of enabled) assert.equal(views.find(v => v.id === s.id)?.status, 'error')
})

test('every enabled skill script exists in the repo scripts dir (what the deploy ships)', () => {
  const registry = new SkillRegistry(REPO_SKILLS_YAML, path.resolve(GATEWAY_DIR, '..', '..', 'scripts', 'agent'))
  assert.deepEqual([...registry.problems().entries()], [])
})

test('health turns degraded after N consecutive failures and recovers on a success', () => {
  const registry = new SkillRegistry(REPO_SKILLS_YAML, path.resolve(GATEWAY_DIR, '..', '..', 'scripts', 'agent'))
  const store = new ExecutionStore()
  const rec = (result: 'success' | 'failure') =>
    store.record({
      skillId: 'stale-pipeline-triage',
      skillLabel: 'Stale Pipeline Triage',
      trigger: 'cron',
      result,
      startedAt: new Date(),
      finishedAt: new Date(),
      error: result === 'failure' ? 'boom' : undefined,
    })
  rec('failure')
  rec('failure')
  assert.equal(gatewayHealth(registry, store).status, 'ok')
  rec('failure')
  const h = gatewayHealth(registry, store)
  assert.equal(h.status, 'degraded')
  assert.equal(h.failing_skills[0]?.id, 'stale-pipeline-triage')
  rec('success')
  assert.equal(gatewayHealth(registry, store).status, 'ok')
})

test('TD-251: no enabled skill runs a script a launchd plist already runs', () => {
  const registry = new SkillRegistry(REPO_SKILLS_YAML)
  const deployDir = path.resolve(GATEWAY_DIR, '..', 'deploy')
  const plists = fs.readdirSync(deployDir).filter(f => f.endsWith('.plist'))
    .map(f => fs.readFileSync(path.join(deployDir, f), 'utf8'))
  const twins = registry.all()
    .filter(s => s.status === 'enabled' && s.script != null)
    .filter(s => plists.some(p => p.includes(`/${path.basename(s.script as string)}`)))
    .map(s => s.id)
  assert.deepEqual(twins, [])
})
