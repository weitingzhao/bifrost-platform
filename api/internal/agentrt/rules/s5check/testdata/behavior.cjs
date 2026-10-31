'use strict';
// Trusted behavior runner, not a security sandbox. Invoke only in the independent,
// network-disabled, resource-limited CI process specified by S5 §6.2.
//
// Three roles, one file (one trusted-suite hash):
//   main      reads and compiles the exact candidate files (never runs them) and,
//             per timezone, starts one executor and one verifier process.
//   --exec    the only process that runs candidate code. Each candidate module,
//             its original trusted test and its added boundary block share one VM
//             context with string/Wasm code generation disabled, so every input is
//             built inside that context and no host-realm object reaches the
//             candidate. The process itself runs with code generation from strings
//             disabled. It reports serialized call records only.
//   --verify  never loads candidate code. It re-runs the same trusted tests and
//             boundary blocks against the executor's recorded results, requires
//             every argument and the complete call sequence to equal its own, and
//             alone counts tests and assertions.
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const {spawnSync} = require('node:child_process');
const files = [
  'console/src/lib/patrol/cronSchedule.ts',
  'console/src/lib/research/signalHealthAgeMeters.ts',
  'console/src/lib/research/researchHealthCopy.ts',
  'console/src/lib/market-data/queueHistoryModel.ts',
];
const testFiles = files.map(p=>p.replace(/\/([^/]+)\.ts$/,'/__tests__/$1.test.ts'));
const aliases = files.map(f=>'@/'+f.slice('console/src/'.length,-3));
// The unrelated massiveNav section of the C2 test is not candidate coverage (§2.2).
const EXCLUDED_IMPORT = '@/lib/research/massiveNav', EXCLUDED_SUITE = 'massiveNav';
const MATCHERS = ['toBe','toEqual','toBeNull','toContain','toMatch','toHaveLength','toBeCloseTo'];
const NO_CODEGEN = '--disallow-code-generation-from-strings';
const MAX_RECORD_BYTES = 4 << 20;

// Added boundaries, one block per candidate module, run in both processes with
// (m, a, ok, harness). In the executor a/ok are no-ops; the verifier counts them.
const BOUNDARIES = [
`const instant = Date.parse('2026-08-31T21:59:59Z');
const before = harness.clockReads();
a(m.formatCountdownTo('2026-08-31T22:00:59Z', instant), 'in 1m');
a(m.formatCountdownTo('invalid', instant), '—');
a(harness.clockReads(), before);
for (const invalid of ['*/0 * * * *', '*/-1 * * * *', 'x * * * *', '0 0 * *']) a(m.iterCronFiresUtc(invalid, new Date(0), new Date(60000)), []);
a(m.formatNextRunAt('2000-01-01T00:00:00Z'), new Date('2000-01-01T00:00:00Z').toLocaleString());`,
`for (const [instant, sla] of [['2026-08-31T21:59:59Z', 72], ['2026-08-31T22:00:00Z', 36], ['2026-09-01T12:00:00Z', 36], ['2026-09-05T12:00:00Z', 72]]) {
  harness.setInstant(instant);
  const before = harness.clockReads();
  a(m.freshnessSlaHours(), sla);
  a(harness.clockReads() - before, 1);
}
for (const invalid of [NaN, Infinity, -1, undefined]) a(m.ageToFillPct(invalid), 0);
a(m.ageToFillPct(5, 0), 100);
a(m.stackFreshnessStatuses([]), {fresh: 0, stale: 0, other: 0, total: 0, readyPct: 0, thinPct: 0, blockedPct: 0});`,
`a(m.buildResearchVerdictCopy({loading: true}).lamp, 'unknown');
a(m.buildResearchVerdictCopy({reachable: false}).lamp, 'fail');
a(m.buildResearchVerdictCopy({reachable: false}).summary, 'Research API unreachable');
a(m.layerVerdictToLamp('unknown'), 'degraded');
a(m.buildResearchHealthLayers({})[0].meta, 'upstream unprobed');`,
`const point = (t, pending, donePerMin) => ({t, pending, running: null, donePerMin, createdPerMin: 0, failedPerMin: 0, oldestPendingHours: null, p95Sec: null});
const points = [point(0, 1, 1), point(100, null, 2), point(200, 3, 3)];
const box = {w: 200, h: 30, max: 3, t0: 0, t1: 200};
for (const [field, expected] of [['pending', [0, 200]], ['donePerMin', []]]) {
  const select = p => p[field];
  a(m.isolatedPoints(points, select, 100).map(p => p.t), expected);
  ok(m.linePath(points, select, box, {gapMs: 100}).length > 0);
  ok(m.areaPath(points, select, box, {gapMs: 100}).length > 0);
  a(m.linePath(points, select, {...box, t1: 0}), '');
}
const localTimestamp = '2000-01-01T00:00:00';
const series = m.buildQueueSeries([{sample_ts: 'invalid'}, {sample_ts: localTimestamp, pending: 1, done_delta: 2}], 60);
a(series.points.length, 1);
a(series.points[0].t, Date.parse(localTimestamp));
a(series.points[0].donePerMin, 2);
harness.report(series.points[0].t);
a(m.buildQueueSeries(undefined, 60).latest, null);
a(m.compactCount(NaN), '—');
a(m.compactCount(Infinity), '—');`,
];

