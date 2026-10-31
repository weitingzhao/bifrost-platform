'use strict';
const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const os=require('node:os');
const path=require('node:path');
const {spawnSync}=require('node:child_process');
const {C1,C2,C3,C4,positive,negative}=require('./cases.cjs');
const files=[C1,C2,C3,C4];
function run(mutate=()=>{},extra=()=>{}) {
  const root=fs.mkdtempSync(path.join(os.tmpdir(),'s5-behavior-'));
  try {
    const tree=Object.fromEntries(files.map(f=>[f,{content:fs.readFileSync(path.join(__dirname,'baseline',f),'utf8')} ]));
    mutate(tree);
    for(const [name,entry] of Object.entries(tree)) {
      fs.mkdirSync(path.dirname(path.join(root,name)),{recursive:true});
      fs.writeFileSync(path.join(root,name),entry.content);
    }
    extra(root);
    return spawnSync(process.execPath,[path.join(__dirname,'behavior.cjs'),'--root',root],{encoding:'utf8',env:process.env,timeout:35000,maxBuffer:1024*1024});
  } finally {fs.rmSync(root,{recursive:true,force:true});}
}
test('trusted original assertions and added boundaries run in both timezones',()=>{
  const result=run();
  assert.equal(result.status,0,result.stderr);
  const output=JSON.parse(result.stdout);
  assert.deepEqual(output.runs.map(r=>r.timezone),['UTC','America/Chicago']);
  for(const row of output.runs){assert.equal(row.tests,30);assert.equal(row.assertions,122);}
  assert.notEqual(output.runs[0].parseMillis,output.runs[1].parseMillis);
});
for(const spec of positive.filter(p=>p.id.startsWith('P')))test('trusted behavior / '+spec.id,()=>{
  const result=run(spec.mutate);assert.equal(result.status,0,result.stderr);
});
for(const id of ['required_date_guard_deleted','required_unreachable_guard_inverted','required_missing_filter_deleted'])test('independent behavioral rejection / '+id,()=>{
  const result=run(negative.find(c=>c.id===id).mutate);assert.notEqual(result.status,0,'guard mutation must fail trusted assertions');
});
test('candidate passed.json cannot turn failing assertions green',()=>{
  const result=run(negative.find(c=>c.id==='required_date_guard_deleted').mutate,root=>fs.writeFileSync(path.join(root,'passed.json'),'{"passed":true,"assertions":999}'));
  assert.notEqual(result.status,0);
});
test('missing candidate input is failure',()=>{
  const result=run(()=>{},root=>fs.unlinkSync(path.join(root,C1)));assert.notEqual(result.status,0);
});
test('candidate dynamic string evaluation is unavailable in behavior VM',()=>{
  const result=run(tree=>{tree[C1].content+='\nFunction("return 1")();\n';});assert.notEqual(result.status,0);assert.match(result.stderr,/Code generation from strings disallowed/);
});

// Review B-1: the candidate executes only in the executor process, with every
// input built inside its own VM context; counting happens in the verifier.
const forge=`
  try {
    const P = (start as any).constructor.constructor('return process')()
    const fs = P.getBuiltinModule('fs')
    fs.writeSync(1, JSON.stringify({timezone: P.env.TZ, tests: 30, assertions: 122, parseMillis: Date.parse('2000-01-01T00:00:00')}))
    P.reallyExit(0)
  } catch {}
`;
const withForge=tree=>{tree[C1].content=tree[C1].content.replace('export function iterCronFiresUtc(expr: string, start: Date, end: Date): Date[] {\n','export function iterCronFiresUtc(expr: string, start: Date, end: Date): Date[] {\n'+forge);};
const invertedFinite=tree=>{tree[C4].content=tree[C4].content.replace('if (n == null || !Number.isFinite(n)) return "—";','if (n == null) return "—";');};
test('suite inputs give the candidate no host realm: the forge payload cannot escape',()=>{
  const result=run(tree=>{withForge(tree);assert.ok(tree[C1].content.includes('constructor.constructor'));});
  assert.equal(result.status,0,result.stderr);
  for(const row of JSON.parse(result.stdout).runs){assert.equal(row.tests,30);assert.equal(row.assertions,122);}
});
test('a forged report cannot turn a failing guard green (behavior-forge regression)',()=>{
  const result=run(tree=>{invertedFinite(tree);withForge(tree);assert.ok(tree[C4].content.includes('if (n == null) return "—";'));});
  assert.notEqual(result.status,0);
  assert.equal(result.stdout,'');
});
test('the verifier alone counts: forged or altered executor records are refused',()=>{
  const {prepare,verify,NO_CODEGEN}=require('./behavior.cjs');
  const job=prepare(path.join(__dirname,'baseline'));
  const exec=spawnSync(process.execPath,[NO_CODEGEN,path.join(__dirname,'behavior.cjs'),'--exec'],{input:JSON.stringify(job),encoding:'utf8',env:{TZ:'UTC',LANG:'en_US.UTF-8'}});
  assert.equal(exec.status,0,exec.stderr);
  const genuine=JSON.parse(exec.stdout);
  const mutate=fn=>{const r=JSON.parse(exec.stdout);fn(r);return JSON.stringify(r);};
  // The parent process here may run in any timezone; only shape/sequence refusals are asserted.
  for(const [why,record] of [
    ['old self-reported counts',JSON.stringify({schema:1,runs:[{timezone:'UTC',tests:30,assertions:122,parseMillis:0}]})],
    ['no recorded calls',mutate(r=>{for(const m of r.modules)m.calls=[];})],
    ['dropped final call',mutate(r=>{r.modules[3].calls.pop();})],
    ['extra unrequested call',mutate(r=>{r.modules[2].calls.push(r.modules[2].calls[0]);})],
    ['altered argument',mutate(r=>{r.modules[0].calls[0].args=['0 5 * * *'];})],
    ['non-data result',mutate(r=>{r.modules[2].calls[0].result={$:'unsupported',v:'accessor lamp'};})],
    ['wrong result',mutate(r=>{const c=r.modules[3].calls.find(x=>x.fn==='compactCount');c.result='forged';})],
  ])assert.throws(()=>verify(job.tests,record),undefined,why);
  assert.ok(genuine.modules.every(m=>m.calls.length>0));
});

