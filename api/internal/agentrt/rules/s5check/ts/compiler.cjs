'use strict';

// The host sees only the supplied tree and this installation's pinned TS libs.
// In particular it never opens a candidate node_modules, package script or plugin.
const ts = require('typescript');
const crypto = require('node:crypto');
const path = require('node:path');
const fs = require('node:fs');
if (ts.version !== '5.9.3') throw new Error('S5_TYPE: TypeScript 5.9.3 is required');
const VROOT = '/s5-tree/';
const LIB = path.dirname(require.resolve('typescript'));
const DEPS = path.dirname(path.dirname(LIB));
const VDEPS = VROOT+'console/node_modules/';
const trustedReadCache=new Map();
function canonical(value) {
  if (Array.isArray(value)) return '['+value.map(item=>canonical(item===undefined?null:item)).join(',')+']';
  if (value && typeof value === 'object') return '{'+Object.keys(value).sort().filter(k=>value[k]!==undefined).map(k=>JSON.stringify(k)+':'+canonical(value[k])).join(',')+'}';
  if (typeof value === 'number' && !Number.isFinite(value)) throw new Error('non-finite canonical number');
  const encoded=JSON.stringify(value);
  if(encoded===undefined)throw new Error('unsupported canonical value');
  return encoded;
}
const sha256 = value => crypto.createHash('sha256').update(typeof value === 'string' ? value : canonical(value)).digest('hex');
function walk(node, visit) { visit(node); ts.forEachChild(node, child => walk(child, visit)); }
function relative(file) { return file.startsWith(VDEPS) ? 'dependencies/'+file.slice(VDEPS.length) : file.startsWith(VROOT) ? file.slice(VROOT.length) : file.startsWith(LIB + path.sep) ? 'typescript/' + path.basename(file) : file; }
function enclosing(node) {
  let result;
  for (let n = node; n && !ts.isSourceFile(n); n = n.parent) {
    if (ts.isFunctionDeclaration(n) || ts.isArrowFunction(n) || ts.isFunctionExpression(n)) result = n;
  }
  return result || node.getSourceFile();
}
function declarationName(node) {
  if (node.name) return node.name.getText();
  if (node.parent && ts.isVariableDeclaration(node.parent)) return node.parent.name.getText();
  if (ts.isSourceFile(node)) return '<module>';
  return '<closure>';
}
function resolvedSymbol(checker, node) {
  let s = checker.getSymbolAtLocation(node);
  const seen = new Set();
  while (s && s.flags & ts.SymbolFlags.Alias) {
    if (seen.has(s)) return undefined;
    seen.add(s);
    const next = checker.getAliasedSymbol(s);
    if (next === s) return undefined;
    s = next;
  }
  return s;
}
function plainAST(node) {
  const out = {kind: ts.SyntaxKind[node.kind]};
  if (ts.isIdentifier(node) || ts.isPrivateIdentifier(node) || ts.isLiteralExpression(node) || ts.isTemplateLiteralToken(node)) out.text = node.text;
  if (node.operatorToken) out.operator = ts.SyntaxKind[node.operatorToken.kind];
  if (node.operator !== undefined) out.operator = ts.SyntaxKind[node.operator];
  if (node.flags & ts.NodeFlags.OptionalChain) out.optionalChain = true;
  if (node.questionToken) out.optional = true;
  const children = [];
  ts.forEachChild(node, c => { children.push(plainAST(c)); });
  if (children.length) out.children = children;
  return out;
}
const symbolCaches=new WeakMap();
function stableSymbolName(symbol){
  if(!/^__@.+@\d+$/.test(symbol.name))return symbol.name;
  const declarations=symbol.declarations||[];
  const sites=declarations.map(d=>({file:relative(d.getSourceFile()?.fileName||'<synthetic>'),name:d.name?plainAST(d.name):null}));
  return symbol.name.replace(/@\d+$/,'')+':'+sha256(sites);
}
function stableType(type,checker,seen=new Set()){
  if(!type)return 'missing';
  if(seen.has(type)){const s=type.aliasSymbol||type.symbol;return {recursive:s?{name:stableSymbolName(s),file:relative(s.declarations?.[0]?.getSourceFile()?.fileName||'<intrinsic>')}:type.flags};}
  const next=new Set(seen);next.add(type);
  const f=type.flags;
  if(f&(ts.TypeFlags.StringLiteral|ts.TypeFlags.NumberLiteral))return {literal:type.value};
  if(f&ts.TypeFlags.BooleanLiteral)return {literal:type.intrinsicName==='true'};
  for(const [flag,name]of [[ts.TypeFlags.String,'string'],[ts.TypeFlags.Number,'number'],[ts.TypeFlags.Boolean,'boolean'],[ts.TypeFlags.Undefined,'undefined'],[ts.TypeFlags.Null,'null'],[ts.TypeFlags.Void,'void'],[ts.TypeFlags.Any,'any'],[ts.TypeFlags.Unknown,'unknown'],[ts.TypeFlags.Never,'never']])if(f&flag)return name;
  if(type.isUnion())return {union:type.types.map(t=>stableType(t,checker,next)).sort((a,b)=>canonical(a)<canonical(b)?-1:canonical(a)>canonical(b)?1:0)};
  if(checker.isTupleType(type))return {tuple:checker.getTypeArguments(type).map(t=>stableType(t,checker,next))};
  if(checker.isArrayType(type))return {array:stableType(checker.getTypeArguments(type)[0],checker,next)};
  const symbol=type.aliasSymbol||type.symbol;
  const named=symbol?{name:stableSymbolName(symbol),file:relative(symbol.declarations?.[0]?.getSourceFile()?.fileName||'<intrinsic>')}:null;
  if(seen.has(type))return {recursive:named};
  if(symbol&&!symbol.name.startsWith('__'))return {named,args:(type.aliasTypeArguments||checker.getTypeArguments(type)||[]).map(t=>stableType(t,checker,next))};
  const signatures=type.getCallSignatures();
  if(signatures.length)return {call:signatures.map(s=>({parameters:s.parameters.map(p=>stableType(checker.getTypeOfSymbolAtLocation(p,p.valueDeclaration||p.declarations?.[0]),checker,next)),return:stableType(checker.getReturnTypeOfSignature(s),checker,next)}))};
  if(f&ts.TypeFlags.Object)return {object:type.getProperties().map(p=>({name:stableSymbolName(p),optional:!!(p.flags&ts.SymbolFlags.Optional),type:stableType(checker.getTypeOfSymbolAtLocation(p,p.valueDeclaration||p.declarations?.[0]),checker,next)})).sort((a,b)=>a.name<b.name?-1:a.name>b.name?1:0)};
  return {kind:f,named};
}
function symbolID(symbol, checker) {
  if (!symbol) return null;
  let cache=symbolCaches.get(checker);if(!cache){cache=new WeakMap();symbolCaches.set(checker,cache);}if(cache.has(symbol))return cache.get(symbol);
  const declarations = symbol.declarations || [];
  if (!declarations.length) return {file: '<intrinsic>', exportOrDecl: symbol.name, signatureHash: sha256(symbol.name)};
  const d = declarations[0];
  // Declarations in the fixed lib may be merged overloads. Include every one.
  const signatures = declarations.map(decl => {
    if (ts.isFunctionDeclaration(decl) || ts.isMethodSignature(decl) || ts.isMethodDeclaration(decl) || ts.isCallSignatureDeclaration(decl) || ts.isConstructSignatureDeclaration(decl)) {
      return {kind:ts.SyntaxKind[decl.kind], parameters:decl.parameters.map(p => ({name:p.name.getText(),type:p.type ? plainAST(p.type) : stableType(checker.getTypeAtLocation(p),checker), optional:!!p.questionToken || !!p.initializer})), return:decl.type ? plainAST(decl.type) : stableType(checker.getReturnTypeOfSignature(checker.getSignatureFromDeclaration(decl)),checker)};
    }
    return {kind:ts.SyntaxKind[decl.kind],name:stableSymbolName(symbol),type:decl.type ? plainAST(decl.type) : decl.parent?stableType(checker.getTypeAtLocation(decl),checker):'<synthetic-declaration>'};
  });
  const result={file:relative(d.getSourceFile()?.fileName||'<synthetic>'),exportOrDecl:stableSymbolName(symbol),signatureHash:sha256(signatures)};cache.set(symbol,result);return result;
}
function ast(node, checker) {
  const out = plainAST(node);
  // Bind identifiers without serializing symbol object graphs or source offsets.
  function bind(n, target) {
    if (ts.isIdentifier(n)) {
      const s = resolvedSymbol(checker,n);
      if (s) target.symbol = symbolID(s,checker);
      const type = checker.getTypeAtLocation(n);
      target.type = stableType(type,checker);
    }
    let i=0;
    ts.forEachChild(n,c => { bind(c,target.children[i++]); });
  }
  if (checker) bind(node,out);
  return out;
}
const astCaches=new WeakMap();
const astHash = (node, checker) => {if(!checker)return sha256(ast(node));let cache=astCaches.get(checker);if(!cache){cache=new WeakMap();astCaches.set(checker,cache);}if(!cache.has(node))cache.set(node,sha256(ast(node,checker)));return cache.get(node);};

