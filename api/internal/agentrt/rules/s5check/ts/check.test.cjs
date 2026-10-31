'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const {setImmediate:yieldTurn} = require('node:timers/promises');
const {check} = require('./check.cjs');
const {canonical,sha256} = require('./compiler.cjs');
const {readTree:readRepositoryTree} = require('./enroll.cjs');
const {contractDigests} = require('./contracts.cjs');
const {contract,positive,negative,C1,C4} = require('../testdata/cases.cjs');
const here = path.resolve(__dirname, '..');
const repo = path.resolve(here, '../../../../..');
const fixtureRoot = path.join(here, 'testdata/baseline');
const manifest = JSON.parse(fs.readFileSync(path.join(here, 'policies/manifest.json'), 'utf8'));
const sha = data => crypto.createHash('sha256').update(data).digest('hex');
const blob = content => crypto.createHash('sha1').update(`blob ${Buffer.byteLength(content)}\0`).update(content).digest('hex');
// Whole-file copies longer than the code-health oversized threshold are stored
// as <original name>.fixture so the repository scan does not count them as
// source. Loading maps them back to the original path with unchanged bytes;
// the checker never sees the suffix.
const FIXTURE_SUFFIX = '.fixture';
const SOURCE_NAME = /\.(?:tsx?|json|css)$/;
function fixtureFile(root, name) {
  const plain = path.join(root, name), stored = plain + FIXTURE_SUFFIX;
  const hasPlain = fs.existsSync(plain), hasStored = fs.existsSync(stored);
  if (hasPlain === hasStored) throw new Error(`fixture ${name} must exist exactly once (plain or ${FIXTURE_SUFFIX})`);
  return hasPlain ? plain : stored;
}
function readFixtureTree(root, prefix = '') {
  const files = {};
  function visit(rel) {
    for (const ent of fs.readdirSync(path.join(root, rel), {withFileTypes:true})) {
      if (ent.name === 'node_modules' || ent.name === 'dist' || ent.name.startsWith('.')) continue;
      const stored = path.posix.join(rel, ent.name);
      if (ent.isDirectory()) { visit(stored); continue; }
      const name = stored.endsWith(FIXTURE_SUFFIX) ? stored.slice(0, -FIXTURE_SUFFIX.length) : stored;
      if (stored !== name && !SOURCE_NAME.test(name)) throw new Error(`unexpected ${FIXTURE_SUFFIX} fixture ${stored}`);
      if (ent.isFile() && SOURCE_NAME.test(name)) {
        if (Object.hasOwn(files, prefix + name)) throw new Error(`duplicate fixture ${name}`);
        const content = fs.readFileSync(path.join(root, stored), 'utf8');
        files[prefix + name] = {content,blob:blob(content),mode:'100644'};
      }
    }
  }
  visit('');
  return files;
}
const baseline = {...readRepositoryTree(repo), ...readFixtureTree(fixtureRoot)};
function request(mutate, prepare=()=>{}) {
  const before = Object.fromEntries(Object.entries(baseline).map(([p,e]) => [p,{...e}]));
  prepare(before);
  for (const e of Object.values(before)) e.blob = blob(e.content);
  const result = Object.fromEntries(Object.entries(before).map(([p,e]) => [p,{...e}]));
  mutate(result);
  for (const e of Object.values(result)) e.blob = blob(e.content);
  return {schema:2,baseline:{files:before},result:{files:result},policies:manifest,key:{}};
}
function summary(out) {
  return out.findings.slice(0,18).map(f=>`${f.code} ${f.path}:${f.line} ${f.detail}`).join('\n');
}
test('frozen real fixtures have independently fixed content hashes', () => {
  const expected = JSON.parse(fs.readFileSync(path.join(here,'testdata/baseline-sha256.json'),'utf8'));
  assert.equal(Object.keys(expected).length,23);
  for (const [name,digest] of Object.entries(expected)) assert.equal(sha(fs.readFileSync(fixtureFile(fixtureRoot,name))),digest,name);
});
test('.fixture copies load at their original paths with their pre-rename bytes', () => {
  // SHA-256 of the files as committed before the rename (442ab73d).
  const renamed = {
    'console/src/components/market-data/MarketDataIngestTab.tsx': 'f9c5f4f4bccec3c6796200539ee967e6ea6eb8af03a8b458784810d65e0a2546',
    'console/src/pages/ResearchEnginePage.tsx': '15538524fa1e25739a9c9b1bafa623777007eadb8f6cd2e6487c4260a9c82cef',
  };
  const tree = readFixtureTree(fixtureRoot);
  assert.deepEqual(Object.keys(tree).filter(p => p.endsWith(FIXTURE_SUFFIX)), []);
  for (const [name,digest] of Object.entries(renamed)) {
    assert.ok(fs.existsSync(path.join(fixtureRoot, name + FIXTURE_SUFFIX)), name);
    assert.equal(sha(Buffer.from(tree[name].content, 'utf8')), digest, name);
    assert.equal(sha(Buffer.from(baseline[name].content, 'utf8')), digest, name);
  }
  const expected = JSON.parse(fs.readFileSync(path.join(here,'testdata/baseline-sha256.json'),'utf8'));
  assert.deepEqual(Object.keys(tree).sort(), Object.keys(expected).sort());
  for (const [name,digest] of Object.entries(expected)) assert.equal(sha(Buffer.from(baseline[name].content, 'utf8')), digest, name);
});
test('785/786 contract inventory remains exactly seven positive and fifteen negative', () => {
  assert.equal(contract.length,22);
  assert.equal(contract.filter(c=>!c.code).length,7);
  assert.equal(contract.filter(c=>c.code).length,15);
});
for (const group of [['contract',contract],['real-positive',positive],['counterexample',negative]]) {
  for (const spec of group[1]) test(`${group[0]} / ${spec.id}`, async () => {
    // Flush each expensive full-tree result before constructing the next program.
    await yieldTurn();
    const out = check(request(spec.mutate,spec.prepare));
    assert.ok(Array.isArray(out.findings), 'checker must produce actual findings');
    if (spec.code) assert.ok(out.findings.some(f=>f.code===spec.code),`${spec.id}: required ${spec.code}\n${summary(out)}`);
    else assert.deepEqual(out.findings,[],summary(out));
  });
}
test('a valid result cannot retroactively approve an unreviewed full baseline', () => {
  const req = request(()=>{});
  req.baseline = {files:{...req.baseline.files,[C1]:{...req.baseline.files[C1],content:req.baseline.files[C1].content+'\nconst injected = Date.now();\n'}}};
  const out = check(req);
  assert.ok(out.findings.some(f=>f.code==='S5_BASELINE_UNREVIEWED'),summary(out));
});
test('frozen DTO and test input deletion is detected', () => {
  const req = request(files=>{delete files['console/src/lib/market-data/__tests__/queueHistoryModel.test.ts'];});
  const out = check(req);
  assert.ok(out.findings.some(f=>['S5_BEHAVIOR','S5_TYPE_CONTRACT','S5_BINDING'].includes(f.code)),summary(out));
});
test('evidence metadata is deterministic and findings are stably sorted', () => {
  const mutation = files=>{files[C4].content += '\nconst escape = globalThis;\n';};
  const a = check(request(mutation));
  const b = check(request(mutation));
  assert.deepEqual(a,b);
  for (const key of ['callersDigest','typesDigest','testsDigest','policyDigest']) assert.match(a[key],/^[0-9a-f]{64}$/,key);
  assert.equal(a.toolchainDigest,undefined,'toolchainDigest is the image tool lock digest, bound outside the checker');
  const order = f => [f.path, f.line, f.column, f.code];
  const compare = (a,b) => {for(let i=0;i<a.length;i++){if(a[i]<b[i])return -1;if(a[i]>b[i])return 1;}return 0;};
  assert.deepEqual(a.findings.map(order),a.findings.map(order).sort(compare));
});
module.exports = {baseline,request};

