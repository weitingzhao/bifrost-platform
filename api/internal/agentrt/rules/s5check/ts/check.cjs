'use strict';

let compiler;
try { compiler = require('./compiler.cjs'); }
catch (error) {
  if (require.main === module) {
    process.stderr.write(`S5_TYPE: Fixed compiler installation is unavailable: ${error.message}\n`);
    process.exit(2);
  }
  throw error;
}
const {createContext,canonical,sha256,ts} = compiler;
const {CANDIDATES,checkContracts,contractDigests} = require('./contracts.cjs');
const {analyze} = require('./analyze.cjs');

// Bounded, content-addressed baseline cache only. No candidate controls executable
// configuration, dependencies, policies or a rule-disable switch.
let baselineCache;
function compare(a,b) {
  for (const key of ['path','line','column','code','detail']) {
    if (a[key]<b[key]) return -1; if(a[key]>b[key]) return 1;
  }
  return 0;
}
const SCANNED=/\.(?:tsx?|json)$/;
function finding(code,path,detail){return {code,path,line:1,column:1,detail};}
function fileContent(e){return typeof e === 'string' ? e : e?.content;}
function validateFiles(files) {
  if (!files || typeof files !== 'object' || Array.isArray(files)) throw Error('Missing immutable file map');
  for (const [p,e] of Object.entries(files)) {
    if (p.startsWith('/') || p.includes('\\') || p.split('/').some(x=>!x || x==='.' || x==='..') || typeof fileContent(e)!=='string') throw Error('Invalid tree entry');
  }
}
function scan(ctx,policies,baseline) {
  const out=[];
  // TypeScript recovers incomplete declarations into nodes with missing bodies.
  // Reject parse errors before contracts or source labels consume that recovery
  // tree, including errors in a caller or DTO rather than only a candidate.
  for(const [path,sf] of ctx.sourceFiles) {
    for(const diagnostic of ctx.program.getSyntacticDiagnostics(sf)) {
      const loc=sf.getLineAndCharacterOfPosition(diagnostic.start||0);
      out.push({code:'S5_PARSE',path,line:loc.line+1,column:loc.character+1,detail:`TS${diagnostic.code}: ${ts.flattenDiagnosticMessageText(diagnostic.messageText,'\n')}`});
    }
  }
  if(out.length)return out;
  out.push(...checkContracts(ctx,policies,{baseline}));
  for(const diagnostic of ctx.diagnostics) out.push(finding('S5_TYPE','console/tsconfig.json',ts.flattenDiagnosticMessageText(diagnostic.messageText,'\n')));
  for(const policy of policies.files) {
    const sf=ctx.sourceFiles.get(policy.path); if(!sf)continue;
    for(const diagnostic of ctx.program.getSemanticDiagnostics(sf)) {
      const loc=sf.getLineAndCharacterOfPosition(diagnostic.start||0);
      out.push({code:'S5_TYPE',path:policy.path,line:loc.line+1,column:loc.character+1,detail:`TS${diagnostic.code}: ${ts.flattenDiagnosticMessageText(diagnostic.messageText,'\n')}`});
    }
    out.push(...analyze(ctx,policy));
  }
  return out;
}
function check(request) {
  let digests={};const findings=[];
  try {
    const policies=request?.policies;
    if(request?.schema!==2 || !policies || !Array.isArray(policies.files)) throw Error('Unsupported request/policy schema');
    digests=contractDigests(policies);
    validateFiles(request.baseline?.files);validateFiles(request.result?.files);
    const {baseline,result}=request;
    // Go selects the requested paths and routes Go profiles before this checker
    // runs (CheckS5Candidate); the request carries only the two trees.
    for(const tree of [baseline,result]) {
      // A symlink, gitlink, executable or binary entry would be read as source
      // text it is not. Every scanned TS/TSX/JSON input must be a regular blob.
      for(const [path,e] of Object.entries(tree.files)) {
        if(SCANNED.test(path) && (typeof e!=='object' || e.mode!=='100644' || e.binary)) findings.push(finding('S5_CHANGE_KIND',path,'Scanned TS/TSX/JSON inputs must be regular, nonbinary 100644 blobs'));
      }
    }
    for(const path of CANDIDATES) {
      for(const tree of [baseline,result]) {
        const e=tree.files[path];
        if(!e || e.mode!=='100644' || fileContent(e)?.includes('\0'))findings.push(finding('S5_CHANGE_KIND',path,'Enrolled files must exist as regular, nonbinary 100644 blobs on both sides'));
      }
    }
    const cacheKey=sha256({files:baseline.files,policies});
    if(!baselineCache || baselineCache.key!==cacheKey) {
      const context=createContext(baseline.files);
      baselineCache={key:cacheKey,findings:scan(context,policies,true)};
    }
    findings.push(...baselineCache.findings);
    findings.push(...scan(createContext(result.files),policies,false));
  } catch(error) {
    findings.push(finding('S5_BINDING','',`Static checker could not complete: ${error.message}`));
  }
  const unique=new Map();for(const f of findings)unique.set(canonical(f),f);
  return {...digests,findings:[...unique.values()].sort(compare)};
}
if(require.main===module) {
  const fs=require('node:fs');
  try { const request=JSON.parse(fs.readFileSync(0,'utf8'));process.stdout.write(canonical(check(request))+'\n'); }
  catch(error){process.stderr.write(`S5_PARSE: ${error.message}\n`);process.exitCode=2;}
}
module.exports={check};