// Serialized value form shared by both sides. The executor's copy is created
// inside the candidate context before candidate code runs; the verifier's copy
// runs on its own values. Only plain data, Dates and (as arguments) function
// source text are representable; anything else is marked and fails closed.
const SERIALIZER = `(() => {
  const isArray = Array.isArray, getProto = Object.getPrototypeOf, ObjectProto = Object.prototype;
  const ownKeys = Reflect.ownKeys, describe = Object.getOwnPropertyDescriptor, getTime = Date.prototype.getTime;
  const fnSource = Function.prototype.toString, call = Function.prototype.call, is = Object.is;
  const isDate = v => { try { call.call(getTime, v); return true; } catch { return false; } };
  const index = (k, n) => String(k >>> 0) === k && (k >>> 0) < n;
  return function ser(v, depth = 0) {
    if (depth > 32) return {$: 'unsupported', v: 'depth'};
    if (v === undefined) return {$: 'undefined'};
    if (v === null || typeof v === 'boolean' || typeof v === 'string') return v;
    if (typeof v === 'number') {
      if (v !== v) return {$: 'number', v: 'NaN'};
      if (v === Infinity || v === -Infinity) return {$: 'number', v: v > 0 ? 'Infinity' : '-Infinity'};
      if (is(v, -0)) return {$: 'number', v: '-0'};
      return v;
    }
    if (typeof v === 'function') return {$: 'function', v: call.call(fnSource, v)};
    if (typeof v !== 'object') return {$: 'unsupported', v: typeof v};
    if (isDate(v)) return {$: 'date', v: ser(call.call(getTime, v), depth + 1)};
    const array = isArray(v), keys = ownKeys(v), out = {};
    for (let i = 0; i < keys.length; i++) {
      const k = keys[i];
      if (typeof k !== 'string') return {$: 'unsupported', v: 'symbol key'};
      if (array && k === 'length') continue;
      if (array && !index(k, v.length)) return {$: 'unsupported', v: 'array property ' + k};
      const d = describe(v, k);
      if (!('value' in d)) return {$: 'unsupported', v: 'accessor ' + k};
      out[k] = ser(d.value, depth + 1);
    }
    if (array) {
      const items = [];
      for (let i = 0; i < v.length; i++) items[i] = String(i) in out ? out[String(i)] : {$: 'undefined'};
      return {$: 'array', v: items};
    }
    const proto = getProto(v);
    if (proto !== null && proto !== ObjectProto && getProto(proto) !== null) return {$: 'unsupported', v: 'prototype'};
    return {$: 'object', v: out};
  };
})()`;