// Review B-1': the executor's own records cannot be rewritten from inside the
// candidate's context (prototype setters, replaced harness) once it loads.
const protoForge=`
;(Object as any).defineProperty((Object as any).prototype, 'result', { configurable: true, set(this: any, v: any) {
  const a = this.args && this.args.v && this.args.v[0]
  const bad = this.fn === 'compactCount' && a && a.$ === 'number' && (a.v === 'NaN' || a.v === 'Infinity' || a.v === '-Infinity')
  ;(Object as any).defineProperty(this, 'result', { value: bad ? '—' : v, enumerable: true, writable: true, configurable: true })
} })
`;
test('a prototype setter cannot rewrite executor records (behavior-forge2 regression)',()=>{
  const result=run(tree=>{invertedFinite(tree);tree[C4].content+=protoForge;});
  assert.notEqual(result.status,0);
  assert.equal(result.stdout,'');
});
test('intrinsics are frozen and the harness binding is fixed before candidate code runs',()=>{
  for(const payload of [
    "\n;(Array as any).prototype.push = () => 0\n",
    "\n;(JSON as any).stringify = () => '{}'\n",
    "\n;(globalThis as any).__harness = { emit: () => '{\"exports\":{},\"calls\":[]}' }\n",
    "\n;(Object as any).defineProperty((Object as any).prototype, 'toJSON', { value: () => ({}) })\n",
  ]){
    const result=run(tree=>{tree[C4].content+=payload;});
    assert.notEqual(result.status,0,payload);
    assert.match(result.stderr,/--exec failed/,payload);
  }
});

// Review B-1'': clock state is closure-only, Date does not lead to the callable
// original, and the async/generator intrinsic chains are frozen too.
const inSla=code=>tree=>{tree[C2].content=tree[C2].content.replace(/export function freshnessSlaHours\(([^)]*)\)([^{]*)\{/,m=>m+'\n'+code);};
for(const [id,mutate] of [
  ['F2 an extra clock read cannot be hidden through a global counter',inSla('  new Date(); (globalThis as any).__clockReads--;')],
  ['F3 the original Date is not reachable through the wrapper prototype',inSla('  const OD: any = (Object as any).getPrototypeOf(Date); const real = new OD(); if (real.getTime() < 0) return 0;')],
  ['F3 an instance constructor is the counting wrapper',inSla('  const C: any = new Date(0).constructor; const real = new C(); if (real.getTime() < 0) return 0;')],
  ['F7 async generator prototypes are frozen',tree=>{tree[C4].content+='\n;(Object as any).getPrototypeOf(async function* () {}).prototype.__x = 1\n';}],
])test(id,()=>{const result=run(mutate);assert.notEqual(result.status,0);assert.equal(result.stdout,'');});

// Review B-1''': Intl formatters read the wall clock without counting; the
// executor does not provide Intl (or Temporal) at all.
test('F8 Intl.DateTimeFormat cannot read the real clock',()=>{
  const result=run(inSla("  const parts: any = new (globalThis as any).Intl.DateTimeFormat('en-US', {year: 'numeric'}).format(); if (parts === 'x') return 0;"));
  assert.notEqual(result.status,0);assert.equal(result.stdout,'');
});