function typeShape(type, checker, active = new Set()) {
  if (!type) return {unknown:true};
  if (active.has(type)) return {unknown:'cycle'};
  const f = type.flags;
  if (f & ts.TypeFlags.Any) return {unknown:'any'};
  if (f & ts.TypeFlags.Unknown) return {unknown:'unknown'};
  if (f & ts.TypeFlags.Never) return 'never';
  if (f & ts.TypeFlags.StringLiteral) return {literal:type.value};
  if (f & ts.TypeFlags.NumberLiteral) return {literal:type.value};
  if (f & ts.TypeFlags.BooleanLiteral) return {literal:type.intrinsicName === 'true'};
  if (f & ts.TypeFlags.String) return 'string';
  if (f & ts.TypeFlags.Number) return 'number';
  if (f & ts.TypeFlags.Boolean) return 'boolean';
  if (f & ts.TypeFlags.Null) return 'null';
  if (f & ts.TypeFlags.Undefined) return 'undefined';
  if (f & ts.TypeFlags.Void) return 'void';
  const next = new Set(active); next.add(type);
  if (type.isUnion()) return {union:type.types.map(t=>typeShape(t,checker,next)).sort((a,b)=>canonical(a)<canonical(b)?-1:canonical(a)>canonical(b)?1:0)};
  const name = type.symbol?.name;
  const isLib = type.symbol?.declarations?.every(d=>d.getSourceFile().fileName.startsWith(LIB + path.sep));
  if (isLib && ['Date','Set','RegExp','Error'].includes(name)) return {opaque:name,args:checker.getTypeArguments(type).map(t=>typeShape(t,checker,next))};
  if (checker.isTupleType(type)) return {tuple:checker.getTypeArguments(type).map(t=>typeShape(t,checker,next))};
  if (checker.isArrayType(type)) return {array:typeShape(checker.getTypeArguments(type)[0],checker,next)};
  const calls = type.getCallSignatures();
  if (calls.length) return {call:calls.map(s=>({parameters:s.parameters.map(p=>typeShape(checker.getTypeOfSymbolAtLocation(p,p.valueDeclaration||p.declarations[0]),checker,next)),return:typeShape(checker.getReturnTypeOfSignature(s),checker,next)}))};
  if (f & ts.TypeFlags.Object) {
    const props = type.getProperties().map(s=>({name:stableSymbolName(s),optional:!!(s.flags & ts.SymbolFlags.Optional),shape:typeShape(checker.getTypeOfSymbolAtLocation(s,s.valueDeclaration || s.declarations[0]),checker,next),kind:s.declarations?.map(d=>ts.SyntaxKind[d.kind])||[]})).sort((a,b)=>a.name<b.name?-1:a.name>b.name?1:0);
    const indices=checker.getIndexInfosOfType(type).map(i=>({key:typeShape(i.keyType,checker,next),value:typeShape(i.type,checker,next)}));
    return {object:props,indices};
  }
  return {unknown:checker.typeToString(type)};
}

