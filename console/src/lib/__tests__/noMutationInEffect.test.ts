import { readFileSync, readdirSync, statSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import * as ts from 'typescript'
import { describe, expect, it } from 'vitest'

const SRC = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')

/**
 * TD-224: opening the Cluster page as an operator started a full-auto
 * remediation agent from a useEffect. A page load, a refetch or a second tab
 * must never dispatch an agent or fire a write — those are operator actions.
 *
 * This scans every useEffect / useLayoutEffect body for calls that start an
 * agent or run a mutation. A deliberate continuation of an operator action may
 * stay if the line above the call carries `// effect-mutation-ok: <reason>`.
 */
const MUTATING_CALL = /^(startRemediation|mutate|mutateAsync|onAutoCheck|handleAutoRemediate)$/
const ALLOW = 'effect-mutation-ok:'

function walk(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const full = path.join(dir, entry)
    if (statSync(full).isDirectory()) {
      if (entry !== '__tests__' && entry !== 'node_modules') walk(full, out)
    } else if (/\.tsx?$/.test(entry) && !/\.test\.tsx?$/.test(entry)) {
      out.push(full)
    }
  }
  return out
}

function calleeName(call: ts.CallExpression): string {
  const ex = call.expression
  if (ts.isIdentifier(ex)) return ex.text
  if (ts.isPropertyAccessExpression(ex)) return ex.name.text
  return ''
}

function mutationsInEffects(file: string, text = readFileSync(file, 'utf8')): string[] {
  if (!text.includes('Effect(')) return []
  const lines = text.split('\n')
  const sf = ts.createSourceFile(
    file,
    text,
    ts.ScriptTarget.Latest,
    true,
    file.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
  )
  const found: string[] = []
  const scanEffectBody = (node: ts.Node) => {
    if (ts.isCallExpression(node) && MUTATING_CALL.test(calleeName(node))) {
      const line = sf.getLineAndCharacterOfPosition(node.getStart()).line
      const allowed = (lines[line - 1] ?? '').includes(ALLOW) || (lines[line] ?? '').includes(ALLOW)
      if (!allowed) found.push(`${path.relative(SRC, file)}:${line + 1} ${node.getText().slice(0, 60)}`)
    }
    ts.forEachChild(node, scanEffectBody)
  }
  const visit = (node: ts.Node) => {
    if (
      ts.isCallExpression(node) &&
      ts.isIdentifier(node.expression) &&
      (node.expression.text === 'useEffect' || node.expression.text === 'useLayoutEffect') &&
      node.arguments[0] != null
    ) {
      scanEffectBody(node.arguments[0])
    }
    ts.forEachChild(node, visit)
  }
  visit(sf)
  return found
}

describe('no agent dispatch or mutation inside effects (TD-224)', () => {
  it('scans console/src', () => {
    const offenders = walk(SRC).flatMap(file => mutationsInEffects(file))
    expect(offenders).toEqual([])
  })

  it('catches the TD-224 shape and honours the allowlist comment', () => {
    const bad = path.join(SRC, 'fixture/Bad.tsx')
    expect(mutationsInEffects(bad, 'useEffect(() => {\n  if (x) onAutoCheck()\n}, [x])\n')).toHaveLength(1)
    expect(
      mutationsInEffects(
        bad,
        'useEffect(() => {\n  // effect-mutation-ok: continues an operator click\n  m.mutate(1)\n}, [x])\n',
      ),
    ).toEqual([])
  })
})