// Executor prelude: everything the trusted test and boundary need is defined
// here, inside the context. No host-side object is placed in the context.
// Executor prelude, one per module: everything the trusted test and boundary
// need is defined here, inside the context, and the context is then sealed.
// Clock state lives only in this closure; Date is a plain wrapper function (its
// prototype chain does not reach the callable original), and Date.prototype
// .constructor points at the wrapper. The global object and every intrinsic
// reachable from it are frozen, so no later script writes any global binding.
const prelude = alias => `'use strict';
(() => {
  const OriginalDate = Date, construct = Reflect.construct, define = Object.defineProperty;
  let clockReads = 0, instant = '2026-08-31T21:59:59Z';
  const FrozenDate = function Date(...args) {
    if (!new.target) { clockReads++; return construct(OriginalDate, [instant], OriginalDate).toString(); }
    if (!args.length) clockReads++;
    return construct(OriginalDate, args.length ? args : [instant], new.target);
  };
  define(FrozenDate, 'prototype', {value: OriginalDate.prototype, writable: false});
  define(FrozenDate, 'parse', {value: OriginalDate.parse});
  define(FrozenDate, 'UTC', {value: OriginalDate.UTC});
  define(FrozenDate, 'now', {value: function now() { clockReads++; return OriginalDate.parse(instant); }});
  define(OriginalDate.prototype, 'constructor', {value: FrozenDate, writable: false, enumerable: false, configurable: false});
  define(globalThis, 'Date', {value: FrozenDate, writable: false, enumerable: false, configurable: false});

  const ser = ${SERIALIZER}, stringify = JSON.stringify, keysOf = Object.keys;
  const create = Object.create, apply = Reflect.apply;
  // Records are null-prototype objects written with the captured defineProperty:
  // no inherited setter or accessor can intercept or rewrite them.
  const put = (o, k, v) => define(o, k, {value: v, enumerable: true, writable: false, configurable: false});
  const calls = create(null), exportsLog = create(null), noop = () => {};
  let count = 0, recorded;
  const record = (name, fn) => function (...args) {
    const entry = create(null);
    put(entry, 'fn', name); put(entry, 'args', ser(args));
    put(calls, count++, entry);
    const result = apply(fn, undefined, args);
    put(entry, 'result', ser(result));
    return result;
  };
  const matchers = {};
  for (const name of ${JSON.stringify(MATCHERS)}) matchers[name] = noop;
  const expectation = {...matchers, not: matchers};
  const vitest = {expect: () => expectation, describe: (name, fn) => { if (name !== ${JSON.stringify(EXCLUDED_SUITE)}) fn(); }, it: (_name, fn) => { fn(); }};
  const harness = {
    load(candidateExports) {
      if (recorded) throw new Error('candidate already loaded');
      recorded = create(null);
      for (const key of keysOf(candidateExports)) {
        const value = candidateExports[key];
        put(exportsLog, key, typeof value === 'function' ? {$: 'export-function'} : ser(value));
        put(recorded, key, typeof value === 'function' ? record(key, value) : value);
      }
    },
    require(name) {
      if (name === 'vitest') return vitest;
      if (name === ${JSON.stringify(alias)} && recorded) return recorded;
      if (name === ${JSON.stringify(EXCLUDED_IMPORT)}) return Object.freeze({});
      throw new Error('unregistered trusted-test import: ' + name);
    },
    module: () => recorded,
    boundary: {setInstant: record('$setInstant', iso => { instant = iso; }), clockReads: record('$clockReads', () => clockReads), report: noop},
    noop,
    emit: () => { const list = []; for (let i = 0; i < count; i++) put(list, i, calls[i]); return stringify({exports: exportsLog, calls: list}); },
  };
  define(globalThis, '__harness', {value: harness, writable: false, enumerable: false, configurable: false});

  const freeze = Object.freeze, getProto = Object.getPrototypeOf, ownKeys = Reflect.ownKeys, describe = Object.getOwnPropertyDescriptor;
  const seen = new Set();
  const visit = v => {
    if (v === null || (typeof v !== 'object' && typeof v !== 'function') || v === globalThis || seen.has(v)) return;
    seen.add(v); freeze(v);
    for (const k of ownKeys(v)) { const d = describe(v, k); if ('value' in d) visit(d.value); else { visit(d.get); visit(d.set); } }
    visit(getProto(v));
  };
  // The VM global object cannot be frozen or made non-extensible; every existing
  // global binding is made read-only and non-configurable instead (full
  // descriptors: the VM proxy drops omitted members). A candidate can only add
  // new globals, which nothing in the harness reads.
  // Intl formatters (and Temporal, where present) read the wall clock without a
  // date argument. Neither the candidates nor the trusted tests use them, so
  // they are removed rather than wrapped.
  for (const k of ['Intl', 'Temporal']) if (describe(globalThis, k)) define(globalThis, k, {value: undefined, writable: false, enumerable: false, configurable: false});
  for (const k of ownKeys(globalThis)) {
    const d = describe(globalThis, k);
    if (!d.configurable) continue;
    define(globalThis, k, 'value' in d ? {value: d.value, writable: false, enumerable: d.enumerable, configurable: false} : {get: d.get, set: d.set, enumerable: d.enumerable, configurable: false});
  }
  // Roots: every global value and the intrinsics only reachable from syntax
  // (iterator, generator, async and async-generator function chains).
  for (const k of ownKeys(globalThis)) { const d = describe(globalThis, k); if ('value' in d) visit(d.value); else { visit(d.get); visit(d.set); } }
  visit(getProto(globalThis));
  const roots = [OriginalDate, [][Symbol.iterator](), ''[Symbol.iterator](), new Map().entries(), new Set().values(),
    /x/[Symbol.matchAll]('x'), function* () {}, (function* () {})(), async function () {}, async () => {},
    async function* () {}, (async function* () {})()];
  for (const root of roots) visit(root);
})();`;