test('canonical JSON sorts numeric-looking and astral keys identically to Go', () => {
  const input = {'2': 2, '10': 10, '\uE000': 'private', '\u{1F600}': 'astral', array: [3, 2, 1]};
  const expected = '{"10":10,"2":2,"array":[3,2,1],"😀":"astral","\uE000":"private"}';
  assert.equal(canonical(input), expected);
  assert.equal(sha256(input), sha(expected));
  assert.equal(canonical({'a': '<>&\u2028', number: -0}), '{"a":"<>&\u2028","number":0}');
  assert.throws(() => canonical({x: Infinity}), /non-finite/);
  assert.throws(() => canonical({x: NaN}), /non-finite/);
});

test('file-map insertion order cannot change canonical findings or identities', () => {
  const a = request(()=>{});
  const b = {...a, baseline:{files:Object.fromEntries(Object.entries(a.baseline.files).reverse())}, result:{files:Object.fromEntries(Object.entries(a.result.files).reverse())}};
  assert.deepEqual(check(b),check(a));
});

// Cross-language golden shared with Go (s5check ComputeMaterials/ToolchainDigest)
// and infra verify_materials.py: the image's pinned materials must be exactly the
// digests this checker reports, and the tool-lock canonical form must agree.
test('pinned CI materials golden equals the checker digests and lock canonical form', () => {
  const g = JSON.parse(fs.readFileSync(path.join(here, 'testdata/ci-materials-golden.json'), 'utf8'));
  const d = contractDigests(manifest);
  assert.deepEqual({profileDigest:d.policyDigest,callersDigest:d.callersDigest,typesDigest:d.typesDigest,testsDigest:d.testsDigest},
    {profileDigest:g.profileDigest,callersDigest:g.callersDigest,typesDigest:g.typesDigest,testsDigest:g.testsDigest});
  assert.equal(d.toolchainDigest, undefined);
  assert.equal(sha256(g.toolsLock), g.toolchainDigest);
  assert.equal(sha256(g.edgeToolsLock), g.edgeToolchainDigest);
});
