'use strict';
// Mutants exist only in this test's memory. No flags or weakened policies are
// accepted by the production checker; the full-CI test always uses every rule.
const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const Module=require('node:module');
const {createContext}=require('../ts/compiler.cjs');
const {readTree}=require('../ts/enroll.cjs');
const {C1,C3,C4,contract,negative}=require('./cases.cjs');
const root=path.resolve(__dirname,'../../../../../..');
const originalFiles=readTree(root);
const manifest=JSON.parse(fs.readFileSync(path.join(__dirname,'../policies/manifest.json'),'utf8'));
const baseContext=createContext(originalFiles);
function loadMutant(file,from,to) {
  const filename=path.join(__dirname,'../ts',file);
  const source=fs.readFileSync(filename,'utf8');
  assert.equal(source.split(from).length-1,1,'Mutation anchor must match exactly once: '+from);
  const mod=new Module(filename,module);
  mod.filename=filename;mod.paths=Module._nodeModulePaths(path.dirname(filename));
  mod._compile(source.replace(from,to),filename);
  return mod.exports;
}
function ctx(spec) {
  const files=Object.fromEntries(Object.entries(originalFiles).map(([f,e])=>[f,{...e}]));
  spec.mutate(files);return createContext(files);
}
const records=[
  {id:'M1 global symbol rule',case:'localStorage_alias_read',file:'analyze.cjs',from:"add('S5_GLOBAL_UNKNOWN', n, `Unregistered value ${name}`);",to:'void name;',source:C1},
  {id:'M2/M4 explicit type import rule',case:'type_to_value_import',file:'analyze.cjs',from:'if (!c.isTypeOnly && !s.isTypeOnly)',to:'if (false && !c.isTypeOnly && !s.isTypeOnly)',source:C4},
  {id:'M3 required guard rule',case:'required_cron_guard_deleted',file:'contracts.cjs',from:'for(const guard of policy.requiredGuards)',to:'for(const guard of [])'},
  {id:'M5/M16 selector exact edge rule',case:'C4_extra_map',file:'contracts.cjs',from:'if(file===CANDIDATES[3]&&!equal(selectorRefs,policy.selectorReferences))',to:'if(false && file===CANDIDATES[3]&&!equal(selectorRefs,policy.selectorReferences))'},
  {id:'M16 selector filter predicate',case:'selector_filter_predicate_changed',file:'contracts.cjs',from:'if(file===CANDIDATES[3]&&!equal(selectorRefs,policy.selectorReferences))',to:'if(false && file===CANDIDATES[3]&&!equal(selectorRefs,policy.selectorReferences))'},
  {id:'M16 selector loop input',case:'selector_loop_input_changed',file:'contracts.cjs',from:'if(file===CANDIDATES[3]&&!equal(selectorRefs,policy.selectorReferences))',to:'if(false && file===CANDIDATES[3]&&!equal(selectorRefs,policy.selectorReferences))'},
  {id:'M6 fresh local date rule',case:'borrowed_date_setter',file:'analyze.cjs',from:"if (!r.fresh || r.module) add('S5_MUTATION_ESCAPE', n, 'Date setter requires a fresh local Date');",to:'// Test mutant: omitted fresh-local requirement.',source:C1},
  {id:'M7 prototype capability rule',case:'prototype_constructor_chain',file:'analyze.cjs',from:"const PROTOTYPE = new Set(['constructor', 'prototype', '__proto__']);",to:'const PROTOTYPE = new Set();',source:C1},
  {id:'M15 frozen Date.parse rule',case:'786_parse',file:'contracts.cjs',from:"for(const kind of ['clock','locale-format','date-parse'])",to:"for(const kind of ['clock','locale-format'])"},
  {id:'M15 same-file consumer rule',case:'786_clock_relay',file:'contracts.cjs',from:'if(!equal(actual,expected)){',to:'if(false && !equal(actual,expected)){'},
  {id:'M15 join element rule',case:'786_join',file:'analyze.cjs',from:"if (r.element?.tag !== 'scalar' || !isScalarType(r.element.type)) add('S5_IMPLICIT_COERCION', n, 'join elements must all be scalar/nullish, including helper results');",to:'// Test mutant: omitted join element source requirement.',source:C1},
  {id:"A-1 conditional branch writes reach later reads",case:"B1_conditional_branch_assignment_join_Date",file:'analyze.cjs',from:"return merge(expression(n.whenTrue, scope), expression(n.whenFalse, scope));",to:"return merge(expression(n.whenTrue, new Map(scope)), expression(n.whenFalse, new Map(scope)));",source:C1},
  {id:"A-1 closures read captured labels at the reference site",case:"B2_arrow_stale_capture_join_Date",file:'analyze.cjs',from:"return v?.tag === 'closure' ? { ...v, captures: scope } : v; }",to:"return v; }",source:C1},
  {id:"A-1 catch starts from pre-try and end-of-try labels",case:"B3_catch_scope_join_Date",file:'analyze.cjs',from:"const no = new Map(scope); mergeScopes(no, [scope, yes]);",to:"const no = new Map(scope);",source:C1},
  {id:"A-1 conditional alias write to borrowed input",detail:"Borrowed, nested borrowed and module data are read-only",case:"B4_conditional_alias_write_borrowed_input",file:'analyze.cjs',from:"return merge(expression(n.whenTrue, scope), expression(n.whenFalse, scope));",to:"return merge(expression(n.whenTrue, new Map(scope)), expression(n.whenFalse, new Map(scope)));",source:C3},
  {id:"A-1 conditional alias write to module constant",detail:"Borrowed, nested borrowed and module data are read-only",case:"B5_conditional_alias_write_module_RANK",file:'analyze.cjs',from:"return merge(expression(n.whenTrue, scope), expression(n.whenFalse, scope));",to:"return merge(expression(n.whenTrue, new Map(scope)), expression(n.whenFalse, new Map(scope)));",source:C3},
  {id:"A-1 catch alias write to module constant",detail:"Borrowed, nested borrowed and module data are read-only",case:"B6_catch_alias_write_module_RANK",file:'analyze.cjs',from:"const no = new Map(scope); mergeScopes(no, [scope, yes]);",to:"const no = new Map(scope);",source:C3},
  {id:"A-1 conditional alias Date setter on borrowed start",case:"B7_conditional_alias_Date_setter_on_borrowed_start",file:'analyze.cjs',from:"return merge(expression(n.whenTrue, scope), expression(n.whenFalse, scope));",to:"return merge(expression(n.whenTrue, new Map(scope)), expression(n.whenFalse, new Map(scope)));",source:C1},
  {id:"A-1 statement-level writes only",case:"expression_level_assignment",file:'analyze.cjs',from:"if (ts.isExpressionStatement(p) || (ts.isForStatement(p)",to:"if (true || ts.isExpressionStatement(p) || (ts.isForStatement(p)",source:C1},
  {id:"A-1 switch fallthrough labels",case:"switch_fallthrough_label",file:'analyze.cjs',from:"if (branches.length) mergeScopes(branch, [scope, branches[branches.length - 1]]);",to:"// Test mutant: no fallthrough merge.",source:C1},
  {id:"A-1 closure in a branch sees the branch write",case:"closure_called_in_branch_after_write",file:'analyze.cjs',from:"return v?.tag === 'closure' ? { ...v, captures: scope } : v; }",to:"return v; }",source:C1},
  {id:"C2 uncalled nested declaration is inspected",case:"nested_function_uncalled_fetch",file:'analyze.cjs',from:"callClosure(closure, n.parameters.map(p => fromType(type(p.name))), n.parameters.map(p => p.name), n);\n      return;",to:"return;",source:C1},
  {id:"C1 unknown receiver method is a write escape",case:"localStorage_alias_write_escape",file:'analyze.cjs',from:"if (r.tag === 'unknown') add('S5_MUTATION_ESCAPE'",to:"if (false) add('S5_MUTATION_ESCAPE'",source:C1},
  {id:"A-1'' container writes only through the allocating binding / N1_conditional_alias_push_join",case:"N1_conditional_alias_push_join",file:'analyze.cjs',from:"add('S5_MUTATION_ESCAPE', node, 'Container writes must target the const binding that allocated it in this function');",to:"void node;",source:C1},
  {id:"A-1'' container writes only through the allocating binding / N2_if_merge_splits_alias_push_join",case:"N2_if_merge_splits_alias_push_join",file:'analyze.cjs',from:"add('S5_MUTATION_ESCAPE', node, 'Container writes must target the const binding that allocated it in this function');",to:"void node;",source:C1},
  {id:"A-1'' container writes only through the allocating binding / N3_or_alias_push_join",case:"N3_or_alias_push_join",file:'analyze.cjs',from:"add('S5_MUTATION_ESCAPE', node, 'Container writes must target the const binding that allocated it in this function');",to:"void node;",source:C1},
  {id:"A-1'' container writes only through the allocating binding / N4_loop_alias_push_join",case:"N4_loop_alias_push_join",file:'analyze.cjs',from:"add('S5_MUTATION_ESCAPE', node, 'Container writes must target the const binding that allocated it in this function');",to:"void node;",source:C1},
  {id:"A-1'' container writes only through the allocating binding / N5_nested_alias_push_join",case:"N5_nested_alias_push_join",file:'analyze.cjs',from:"add('S5_MUTATION_ESCAPE', node, 'Container writes must target the const binding that allocated it in this function');",to:"void node;",source:C1},
  {id:"A-1'' container writes only through the allocating binding / S4_optional_call_alias_push_join",case:"S4_optional_call_alias_push_join",file:'analyze.cjs',from:"add('S5_MUTATION_ESCAPE', node, 'Container writes must target the const binding that allocated it in this function');",to:"void node;",source:C1},
  {id:"A-1'' container writes only through the allocating binding / S5_destructure_default_alias_push_join",case:"S5_destructure_default_alias_push_join",file:'analyze.cjs',from:"add('S5_MUTATION_ESCAPE', node, 'Container writes must target the const binding that allocated it in this function');",to:"void node;",source:C1},
  {id:"A-1'' container writes only through the allocating binding / N6_find_copy_push_object_join",case:"N6_find_copy_push_object_join",file:'analyze.cjs',from:"add('S5_MUTATION_ESCAPE', node, 'Container writes must target the const binding that allocated it in this function');",to:"void node;",source:C3},
  {id:"A-1'' container writes only through the allocating binding / R1_merged_unknown_element_push",case:"R1_merged_unknown_element_push",file:'analyze.cjs',from:"add('S5_MUTATION_ESCAPE', node, 'Container writes must target the const binding that allocated it in this function');",to:"void node;",source:C1},
  {id:"A-1'' container writes only through the allocating binding / R2_let_reassigned_alias_push",case:"R2_let_reassigned_alias_push",file:'analyze.cjs',from:"add('S5_MUTATION_ESCAPE', node, 'Container writes must target the const binding that allocated it in this function');",to:"void node;",source:C1},
  {id:"A-1'' container writes only through the allocating binding / R3_nested_merged_element_push",case:"R3_nested_merged_element_push",file:'analyze.cjs',from:"add('S5_MUTATION_ESCAPE', node, 'Container writes must target the const binding that allocated it in this function');",to:"void node;",source:C1},
  {id:"A-1'' container writes only through the allocating binding / R4_object_alias_unknown_field_push",case:"R4_object_alias_unknown_field_push",file:'analyze.cjs',from:"add('S5_MUTATION_ESCAPE', node, 'Container writes must target the const binding that allocated it in this function');",to:"void node;",source:C1},
  {id:"A-1'' container writes only through the allocating binding / index_write_through_alias",case:"index_write_through_alias",file:'analyze.cjs',from:"add('S5_MUTATION_ESCAPE', node, 'Container writes must target the const binding that allocated it in this function');",to:"void node;",source:C1},
  {id:"A-1'' unknown labels never fit / R5_unknown_field_label_push",case:'R5_unknown_field_label_push',file:'analyze.cjs',from:"const exact = (label, v, n) => known(label) && known(v) && fits(label, v, n);",to:"const exact = (label, v, n) => fits(label, v, n);",source:C1},
  {id:"C array writes stay in bounds / index_write_out_of_bounds",case:'index_write_out_of_bounds',file:'analyze.cjs',from:"if (!index || !ts.isNumericLiteral(index) || !Number.isInteger(+index.text) || +index.text >= owner.length) add('S5_MUTATION_ESCAPE'",to:"if (false) add('S5_MUTATION_ESCAPE'",source:C1},
  {id:"C array writes stay in bounds / array_length_write",case:'array_length_write',file:'analyze.cjs',from:"if (!index || !ts.isNumericLiteral(index) || !Number.isInteger(+index.text) || +index.text >= owner.length) add('S5_MUTATION_ESCAPE'",to:"if (false) add('S5_MUTATION_ESCAPE'",source:C1},
];
for(const record of records)test('isolated static sensitivity / '+record.id,()=>{
  const spec=[...contract,...negative].find(c=>c.id===record.case);
  assert.ok(spec,'registered counterexample');
  const original=require('../ts/'+record.file);
  const mutant=loadMutant(record.file,record.from,record.to);
  const run=(implementation,context)=>record.file==='analyze.cjs'?implementation.analyze(context,manifest.files.find(p=>p.path===record.source)):implementation.checkContracts(context,manifest);
  // A record names the finding detail when another, independent rule also
  // reports the same code for the case (defence in depth); the target rule's
  // own finding must still disappear when only that rule is disabled.
  const hit=findings=>findings.some(f=>f.code===spec.code&&(!record.detail||f.detail===record.detail));
  const originalBaseline=run(original,baseContext);
  assert.ok(!hit(originalBaseline),'positive control fails the target predicate');
  const context=ctx(spec);
  assert.ok(hit(run(original,context)),'actual predicate must reject the mutation with '+spec.code);
  assert.ok(!hit(run(mutant,context)),'disabling this predicate must lose '+spec.code+'; the corresponding production test would turn red');
});