const sha = s=>crypto.createHash('sha256').update(s).digest('hex');
function exactSource(root, name) {
  const resolved = path.resolve(root,name);
  const rel = path.relative(root,resolved);
  if (rel.startsWith('..') || path.isAbsolute(rel)) throw Error('unsafe source path');
  let current = path.resolve(root);
  for (const part of rel.split(path.sep)) {
    current = path.join(current,part);
    if (fs.lstatSync(current).isSymbolicLink()) throw Error('symlink source rejected');
  }
  const source = fs.readFileSync(resolved,'utf8');
  if (Buffer.byteLength(source)>256*1024) throw Error('source exceeds trusted resource bound');
  return source;
}
function compile(ts, source) {
  const out = ts.transpileModule(source,{reportDiagnostics:true,compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}});
  if (out.diagnostics?.some(d=>d.category===ts.DiagnosticCategory.Error)) throw Error('candidate transpilation failed');
  return out.outputText;
}
function trustedTests(ts) {
  const hashes = JSON.parse(fs.readFileSync(path.join(__dirname,'baseline-sha256.json'),'utf8'));
  return testFiles.map(testFile => {
    const source = fs.readFileSync(path.join(__dirname,'baseline',testFile),'utf8');
    assert.equal(sha(source),hashes[testFile],'trusted original test hash');
    return compile(ts,source);
  });
}

// --exec: candidate code runs only here.
function execute(job) {
  const reports = job.modules.map((candidate,i) => {
    const context = vm.createContext(Object.create(null),{codeGeneration:{strings:false,wasm:false}});
    const run = (source,timeout=1000) => vm.runInContext(source,context,{timeout});
    run(prelude(aliases[i]));
    // No script below writes a global: the context is sealed by the prelude.
    run(`__harness.load((function (exports) {\n${candidate}\n;return exports;})(Object.create(null)));`);
    run(`(function (require, exports) {\n${job.tests[i]}\n})(__harness.require, Object.create(null));`,5000);
    run(`(function (m, a, ok, harness) {\n${BOUNDARIES[i]}\n})(__harness.module(), __harness.noop, __harness.noop, __harness.boundary);`,5000);
    const report = run('__harness.emit()');
    if (typeof report !== 'string') throw Error('executor report is not a string');
    return JSON.parse(report);
  });
  return JSON.stringify({schema:1,modules:reports});
}