function createContext(files) {
  const normalized = new Map();
  for (const [name, entry] of Object.entries(files).sort(([a],[b])=>a<b?-1:a>b?1:0)) {
    if (name.startsWith('/') || name.split('/').some(p=>p==='..' || p==='.' || p==='') || name.includes('\\')) throw new Error('S5_BINDING: invalid tree path');
    if(name.split('/').includes('node_modules'))throw new Error('S5_BINDING: candidate dependencies cannot enter the compiler host');
    normalized.set(VROOT+name, typeof entry === 'string' ? entry : entry.content);
  }
  const libFile = file => path.dirname(file) === LIB && /^lib[.].+[.]d[.]ts$/.test(path.basename(file));
  const toolchain=new Map();
  for(const [name,disk]of [['typescript/typescript.js',require.resolve('typescript')],['typescript/package.json',path.join(LIB,'..','package.json')]]){
    if(!trustedReadCache.has(disk)){const content=fs.readFileSync(disk,'utf8');trustedReadCache.set(disk,{content,hash:sha256(content)});}
    toolchain.set(name,trustedReadCache.get(disk).hash);
  }
  const dependencyPath=file=>file.startsWith(VDEPS)&&!file.slice(VDEPS.length).split('/').includes('..')?path.join(DEPS,file.slice(VDEPS.length)):undefined;
  const trustedPath=file=>libFile(file)?file:dependencyPath(file);
  const readFile = file => {
    if(normalized.has(file))return normalized.get(file);
    const disk=trustedPath(file);
    if(!disk||!(/\.(?:[cm]?ts|json)$/.test(disk))||!fs.existsSync(disk)||!fs.statSync(disk).isFile())return undefined;
    if(!trustedReadCache.has(disk)){const content=fs.readFileSync(disk,'utf8');trustedReadCache.set(disk,{content,hash:sha256(content)});}
    const value=trustedReadCache.get(disk);toolchain.set(relative(file),value.hash);return value.content;
  };
  const fileExists = file => normalized.has(file) || !!(trustedPath(file)&&fs.existsSync(trustedPath(file))&&fs.statSync(trustedPath(file)).isFile());
  const configPath=VROOT+'console/tsconfig.json';
  const cfg = ts.readConfigFile(configPath,readFile);
  const dirs=new Set([VROOT.slice(0,-1)]);
  for(const f of normalized.keys()) {let d=path.posix.dirname(f); while(d.startsWith(VROOT.slice(0,-1))){dirs.add(d);const up=path.posix.dirname(d);if(up===d)break;d=up;}}
  const configHost={useCaseSensitiveFileNames:true,readFile,fileExists,readDirectory:()=>Array.from(normalized.keys()).filter(f=>/\.tsx?$/.test(f)), directoryExists:d=>dirs.has(d)};
  // Parse the reviewed config for diagnostics, but don't let it expand the host.
  const parsed=ts.parseJsonConfigFileContent(cfg.config||{},configHost,VROOT+'console',undefined,configPath);
  const options={...parsed.options,noEmit:true,incremental:false,composite:false,types:[],typeRoots:[],baseUrl:VROOT+'console',paths:{'@/*':['src/*'],'@bifrost/ui':['node_modules/@bifrost/ui/dist/index.d.ts'],'@bifrost/ui/styles':['node_modules/@bifrost/ui/dist/styles/bifrost-ui.css'],'react':['node_modules/@types/react'],'react-dom':['node_modules/@types/react-dom'],'radix-ui':['node_modules/radix-ui'],'class-variance-authority':['node_modules/class-variance-authority'],'lucide-react':['node_modules/lucide-react']},lib:['lib.es2022.d.ts','lib.dom.d.ts','lib.dom.iterable.d.ts'],skipLibCheck:true};
  delete options.plugins;
  const directoryExists=d=>dirs.has(d)||d===LIB||!!(dependencyPath(d+'/')&&fs.existsSync(dependencyPath(d+'/'))&&fs.statSync(dependencyPath(d+'/')).isDirectory());
  const getSourceFile=(f,lang)=>{const text=readFile(f);return text===undefined?undefined:ts.createSourceFile(f,text,lang,true);};
  const host={getSourceFile,getDefaultLibFileName:()=>path.join(LIB,'lib.es2022.full.d.ts'),writeFile:()=>{throw new Error('read-only compiler host');},getCurrentDirectory:()=>VROOT,getDirectories:d=>Array.from(dirs).filter(x=>path.posix.dirname(x)===d),directoryExists,fileExists,readFile,getCanonicalFileName:f=>f,useCaseSensitiveFileNames:()=>true,getNewLine:()=> '\n',realpath:f=>f};
  const rootNames=Array.from(normalized.keys()).filter(f=>f.startsWith(VROOT+'console/src/') && /\.tsx?$/.test(f)&&!/(?:^|\/)(__tests__|testdata)(?:\/|$)|\.(?:test|spec)\.tsx?$/.test(f)).sort();
  const program=ts.createProgram(rootNames,options,host), checker=program.getTypeChecker();
  // Complete inference before fingerprints are taken. Lazy contextual inference
  // can otherwise make a first AST query observe a provisional signature.
  const semanticDiagnostics=program.getSemanticDiagnostics();
  const sourceFiles=new Map(program.getSourceFiles().filter(f=>f.fileName.startsWith(VROOT)&&!f.fileName.startsWith(VDEPS)).map(f=>[relative(f.fileName),f]));
  return {ts,files,program,checker,sourceFiles,options,diagnostics:[...(cfg.error?[cfg.error]:[]),...parsed.errors],semanticDiagnostics,relative,toolchain};
}
function findingAt(node, code, detail, symbol) {
  const sf=node?.getSourceFile();
  const p=sf?sf.getLineAndCharacterOfPosition(node.getStart(sf)): {line:0,character:0};
  return {code,path:sf?relative(sf.fileName):'',line:p.line+1,column:p.character+1,...(symbol?{symbol}:{}),detail};
}
module.exports={ts,createContext,canonical,sha256,walk,relative,enclosing,declarationName,resolvedSymbol,symbolID,plainAST,ast,astHash,typeShape,findingAt,VROOT,LIB};
