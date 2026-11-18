import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import ts from 'typescript'

// Descriptive source inventory only. CI still executes the existing console module.
export function buildInventory(repo, declaration) {
  const files = []
  const walk = dir => {
    for (const entry of fs.readdirSync(path.join(repo, dir), { withFileTypes: true })) {
      const name = `${dir}/${entry.name}`
      if (entry.isDirectory()) walk(name)
      else if (entry.isFile()) files.push(name)
    }
  }
  walk(declaration.root)
  files.sort()
  const all = new Set(files)
  const paths = Object.keys(declaration.groups).sort((a, b) => b.length - a.length || a.localeCompare(b))
  const owner = file => paths.find(p => file === p || file.startsWith(`${p}/`))
  const groups = new Map(paths.map(p => [p, { path: p, files: [], imports: new Set(), test_imports: new Set(), external_imports: new Set(), source_inputs: new Set(), test_inputs: new Set(), unresolved_imports: new Set() }]))
  const isTest = file => /\/(?:__tests__|__fixtures__)\/|\.(?:test|spec)\.[cm]?[jt]sx?$/.test(file)
  for (const file of files) {
    const group = groups.get(owner(file))
    if (!group) throw new Error(`No source group for ${file}`)
    group.files.push(file)
    if (!/\.[cm]?[jt]sx?$/.test(file)) continue
    const source = ts.createSourceFile(file, fs.readFileSync(path.join(repo, file), 'utf8'), ts.ScriptTarget.Latest, true)
    const imports = []
    const visit = node => {
      if ((ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) && node.moduleSpecifier && ts.isStringLiteral(node.moduleSpecifier)) imports.push(node.moduleSpecifier.text)
      if (ts.isCallExpression(node) && (node.expression.kind === ts.SyntaxKind.ImportKeyword || node.expression.getText(source) === 'require')) {
        if (node.arguments.length === 1 && ts.isStringLiteral(node.arguments[0])) imports.push(node.arguments[0].text)
        else group.unresolved_imports.add(`${file}: non-literal import`)
      }
      ts.forEachChild(node, visit)
    }
    visit(source)
    for (const spec of imports) {
      if (!spec.startsWith('.') && !spec.startsWith('@/')) { group.external_imports.add(spec); continue }
      const base = spec.startsWith('@/') ? `${declaration.root}/${spec.slice(2)}` : path.posix.normalize(path.posix.join(path.posix.dirname(file), spec))
      const candidates = [base, ...['.ts', '.tsx', '.js', '.jsx', '.json', '.d.ts', '.mjs', '/index.ts', '/index.tsx', '/index.js'].map(ext => base + ext)]
      const found = candidates.find(p => all.has(p))
      if (!found) {
        const input = candidates.find(p => !p.startsWith('../') && fs.existsSync(path.join(repo, p)) && fs.statSync(path.join(repo, p)).isFile())
        if (input) { (isTest(file) ? group.test_inputs : group.source_inputs).add(input); continue }
      }
      if (!found) { group.unresolved_imports.add(`${file}: ${spec}`); continue }
      const target = owner(found)
      if (target !== group.path) (isTest(file) ? group.test_imports : group.imports).add(target)
    }
  }
  for (const group of groups.values()) if (group.files.length === 0) throw new Error(`Declared frontend group has no files: ${group.path}`)
  return { version: 1, root: declaration.root, groups: [...groups.values()].sort((a, b) => a.path.localeCompare(b.path)).map(g => ({ ...g, imports: [...g.imports].sort(), test_imports: [...g.test_imports].sort(), external_imports: [...g.external_imports].sort(), source_inputs: [...g.source_inputs].sort(), test_inputs: [...g.test_inputs].sort(), unresolved_imports: [...g.unresolved_imports].sort() })) }
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
  const doc = JSON.parse(fs.readFileSync(path.join(repo, 'console/domain-ownership.json'), 'utf8'))
  fs.writeFileSync(path.join(repo, doc.frontend.inventory), `${JSON.stringify(buildInventory(repo, doc.frontend), null, 2)}\n`)
}
