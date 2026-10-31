'use strict';

const fs=require('node:fs');
const path=require('node:path');
const {ts,canonical,sha256,walk,relative,enclosing,declarationName,resolvedSymbol,symbolID,astHash,plainAST,typeShape,findingAt} = require('./compiler.cjs');
const CANDIDATES = [
  'console/src/lib/patrol/cronSchedule.ts',
  'console/src/lib/research/signalHealthAgeMeters.ts',
  'console/src/lib/research/researchHealthCopy.ts',
  'console/src/lib/market-data/queueHistoryModel.ts',
];
const TESTS = CANDIDATES.map(p=>p.replace(/\/([^/]+)\.ts$/, '/__tests__/$1.test.ts'));
const DTO = 'console/src/api/marketDataQueueHistory.ts';
const PANEL = 'console/src/components/market-data/QueueHistoryPanel.tsx';
const isTest = f => /(?:^|\/)(__tests__|testdata)(?:\/|$)|\.(?:test|spec)\.tsx?$/.test(f);
function trustedSuite(){
  const paths=['testdata/behavior.cjs','testdata/baseline-sha256.json',...TESTS.map(f=>'testdata/baseline/'+f)];
  return paths.map(p=>({path:p,sha256:sha256(fs.readFileSync(path.join(__dirname,'..',p),'utf8'))}));
}
function content(ctx,p){const e=ctx.files[p];return typeof e==='string'?e:e?.content;}
function exportDeclarations(ctx,p) {
  const sf=ctx.sourceFiles.get(p); if(!sf)return [];
  const mod=ctx.checker.getSymbolAtLocation(sf);if(!mod)return [];
  return ctx.checker.getExportsOfModule(mod).map(s=>({id:symbolID(s,ctx.checker),shape:typeShape(s.flags&ts.SymbolFlags.TypeAlias?ctx.checker.getDeclaredTypeOfSymbol(s):ctx.checker.getTypeOfSymbolAtLocation(s,s.valueDeclaration||s.declarations[0]),ctx.checker),symbol:s}));
}
function exportsOf(ctx,p){return exportDeclarations(ctx,p).map(e=>e.id).sort((a,b)=>a.exportOrDecl<b.exportOrDecl?-1:a.exportOrDecl>b.exportOrDecl?1:0);}
function exportShapes(ctx,p){return exportDeclarations(ctx,p).map(e=>({name:e.id.exportOrDecl,shape:e.shape})).sort((a,b)=>a.name<b.name?-1:a.name>b.name?1:0);}
function statementOrFunction(n) {
  const fn=enclosing(n);if(ts.isFunctionDeclaration(fn))return fn;
  let current=n;while(current.parent&&!ts.isSourceFile(current.parent))current=current.parent;
  return current;
}
function nodePath(node,root) {
  const indices=[];
  for(let n=node;n&&n!==root;n=n.parent){const children=[];ts.forEachChild(n.parent,c=>{children.push(c);});indices.push(children.indexOf(n));}
  return indices.reverse();
}
function directParent(node){let p=node.parent;while(p&&(ts.isParenthesizedExpression(p)||ts.isNonNullExpression(p)))p=p.parent;return p;}
function inImport(node){for(let p=node;p&&!ts.isSourceFile(p);p=p.parent)if(ts.isImportDeclaration(p)||ts.isExportDeclaration(p))return p;return null;}
function isDefinitionName(n){const p=n.parent;return !!(p&&p.name===n&&(ts.isFunctionDeclaration(p)||ts.isVariableDeclaration(p)||ts.isTypeAliasDeclaration(p)||ts.isParameter(p)||ts.isPropertySignature(p)));}
function references(ctx) {
  const exports=new Map();
  for(const file of CANDIDATES)for(const e of exportDeclarations(ctx,file))exports.set(e.symbol,{...e.id});
  const possibleNames=new Set(Array.from(exports.values(),e=>e.exportOrDecl));
  for(const [,sf]of ctx.sourceFiles)walk(sf,n=>{
    if(ts.isImportSpecifier(n)||ts.isExportSpecifier(n)){
      const s=resolvedSymbol(ctx.checker,n.name);if(exports.has(s))possibleNames.add(n.name.text);
    }
  });
  const rows=[];const hashCache=new Map();
  const hash=n=>{if(!hashCache.has(n))hashCache.set(n,astHash(n,ctx.checker));return hashCache.get(n);};
  for(const [file,sf] of ctx.sourceFiles) {
    if(isTest(file))continue;
    walk(sf,n=>{
      if(!(ts.isIdentifier(n)&&possibleNames.has(n.text)||ts.isStringLiteral(n)&&ts.isElementAccessExpression(n.parent)&&n.parent.argumentExpression===n&&possibleNames.has(n.text))||isDefinitionName(n))return;
      const symbol=resolvedSymbol(ctx.checker,n), callee=exports.get(symbol);
      if(!callee)return;
      const imp=inImport(n);
      if(imp&&ts.isImportSpecifier(n.parent)&&n.parent.propertyName===n)return;
      const outer=statementOrFunction(n), parent=directParent(n);
      const call=parent&&ts.isCallExpression(parent)&&parent.expression===n?parent:null;
      const isType=imp? !!(ts.isImportDeclaration(imp)&&(imp.importClause?.isTypeOnly || n.parent.isTypeOnly)) : (()=>{for(let a=n.parent;a&&a!==outer;a=a.parent){if(ts.isTypeNode(a))return true;}return false;})();
      let typeRoot=n;
      if(isType&&!imp)for(let a=n.parent;a&&a!==outer&&ts.isTypeNode(a);a=a.parent)typeRoot=a;
      const boundRoot=isType&&!imp?typeRoot:outer;
      const row={file,enclosingSymbol:declarationName(outer),normalizedAstHash:hash(boundRoot),callee,use:isType?'dto-only':'display',inputs:call?call.arguments.map(a=>canonical(typeShape(ctx.checker.getTypeAtLocation(a),ctx.checker))):[],sinks:[isType?'erased-type':imp?'module-binding':'reviewed-display'],argumentAstHashes:call?call.arguments.map(hash):[],defaultArguments:call?Array.from({length:Math.max(call.arguments.length,((symbol?.valueDeclaration||{}).parameters||[]).length)},(_,i)=>i<call.arguments.length?'present-nonundefined':'registered-omission'):[],site:nodePath(n,boundRoot),contextHash:hash(isType&&!imp?typeRoot:parent||n),role:imp?(ts.isExportDeclaration(imp)?'re-export':'import'):isType?'type':'value'};
      rows.push(row);
    });
  }
  return rows.sort((a,b)=>canonical(a)<canonical(b)?-1:canonical(a)>canonical(b)?1:0);
}
function derivedConsumers(ctx,direct){
  const functions=new Map(),owners=new Map();
  for(const [file,sf]of ctx.sourceFiles){if(isTest(file)||CANDIDATES.includes(file))continue;
    for(const s of sf.statements){
      if(ts.isFunctionDeclaration(s)&&s.name){const symbol=resolvedSymbol(ctx.checker,s.name);if(symbol){functions.set(symbol,s);owners.set(s,symbol);}}
      if(ts.isVariableStatement(s))for(const d of s.declarationList.declarations)if(ts.isIdentifier(d.name)&&d.initializer&&ctx.checker.getTypeAtLocation(d).getCallSignatures().length){const symbol=resolvedSymbol(ctx.checker,d.name);if(symbol){functions.set(symbol,s);owners.set(s,symbol);}}
    }
  }
  const index=new Map();
  for(const [file,sf]of ctx.sourceFiles){if(isTest(file))continue;walk(sf,n=>{
    if(!ts.isIdentifier(n)||isDefinitionName(n))return;
    const target=resolvedSymbol(ctx.checker,n);if(!functions.has(target))return;
    const imp=inImport(n);if(imp&&ts.isImportSpecifier(n.parent)&&n.parent.propertyName===n)return;
    const owner=statementOrFunction(n);
    if(!index.has(target))index.set(target,[]);index.get(target).push({file,node:n,owner,role:imp?'import':'value',ownerSymbol:owners.get(owner)});
  });}
  const queue=[];
  for(const [symbol,owner]of functions){const file=relative(owner.getSourceFile().fileName),name=declarationName(owner);if(direct.some(r=>r.file===file&&r.enclosingSymbol===name&&r.role==='value'))queue.push(symbol);}
  const seen=new Set(),rows=[];
  for(let i=0;i<queue.length;i++){const symbol=queue[i];if(seen.has(symbol))continue;seen.add(symbol);for(const use of index.get(symbol)||[]){
    const {file,node:n,owner,role}=use,parent=directParent(n);
    rows.push({file,enclosingSymbol:declarationName(owner),callee:symbolID(symbol,ctx.checker),normalizedAstHash:astHash(owner,ctx.checker),contextHash:astHash(parent||n,ctx.checker),site:nodePath(n,owner),role});
    if(role==='value'&&use.ownerSymbol)queue.push(use.ownerSymbol);
  }}
  return {symbols:[...seen].map(s=>symbolID(s,ctx.checker)).sort((a,b)=>canonical(a)<canonical(b)?-1:1),references:rows.sort((a,b)=>canonical(a)<canonical(b)?-1:canonical(a)>canonical(b)?1:0)};
}
function environmentSites(ctx,p) {
  const sf=ctx.sourceFiles.get(p);if(!sf)return [];
  const result=[];
  walk(sf,n=>{
    if(!ts.isCallExpression(n)&&!ts.isNewExpression(n))return;
    const e=n.expression;let kind;
    if(ts.isNewExpression(n)&&ts.isIdentifier(e)&&e.text==='Date'&&(!n.arguments||!n.arguments.length))kind='clock';
    if(ts.isPropertyAccessExpression(e)&&ts.isIdentifier(e.expression)&&e.expression.text==='Date'){
      if(e.name.text==='now')kind='clock';
      if(e.name.text==='parse')kind='date-parse';
    }
    if(ts.isPropertyAccessExpression(e)&&e.name.text==='toLocaleString')kind='locale-format';
    if(!kind)return;
    const owner=enclosing(n);let parameter;
    for(let a=n.parent;a&&a!==owner;a=a.parent)if(ts.isParameter(a))parameter=a;
    const position=parameter?'parameter-default':ts.isReturnStatement(n.parent)?'return-expression':'call-expression';
    result.push({file:p,enclosingSymbol:declarationName(owner),position,...(parameter?{parameterIndex:owner.parameters.indexOf(parameter)}:{}),target:symbolID(resolvedSymbol(ctx.checker,ts.isPropertyAccessExpression(e)?e.name:e),ctx.checker),normalizedAstHash:astHash(n,ctx.checker),kind,...(kind==='date-parse'?{enclosingAstHash:astHash(owner,ctx.checker)}:{}),...(kind==='clock'?{defaultUse:p===CANDIDATES[0]?'never-in-production':'registered-omission-only'}:{}),site:nodePath(n,owner)});
  });
  return result;
}
function guardNodes(ctx,p){
  const sf=ctx.sourceFiles.get(p);if(!sf)return [];
  const rows=[];
  walk(sf,n=>{
    const owner=enclosing(n), name=declarationName(owner);
    let protect=false;
    if(ts.isIfStatement(n)){
      const cond=n.expression.getText();
      if(p===CANDIDATES[0])protect= ((name==='describeCronSchedule'||name==='iterCronFiresUtc')&&cond==='fields.length !== 5') || (name==='parseCronSet'&&cond.includes('step <= 0')) || (['formatCountdownTo','formatNextRunAt'].includes(name)&&cond.includes('Number.isFinite'));
      if(p===CANDIDATES[1])protect=name==='ageToFillPct'&&cond.includes('ageHours < 0');
      if(p===CANDIDATES[2])protect=cond.includes("'unknown'")||cond==='input.loading'||cond==='input.reachable === false';
      if(p===CANDIDATES[3])protect=name==='linePath'&&(cond==='points.length === 0 || box.t1 <= box.t0'||cond==='v == null');
      if(protect)rows.push({node:n,predicate:n.expression,consequence:n.thenStatement,owner});
    }
    if(p===CANDIDATES[3]&&name==='buildQueueSeries'&&ts.isCallExpression(n)&&ts.isPropertyAccessExpression(n.expression)&&n.expression.name.text==='filter')rows.push({node:n,predicate:n.arguments[0],consequence:n,owner});
  });return rows;
}
function requiredGuards(ctx,p){return guardNodes(ctx,p).map(({node,predicate,consequence,owner},i)=>({id:`C${CANDIDATES.indexOf(p)+1}-G${i+1}`,enclosingSymbol:declarationName(owner),predicateAstHash:astHash(predicate,ctx.checker),consequenceAstHash:astHash(consequence,ctx.checker),site:nodePath(node,owner),kind:ts.SyntaxKind[node.kind]}));}
function selectorInputContracts(ctx,node,owner){
  const sources=[];
  for(let n=node.parent;n&&n!==owner;n=n.parent){
    if(ts.isForOfStatement(n))sources.push({kind:'loop-input',site:nodePath(n,owner),normalizedAstHash:astHash(n.expression,ctx.checker)});
    if(ts.isArrowFunction(n)){
      // Bind the whole registered predicate, not just its value(p) sub-call.
      // The surrounding array call also pins the receiver/input provenance.
      const call=directParent(n);
      sources.push({kind:'closure-input',site:nodePath(n,owner),normalizedAstHash:astHash(n,ctx.checker),callHash:call&&ts.isCallExpression(call)?astHash(call,ctx.checker):null});
    }
  }
  return sources;
}
function selectorReferences(ctx){
  const result=[];
  for(const file of [CANDIDATES[3],PANEL]){
    const sf=ctx.sourceFiles.get(file);if(!sf)continue;
    walk(sf,n=>{
      if(!ts.isIdentifier(n)||n.text!=='value')return;
      const owner=enclosing(n),name=declarationName(owner);
      if(!(file===PANEL?['Plot','QueueHistoryPanel'].includes(name):['linePath','areaPath','isolatedPoints'].includes(name)))return;
      if(isDefinitionName(n)&&ts.isParameter(n.parent))return;
      const symbol=resolvedSymbol(ctx.checker,n);if(file!==PANEL&&symbol?.valueDeclaration?.parent!==owner)return;
      const parent=directParent(n);
      result.push({file,enclosingSymbol:name,symbol:symbolID(symbol,ctx.checker),site:nodePath(n,owner),contextHash:astHash(parent||n,ctx.checker),inputContracts:selectorInputContracts(ctx,n,owner),originIDs:['Plot.pending','Plot.donePerMin'],parameterIndex:1});
    });
    if(file===PANEL)walk(sf,n=>{if(ts.isJsxSelfClosingElement(n)&&n.tagName.getText()==='Plot'){result.push({file,enclosingSymbol:declarationName(enclosing(n)),site:nodePath(n,enclosing(n)),contextHash:astHash(n,ctx.checker),originIDs:['Plot.pending','Plot.donePerMin'],parameterIndex:1});}});
  }
  return result;
}
function importRelations(ctx){
  const rows=[];
  for(const [file,sf]of ctx.sourceFiles){if(isTest(file)||CANDIDATES.includes(file))continue;
    walk(sf,n=>{if((ts.isImportDeclaration(n)||ts.isExportDeclaration(n))&&n.moduleSpecifier&&ts.isStringLiteral(n.moduleSpecifier)){
      const target=resolvedSymbol(ctx.checker,n.moduleSpecifier);
      const targetFile=target?.declarations?.map(d=>relative(d.getSourceFile().fileName)).find(p=>CANDIDATES.includes(p));
      if(targetFile)rows.push({from:file,to:targetFile,kind:n.importClause?.isTypeOnly?'type-only':ts.isExportDeclaration(n)?'re-export':'display-load',normalizedAstHash:astHash(n,ctx.checker)});
    }});
  }return rows.sort((a,b)=>canonical(a)<canonical(b)?-1:1);
}
function moduleEdges(ctx){
  const rows=[];
  for(const [file,sf]of ctx.sourceFiles){if(isTest(file)||CANDIDATES.includes(file))continue;
    walk(sf,n=>{
      let spec,kind;
      if((ts.isImportDeclaration(n)||ts.isExportDeclaration(n))&&n.moduleSpecifier){spec=n.moduleSpecifier;kind=ts.isExportDeclaration(n)?'re-export':n.importClause?.isTypeOnly?'type-import':'import';}
      if(ts.isCallExpression(n)&&(n.expression.kind===ts.SyntaxKind.ImportKeyword||ts.isIdentifier(n.expression)&&n.expression.text==='require')){spec=n.arguments[0];kind='dynamic';}
      if(ts.isImportTypeNode(n)){spec=n.argument;kind='import-type-query';}
      if(!spec)return;
      const literal=ts.isStringLiteral(spec)?spec.text:ts.isLiteralTypeNode(spec)&&ts.isStringLiteral(spec.literal)?spec.literal.text:null;
      const target=resolvedSymbol(ctx.checker,spec);
      rows.push({file,kind,module:literal,targets:(target?.declarations||[]).map(d=>relative(d.getSourceFile().fileName)).sort(),astHash:astHash(n,ctx.checker),...(literal===null?{enclosingHash:astHash(statementOrFunction(n),ctx.checker)}:{})});
    });
  }
  return rows.sort((a,b)=>canonical(a)<canonical(b)?-1:canonical(a)>canonical(b)?1:0);
}
function outsideConsoleSources(ctx){
  const rows=[];
  for(const file of Object.keys(ctx.files).sort()){
    if(file.startsWith('console/src/')||file.startsWith('api/internal/agentrt/rules/s5check/')||isTest(file)||!/\.tsx?$/.test(file))continue;
    const source=content(ctx,file), sf=ts.createSourceFile(file,source,ts.ScriptTarget.ES2022,true);
    const edges=[];
    walk(sf,n=>{
      let spec;
      if((ts.isImportDeclaration(n)||ts.isExportDeclaration(n))&&n.moduleSpecifier)spec=n.moduleSpecifier;
      if(ts.isCallExpression(n)&&(n.expression.kind===ts.SyntaxKind.ImportKeyword||ts.isIdentifier(n.expression)&&n.expression.text==='require'))spec=n.arguments[0];
      if(!spec)return;
      const module=ts.isStringLiteral(spec)?spec.text:null;
      let resolved=null;
      if(module&&(module.startsWith('.')||module.startsWith('@/'))){
        const base=module.startsWith('@/')?'console/src/'+module.slice(2):path.posix.normalize(path.posix.join(path.posix.dirname(file),module));
        resolved=[base,base+'.ts',base+'.tsx',base+'.d.ts',base.replace(/\.js$/,'.ts'),base.replace(/\.js$/,'.tsx'),base+'/index.ts',base+'/index.tsx'].find(p=>ctx.files[p]!==undefined)||null;
      }
      edges.push({module,resolved,kind:ts.SyntaxKind[n.kind],normalizedAstHash:sha256(plainAST(n))});
    });
    rows.push({path:file,sourceSHA:sha256(source),edges});
  }
  return rows;
}
function dataSourceContracts(ctx,refs,edges){
  const callerFiles=new Set(refs.filter(r=>r.role!=='import').map(r=>r.file).filter(f=>!CANDIDATES.includes(f)));
  const graph=new Map();for(const e of edges){if(!graph.has(e.file))graph.set(e.file,new Set());for(const t of e.targets)if(ctx.sourceFiles.has(t))graph.get(e.file).add(t);}
  const seen=new Set(),queue=[...callerFiles];
  for(let i=0;i<queue.length;i++){const f=queue[i];if(seen.has(f)||CANDIDATES.includes(f))continue;seen.add(f);for(const target of graph.get(f)||[])queue.push(target);}
  const sources=[...seen].filter(f=>!callerFiles.has(f)).sort().map(file=>({path:file,sourceSHA:sha256(content(ctx,file)),kind:'frozen-local-producer-module',allowedPrototype:'ordinary DTO or fixed builtin; no injected getter, Proxy, iterator or method replacement',transferEdges:edges.filter(e=>e.targets.includes(file)).map(e=>({from:e.file,module:e.module,normalizedAstHash:e.astHash}))}));
  const witnesses=new Map();
  for(const file of callerFiles){
    const sf=ctx.sourceFiles.get(file),names=new Set(refs.filter(r=>r.file===file).map(r=>r.enclosingSymbol));
    const owners=sf.statements.filter(s=>ts.isFunctionDeclaration(s)&&names.has(declarationName(s))),visited=new Set();
    for(let index=0;index<owners.length;index++){
      const owner=owners[index];if(visited.has(owner))continue;visited.add(owner);
      walk(owner,n=>{if(!ts.isIdentifier(n))return;const symbol=resolvedSymbol(ctx.checker,n);for(const decl of symbol?.declarations||[]){
        if(relative(decl.getSourceFile()?.fileName||'')!==file)continue;
        const root=statementOrFunction(decl);if(root===owner||ts.isImportDeclaration(root)||ts.isTypeAliasDeclaration(root))continue;
        const id=symbolID(symbol,ctx.checker),key=canonical(id);
        witnesses.set(key,{file,symbol:id,normalizedAstHash:astHash(root,ctx.checker),kind:'frozen-local-constructor-or-constant',consumers:[`${file}#${declarationName(owner)}`]});
        if(!visited.has(root))owners.push(root);
      }});
    }
  }
  return {sources,witnesses:[...witnesses.values()].sort((a,b)=>canonical(a)<canonical(b)?-1:1)};
}
function typeImports(ctx){
  const sf=ctx.sourceFiles.get(DTO);if(!sf)return [];
  const d=sf.statements.find(n=>ts.isTypeAliasDeclaration(n)&&n.name.text==='QueueHistoryPoint');if(!d)return [];
  return [{module:'@/api/marketDataQueueHistory',name:'QueueHistoryPoint',shapeHash:sha256(typeShape(ctx.checker.getTypeAtLocation(d),ctx.checker)),sourceSHA:sha256(content(ctx,DTO)),file:DTO,declarationHash:astHash(d,ctx.checker)}];
}
// Exactly what analyze.cjs executes per builtin call: resolved standard-library
// identity and form. Argument contracts are the §3.4 rules coded in analyze.cjs;
// local closures are checked by instantiation, not by an enrolled row.
function calls(ctx,p){
  const sf=ctx.sourceFiles.get(p);const result=new Map();if(!sf)return [];
  walk(sf,n=>{
    if(!ts.isCallExpression(n)&&!ts.isNewExpression(n))return;
    const e=n.expression, member=ts.isPropertyAccessExpression(e), symbol=resolvedSymbol(ctx.checker,member?e.name:e);
    if(!symbol?.declarations?.length||!symbol.declarations.every(d=>ctx.program.isSourceFileDefaultLibrary(d.getSourceFile())))return;
    const entry={target:symbolID(symbol,ctx.checker),form:ts.isNewExpression(n)?'construct':member?'method':'call'};
    result.set(canonical(entry),entry);
  });return Array.from(result.values()).sort((a,b)=>canonical(a)<canonical(b)?-1:1);
}
function assets(ctx){const sf=ctx.sourceFiles.get('console/src/main.tsx');if(!sf)return [];return sf.statements.filter(n=>ts.isImportDeclaration(n)&&n.moduleSpecifier.text.endsWith('.css')).map(n=>({file:'console/src/main.tsx',module:n.moduleSpecifier.text,normalizedAstHash:astHash(n,ctx.checker)}));}
function enroll(ctx){
  const refs=references(ctx),selectors=selectorReferences(ctx),types=typeImports(ctx);
  const derived=derivedConsumers(ctx,refs);
  const moduleInventory=moduleEdges(ctx),dataContracts=dataSourceContracts(ctx,[...refs,...derived.references],moduleInventory);
  const files=CANDIDATES.map((p,i)=>({path:p,language:'ts',profileVersion:1,sourceSHA:sha256(content(ctx,p)),exports:exportsOf(ctx,p),exportShapes:exportShapes(ctx,p),typeImports:i===3?types:[],valueImports:[],allowedCalls:calls(ctx,p),environmentSites:environmentSites(ctx,p),requiredGuards:requiredGuards(ctx,p),callers:refs.filter(r=>r.callee.file===p),trustedTests:[TESTS[i]],selectorReferences:i===3?selectors:[],regularExpressions:[] }));
  for(const policy of files){walk(ctx.sourceFiles.get(policy.path),n=>{if(ts.isRegularExpressionLiteral(n)&&!policy.regularExpressions.includes(n.text))policy.regularExpressions.push(n.text);});}
  return {schema:2,profileVersion:1,typescript:'5.9.3',reviewedCommit:'12a1054106a56b85fce957179116b76702e3a387',enrolledAtCommit:'3053dd25362392022d5f5c5d1386fd469d6b0f51',toolchain:Object.fromEntries([...ctx.toolchain].sort(([a],[b])=>a<b?-1:a>b?1:0)),outsideConsoleSources:outsideConsoleSources(ctx),derivedConsumers:derived,dataContracts,files,relations:{edges:importRelations(ctx),moduleEdges:moduleInventory},assets:assets(ctx),configs:[{path:'console/tsconfig.json',sha256:sha256(content(ctx,'console/tsconfig.json'))}],trustedTests:TESTS.map(p=>({path:p,sha256:sha256(content(ctx,p))})),trustedSuite:trustedSuite(),dataSources:[{path:DTO,sourceSHA:sha256(content(ctx,DTO))}]};
}
function equal(a,b){return canonical(a)===canonical(b);}
function checkContracts(ctx,manifest,{baseline=false}={}){
  const out=[];
  const add=(file,code,detail,node)=>out.push(node?findingAt(node,code,detail):{path:file,code,line:1,column:1,detail});
  if(manifest.schema!==2||manifest.profileVersion!==1||manifest.typescript!==ts.version||!equal(manifest.files?.map(p=>p.path),CANDIDATES)){add('','S5_BINDING','Unknown policy schema, compiler version or enrollment set');return out;}
  if(!equal(Object.fromEntries(ctx.toolchain),manifest.toolchain))add('','S5_TYPE','Pinned compiler library or dependency material changed');
  for(const c of manifest.configs||[])if(content(ctx,c.path)===undefined||sha256(content(ctx,c.path))!==c.sha256)add(c.path,'S5_TYPE','Reviewed compiler configuration changed or missing');
  for(const test of manifest.trustedTests||[])if(content(ctx,test.path)===undefined||sha256(content(ctx,test.path))!==test.sha256)add(test.path,'S5_BEHAVIOR','Protected behavior test is missing or modified');
  if(!equal(trustedSuite(),manifest.trustedSuite))add('','S5_BEHAVIOR','Installed trusted behavior suite differs from policy');
  for(const source of manifest.dataSources||[])if(content(ctx,source.path)===undefined||sha256(content(ctx,source.path))!==source.sourceSHA){add(source.path,'S5_DATA_PROVENANCE','Reviewed data construction source changed');}
  for(const source of manifest.dataContracts?.sources||[])if(content(ctx,source.path)===undefined||sha256(content(ctx,source.path))!==source.sourceSHA)add(source.path,'S5_DATA_PROVENANCE','Frozen upstream DTO or data producer module changed');
  for(const witness of manifest.dataContracts?.witnesses||[]){
    const sf=ctx.sourceFiles.get(witness.file);let found=false;
    if(sf)walk(sf,n=>{if(found||!ts.isIdentifier(n)||n.text!==witness.symbol.exportOrDecl)return;const s=resolvedSymbol(ctx.checker,n);if(!s||!equal(symbolID(s,ctx.checker),witness.symbol))return;for(const d of s.declarations||[])if(astHash(statementOrFunction(d),ctx.checker)===witness.normalizedAstHash)found=true;});
    if(!found)add(witness.file,'S5_DATA_PROVENANCE','Frozen local input constructor changed: '+witness.symbol.exportOrDecl);
  }
  const outside=outsideConsoleSources(ctx);
  if(!equal(outside,manifest.outsideConsoleSources)){
    add('','S5_CALLER_CHANGED','An unprofiled TypeScript package changed; its complete source seal requires core review');
    add('','S5_IMPORT_UNKNOWN','Outside-Console imports and consumers are closed in this profile');
    if(baseline)add('','S5_BASELINE_UNREVIEWED','Unreviewed outside-Console baseline');
  }
  const allrefs=references(ctx),relations=importRelations(ctx),selectorRefs=selectorReferences(ctx);
  const derived=derivedConsumers(ctx,allrefs);
  if(!equal(derived,manifest.derivedConsumers)){
    add('','S5_CALLER_CHANGED','Derived consumer symbol/reference closure changed');
    add('','S5_TRUST_USE','Output propagation beyond the reviewed display consumers requires core review');
  }
  if(!equal(relations,manifest.relations.edges))add('','S5_CALLER_CHANGED','Runtime/type/re-export relationship inventory changed');
  const moduleInventory=moduleEdges(ctx);
  // Every recorded data-contract member (transfer edges, witnesses and their
  // consumers) is re-derived, not only the source hashes checked above.
  if(!equal(dataSourceContracts(ctx,[...allrefs,...derived.references],moduleInventory),manifest.dataContracts))add('','S5_DATA_PROVENANCE','Data source, transfer edge or construction witness inventory changed');
  for(const edge of moduleInventory)if(!edge.targets.length)add(edge.file,'S5_IMPORT_UNKNOWN','Module resolution failed: '+edge.module);
  if(!equal(moduleInventory,manifest.relations.moduleEdges))add('','S5_IMPORT_UNKNOWN','Complete module-edge inventory changed; unresolved or new dynamic relationships need core review');
  if(!equal(assets(ctx),manifest.assets))add('console/src/main.tsx','S5_IMPORT_UNKNOWN','Unregistered asset import or changed CSS entry');
  for(const policy of manifest.files){
    const file=policy.path,sf=ctx.sourceFiles.get(file);
    if(!sf){add(file,'S5_CHANGE_KIND','Enrolled source is missing');continue;}
    if(baseline&&sha256(content(ctx,file))!==policy.sourceSHA)add(file,'S5_BASELINE_UNREVIEWED','M0 complete source differs from the reviewed baseline');
    if(!equal(exportsOf(ctx,file),policy.exports))add(file,'S5_EXPORT_CONTRACT','Export identity or signature changed');
    if(!equal(exportShapes(ctx,file),policy.exportShapes))add(file,'S5_TYPE_CONTRACT','Exported data shape or function signature changed');
    // First batch: no value imports, one fixed original test per file.
    if(!equal(policy.valueImports,[])||!equal(policy.trustedTests,[TESTS[CANDIDATES.indexOf(file)]]))add(file,'S5_BINDING','Profile enrolls value imports or a different trusted test');
    for(const entry of calls(ctx,file))if(!policy.allowedCalls.some(rule=>equal(rule,entry)))add(file,'S5_CALL_UNKNOWN',`Builtin ${entry.form} ${entry.target.exportOrDecl} is outside this exact FilePolicy`);
    if(file===CANDIDATES[3]&&!equal(typeImports(ctx),policy.typeImports))add(DTO,'S5_TYPE_CONTRACT','QueueHistoryPoint declaration, shape or source changed');
    const actualCallers=allrefs.filter(r=>r.callee.file===file);
    // Consumers of a site's enclosing function are the exact `callers` contract below.
    const actualSites=environmentSites(ctx,file);
    for(const kind of ['clock','locale-format','date-parse']){
      if(!equal(actualSites.filter(s=>s.kind===kind),policy.environmentSites.filter(s=>s.kind===kind)))add(file,kind==='clock'?'S5_CLOCK_SITE':kind==='date-parse'?'S5_DATE_PARSE_SITE':'S5_ENVIRONMENT_SITE',`Frozen ${kind} site or its enclosing contract changed`);
    }
    // Match by symbol and structural route, then hash the actual AST. The route
    // prevents a copied guard in a dead branch from satisfying the contract.
    for(const guard of policy.requiredGuards){
      const owner=sf.statements.find(n=>ts.isFunctionDeclaration(n)&&n.name?.text===guard.enclosingSymbol);
      let n=owner;
      for(const index of guard.site){if(!n)break;const children=[];ts.forEachChild(n,c=>{children.push(c);});n=children[index];}
      const pred=n&&ts.isIfStatement(n)?n.expression:n&&ts.isCallExpression(n)?n.arguments[0]:null;
      const consequence=n&&ts.isIfStatement(n)?n.thenStatement:n;
      if(!pred||!consequence||ts.SyntaxKind[n.kind]!==guard.kind||astHash(pred,ctx.checker)!==guard.predicateAstHash||astHash(consequence,ctx.checker)!==guard.consequenceAstHash)add(file,'S5_REQUIRED_GUARD',`Required guard ${guard.id} is absent, moved or changed`,n||owner||sf);
    }
    const expected=policy.callers,actual=actualCallers;
    if(!equal(actual,expected)){
      const extras=actual.filter(r=>!expected.some(e=>equal(e,r)));
      const changed=extras[0];add(changed?.file||file,'S5_CALLER_CHANGED',`Complete reference/default-argument/consumer contract changed for ${file}`);
      if(extras.some(r=>r.file!==file&&r.role==='value'))add(changed?.file||file,'S5_DATA_PROVENANCE','New or changed consumer lacks a reviewed data source and sink contract');
    }
    if(file===CANDIDATES[3]&&!equal(selectorRefs,policy.selectorReferences))add(file,'S5_SELECTOR_UNKNOWN','Selector origin, forwarding or invocation edge is not registered');
  }
  return out;
}
// toolchainDigest is not derived here: it is SHA-256 of the canonical image
// TOOLS.lock.json, bound by the Go wrapper (s5check.ToolchainDigest). Compiler
// version, config and every declaration read stay bound through policyDigest
// (manifest.typescript/configs/toolchain) and are re-measured by checkContracts.
function contractDigests(manifest){return {policyDigest:sha256(manifest),callersDigest:sha256({callers:manifest.files.map(f=>f.callers),selectors:manifest.files.map(f=>f.selectorReferences),relations:manifest.relations,dataSources:manifest.dataSources,assets:manifest.assets,outsideConsoleSources:manifest.outsideConsoleSources,dataContracts:manifest.dataContracts,derivedConsumers:manifest.derivedConsumers}),typesDigest:sha256(manifest.files.map(f=>({exports:f.exports,exportShapes:f.exportShapes,typeImports:f.typeImports}))),testsDigest:sha256({originalTests:manifest.trustedTests,suite:manifest.trustedSuite})};}
module.exports={CANDIDATES,TESTS,DTO,PANEL,enroll,checkContracts,contractDigests,references,selectorReferences,environmentSites,moduleEdges,nodePath};