// --verify: no candidate code is loaded; results are replayed as plain data.
const ser = vm.runInThisContext(SERIALIZER);
const NUMBERS = {NaN, Infinity, '-Infinity': -Infinity, '-0': -0};
function deserialize(v) {
  if (v === null || typeof v !== 'object') return v;
  if (v.$ === 'undefined') return undefined;
  if (v.$ === 'number' && Object.hasOwn(NUMBERS, v.v)) return NUMBERS[v.v];
  if (v.$ === 'date') return new Date(deserialize(v.v));
  if (v.$ === 'array' && Array.isArray(v.v)) return v.v.map(deserialize);
  if (v.$ === 'object' && v.v && typeof v.v === 'object' && !Array.isArray(v.v)) return Object.fromEntries(Object.entries(v.v).map(([k,x])=>[k,deserialize(x)]));
  throw Error('executor returned a non-data value: '+JSON.stringify(v).slice(0,200));
}
function verify(tests, raw) {
  if (typeof raw !== 'string' || Buffer.byteLength(raw) > MAX_RECORD_BYTES) throw Error('executor record missing or too large');
  const record = JSON.parse(raw);
  if (record?.schema !== 1 || !Array.isArray(record.modules) || record.modules.length !== files.length) throw Error('executor record has the wrong shape');
  const copy = value => JSON.parse(JSON.stringify(value));
  let assertions = 0, testCount = 0, parseMillis;
  const check = fn => { assertions++; fn(); };
  function expect(actual) {
    const make = negated => {
      const result = {};
      const matches = {
        toBe: expected=>Object.is(actual,expected),
        toEqual: expected=>{try{assert.deepEqual(copy(actual),copy(expected));return true;}catch{return false;}},
        toBeNull: ()=>actual===null,
        toContain: expected=>actual?.includes(expected),
        toMatch: expected=>typeof expected==='string' ? String(actual).includes(expected) : expected.test(actual),
        toHaveLength: expected=>actual?.length===expected,
        toBeCloseTo: (expected,digits=2)=>Math.abs(actual-expected)<Math.pow(10,-digits)/2,
      };
      for(const [name,predicate] of Object.entries(matches)) result[name]=(...args)=>check(()=>assert.equal(Boolean(predicate(...args)),!negated,`${name}: ${JSON.stringify(actual)} vs ${JSON.stringify(args)}`));
      if (!negated) Object.defineProperty(result,'not',{get:()=>make(true)});
      return result;
    };
    return make(false);
  }
  const vitest = {expect,describe:(name,fn)=>{if(name!==EXCLUDED_SUITE)fn();},it:(_name,fn)=>{testCount++;fn();}};
  const a = (actual,expected) => check(()=>assert.deepEqual(copy(actual),copy(expected)));
  const ok = value => check(()=>assert.ok(value));
  files.forEach((file,i) => {
    const {exports,calls} = record.modules[i] || {};
    if (!exports || typeof exports !== 'object' || !Array.isArray(calls)) throw Error('executor record has the wrong shape');
    let next = 0;
    const replay = name => (...args) => {
      const entry = calls[next++];
      if (!entry) throw Error(`${file}: executor recorded fewer calls than the trusted suite makes`);
      assert.equal(entry.fn,name,`${file}: call ${next-1} differs from the trusted suite`);
      assert.deepEqual(entry.args,ser(args),`${file}: arguments of call ${next-1} (${name}) differ from the trusted suite`);
      return deserialize(entry.result);
    };
    const module = {};
    for (const [key,value] of Object.entries(exports)) module[key] = value?.$ === 'export-function' ? replay(key) : deserialize(value);
    vm.runInThisContext(`(function (require, exports) {\n${tests[i]}\n})`)(name => {
      if (name === 'vitest') return vitest;
      if (name === aliases[i]) return module;
      if (name === EXCLUDED_IMPORT) return Object.freeze({});
      throw Error('unregistered trusted-test import: '+name);
    },{});
    const harness = {setInstant:replay('$setInstant'),clockReads:replay('$clockReads'),report:value=>{parseMillis=value;}};
    vm.runInThisContext(`(function (m, a, ok, harness) {\n${BOUNDARIES[i]}\n})`)(module,a,ok,harness);
    if (next !== calls.length) throw Error(`${file}: executor recorded calls the trusted suite did not make`);
  });
  return {timezone:process.env.TZ,tests:testCount,assertions,parseMillis};
}

function child(role, input, TZ) {
  const result = spawnSync(process.execPath,[NO_CODEGEN,__filename,role],{input,encoding:'utf8',env:{TZ,LANG:'en_US.UTF-8'},timeout:15000,maxBuffer:MAX_RECORD_BYTES});
  if (result.error || result.status !== 0) throw Error(`trusted behavior ${role} failed for ${TZ}: ${result.error?.message||result.stderr}`);
  return result.stdout;
}
// Compiles (never runs) the exact candidate files and the trusted original tests.
function prepare(root) {
  const ts = require('typescript');
  assert.equal(ts.version,'5.9.3','trusted compiler version');
  return {modules:files.map(f=>compile(ts,exactSource(root,f))),tests:trustedTests(ts)};
}
function main(args) {
  if(args.length!==2||args[0]!=='--root')throw Error('usage: node behavior.cjs --root READ_ONLY_RESULT_TREE');
  const job = prepare(path.resolve(args[1]));
  const outputs=[];
  for(const TZ of ['UTC','America/Chicago']) {
    const record = child('--exec',JSON.stringify(job),TZ);
    outputs.push(JSON.parse(child('--verify',JSON.stringify({tests:job.tests,record}),TZ)));
  }
  assert.notEqual(outputs[0].parseMillis,outputs[1].parseMillis,'baseline parsing still uses local timezone');
  process.stdout.write(JSON.stringify({schema:1,runs:outputs})+'\n');
}
if(require.main===module) {
  try {
    const role = process.argv[2];
    if (role === '--exec') process.stdout.write(execute(JSON.parse(fs.readFileSync(0,'utf8'))));
    else if (role === '--verify') { const input = JSON.parse(fs.readFileSync(0,'utf8')); process.stdout.write(JSON.stringify(verify(input.tests,input.record))); }
    else main(process.argv.slice(2));
  } catch(error) {process.stderr.write(String(error.stack||error)+'\n');process.exitCode=1;}
}
module.exports={prepare,verify,NO_CODEGEN};
