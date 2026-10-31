'use strict';

// This interpreter computes finite source labels. It never evaluates candidate JS.
const { ts, resolvedSymbol, symbolID, canonical } = require('./compiler.cjs');
const PROTOTYPE = new Set(['constructor', 'prototype', '__proto__']);
const REGEX = new Set(['/^\\d+$/', '/^(\\d)-(\\d)$/', '/\\s+/', '/-?\\d+(?:\\.\\d+)?/g']);
const STRUCTURAL = new Set((`
SourceFile ImportDeclaration ImportClause NamedImports ImportSpecifier FunctionDeclaration
ArrowFunction Parameter VariableStatement VariableDeclarationList VariableDeclaration TypeAliasDeclaration
Block ReturnStatement IfStatement SwitchStatement CaseBlock CaseClause DefaultClause ForStatement
ForOfStatement WhileStatement BreakStatement ContinueStatement ThrowStatement TryStatement CatchClause
ExpressionStatement Identifier StringLiteral NumericLiteral RegularExpressionLiteral TrueKeyword FalseKeyword
NullKeyword ArrayLiteralExpression ObjectLiteralExpression PropertyAssignment ShorthandPropertyAssignment
BindingElement ObjectBindingPattern ArrayBindingPattern TemplateExpression TemplateSpan NoSubstitutionTemplateLiteral
TemplateHead TemplateMiddle TemplateTail ParenthesizedExpression PropertyAccessExpression ElementAccessExpression
ConditionalExpression BinaryExpression PrefixUnaryExpression PostfixUnaryExpression CallExpression NewExpression
AsExpression NonNullExpression TypeReference TypeLiteral PropertySignature ArrayType TupleType UnionType LiteralType
ParenthesizedType IndexedAccessType TypeOperator FunctionType StringKeyword NumberKeyword BooleanKeyword
UndefinedKeyword VoidKeyword ExportKeyword ReadonlyKeyword EndOfFileToken
`).trim().split(/\s+/).map(k => ts.SyntaxKind[k]));
const OPERATOR_TOKENS = new Set(['PlusToken','MinusToken','AsteriskToken','SlashToken','PercentToken','AsteriskAsteriskToken',
  'LessThanToken','LessThanEqualsToken','GreaterThanToken','GreaterThanEqualsToken','EqualsEqualsEqualsToken',
  'ExclamationEqualsEqualsToken','EqualsEqualsToken','ExclamationEqualsToken','AmpersandAmpersandToken','BarBarToken',
  'QuestionQuestionToken','ExclamationToken','EqualsToken','PlusEqualsToken','MinusEqualsToken','PlusPlusToken','MinusMinusToken',
  'QuestionToken','QuestionDotToken','ColonToken','EqualsGreaterThanToken'].map(k => ts.SyntaxKind[k]));

function analyze(ctx, policy) {
  const { checker } = ctx;
  const sf = ctx.sourceFiles.get(policy.path);
  if (!sf) return [];
  const findings = [], emitted = new Set(), env = new Map(), locals = new Map(), stack = [], moduleSymbols = new Set();
  let activeFunction;
  const type = n => { try { return checker.getTypeAtLocation(n); } catch { return undefined; } };
  const sym = n => { try { return checker.getSymbolAtLocation(n); } catch { return undefined; } };
  const add = (code, n, detail) => {
    const at = n ? n.getStart(sf) : 0, key = `${code}:${at}:${detail}`;
    if (emitted.has(key)) return;
    emitted.add(key);
    const loc = sf.getLineAndCharacterOfPosition(at);
    findings.push({ code, path: policy.path, line: loc.line + 1, column: loc.character + 1,
      ...(activeFunction?.name ? { symbol: activeFunction.name.getText(sf) } : {}), detail });
  };
  const unknown = (n, why = 'Unproved value source') => ({ tag: 'unknown', type: type(n), why });
  const scalar = n => ({ tag: 'scalar', type: type(n) });
  const isScalarType = t => !!t && (t.isUnion() ? t.types.every(isScalarType) :
    !!(t.flags & (ts.TypeFlags.StringLike | ts.TypeFlags.NumberLike | ts.TypeFlags.BooleanLike | ts.TypeFlags.Null | ts.TypeFlags.Undefined | ts.TypeFlags.Never)));
  const isType = (t, flag) => !!t && (t.isUnion() ? t.types.every(x => isType(x, flag)) : !!(t.flags & flag));
  const nullable = t => !!t && (t.isUnion() ? t.types.some(nullable) : !!(t.flags & (ts.TypeFlags.Null | ts.TypeFlags.Undefined)));
  const nonnull = t => t && checker.getNonNullableType(t);
  const symbolName = t => t?.getSymbol()?.getName();
  const builtinType = (t, name) => symbolName(nonnull(t)) === name && (nonnull(t)?.getSymbol()?.declarations || []).every(d => ctx.program.isSourceFileDefaultLibrary(d.getSourceFile()));
  const merge = (a, b) => {
    if (!a) return b; if (!b) return a;
    if (a.tag === 'scalar' && b.tag === 'scalar') return { ...a, module: !!a.module || !!b.module, fresh: !!a.fresh && !!b.fresh };
    if (a.tag !== b.tag) {
      if (a.tag === 'scalar' && isType(a.type, ts.TypeFlags.Null | ts.TypeFlags.Undefined | ts.TypeFlags.Never)) return { ...b, nullable: true };
      if (b.tag === 'scalar' && isType(b.type, ts.TypeFlags.Null | ts.TypeFlags.Undefined | ts.TypeFlags.Never)) return { ...a, nullable: true };
      return { tag: 'unknown', why: 'Incompatible source labels at control-flow merge' };
    }
    if (['closure','selector','builtin'].includes(a.tag) && a.id !== b.id) return { tag: 'unknown', why: 'Different callable origins' };
    const out = { ...a, fresh: !!a.fresh && !!b.fresh, module: !!a.module || !!b.module, nullable: a.nullable || b.nullable };
    if (a.element && b.element) out.element = merge(a.element, b.element);
    if (a.fields && b.fields) {
      out.fields = new Map();
      for (const [key, value] of a.fields) out.fields.set(key, b.fields.has(key) ? merge(value, b.fields.get(key)) : unknown(sf));
      for (const key of b.fields.keys()) if (!out.fields.has(key)) out.fields.set(key, unknown(sf));
    }
    return out;
  };
  // v fits label when writing it cannot widen label (no in-place label change).
  const fits = (label, v, n) => !!label && !!v && labelKey(merge(label, v)) === labelKey(label) &&
    !!merge(label, v).nullable === !!label.nullable && (label.tag !== 'scalar' || isScalarType(type(n) || v.type));
  // Container writes (push, Set.add, element and field writes) are accepted only
  // through the const binding that allocated the container with a literal (or
  // new Set) in the function being analysed, and only with a value that exactly
  // fits that allocation label, which must be free of unknown parts. No other
  // expression can write a container, so every runtime container's content
  // always fits its allocation label, and every alias label (a copy or merge of
  // it) stays sound however it was derived.
  const allocations = new Map();
  const known = v => !!v && !['unknown','closure','selector','builtin','regex','error'].includes(v.tag) &&
    (!v.element || known(v.element)) && (!v.fields || [...v.fields.values()].every(known));
  const exact = (label, v, n) => known(label) && known(v) && fits(label, v, n);
  function allocation(receiver, node) {
    const a = ts.isIdentifier(receiver) ? allocations.get(sym(receiver)) : undefined;
    if (a && a.fn === activeFunction) return a;
    add('S5_MUTATION_ESCAPE', node, 'Container writes must target the const binding that allocated it in this function');
    return undefined;
  }
  const readonly = value => ({ ...value, fresh: false,
    ...(value.element ? { element: readonly(value.element) } : {}),
    ...(value.fields ? { fields: new Map([...value.fields].map(([k, x]) => [k, readonly(x)])) } : {}) });
  const labelKey = value => canonical({ tag: value.tag, fresh: !!value.fresh, module: !!value.module,
    ...(value.element ? { element: labelKey(value.element) } : {}),
    ...(value.fields ? { fields: [...value.fields].map(([k, v]) => [k, labelKey(v)]) } : {}) });
  function fromType(t, owner = 'borrowed', seen = new Set()) {
    if (!t) return { tag: 'unknown' };
    if (isScalarType(t)) return { tag: 'scalar', type: t };
    if (t.flags & (ts.TypeFlags.Any | ts.TypeFlags.Unknown | ts.TypeFlags.TypeParameter)) return { tag: 'unknown', type: t };
    if (seen.has(t)) return { tag: 'unknown', type: t, why: 'Recursive data shape' };
    const next = new Set(seen); next.add(t);
    if (t.isUnion()) return t.types.map(x => fromType(x, owner, next)).reduce(merge);
    if (builtinType(t, 'Date')) return { tag: 'date', type: t, fresh: owner === 'fresh', module: owner === 'module' };
    if (builtinType(t, 'Set')) return { tag: 'set', type: t, fresh: owner === 'fresh', module: owner === 'module', element: fromType(checker.getTypeArguments(t)[0], 'borrowed', next) };
    if (builtinType(t, 'RegExp')) return { tag: 'regex', type: t };
    if (checker.isArrayType(t) || checker.isTupleType(t) || symbolName(t) === 'ReadonlyArray') {
      const element = checker.getIndexTypeOfType(t, ts.IndexKind.Number);
      return { tag: 'array', type: t, fresh: owner === 'fresh', module: owner === 'module', element: fromType(element, 'borrowed', next) };
    }
    if (t.getCallSignatures().length || t.getConstructSignatures().length) return { tag: 'unknown', type: t, why: 'Function-bearing data' };
    const fields = new Map();
    for (const prop of t.getProperties()) {
      if (PROTOTYPE.has(prop.getName()) || prop.declarations?.some(d => ts.isMethodSignature(d) || ts.isMethodDeclaration(d) || ts.isGetAccessor(d) || ts.isSetAccessor(d))) return { tag: 'unknown', type: t, why: 'Method/accessor data' };
      fields.set(prop.getName(), fromType(checker.getTypeOfSymbolAtLocation(prop, prop.valueDeclaration || prop.declarations?.[0] || sf), 'borrowed', next));
    }
    return { tag: 'object', fields, type: t, fresh: owner === 'fresh', module: owner === 'module' };
  }
  function data(v, n) {
    if (!v || ['unknown','closure','selector','builtin','regex','error'].includes(v.tag)) { add('S5_TYPE_CAPABILITY', n, 'Only proved data may be stored or returned'); return false; }
    if (v.tag === 'array') return data(v.element, n);
    if (v.tag === 'object') return [...v.fields.values()].every(x => data(x, n));
    return true;
  }
  function safeScalar(v, n, code = 'S5_IMPLICIT_COERCION') {
    if (v.tag !== 'scalar' || !isScalarType(type(n) || v.type)) { add(code, n, 'Implicit conversion requires primitive scalar/nullish input'); return false; }
    return true;
  }
  function primitive(v, n, flag, code = 'S5_BUILTIN_ARGUMENT', optional = false) {
    const t = type(n) || v.type;
    if (v.tag !== 'scalar' || !isType(t, flag | (optional ? ts.TypeFlags.Undefined : 0))) {
      add(code, n, 'Builtin argument does not satisfy its primitive contract'); return false;
    }
    return true;
  }
  function constString(n, visited = new Set()) {
    if (!n) return undefined;
    if (ts.isStringLiteralLike(n)) return n.text;
    if (ts.isParenthesizedExpression(n) || ts.isAsExpression(n)) return constString(n.expression, visited);
    if (ts.isBinaryExpression(n) && n.operatorToken.kind === ts.SyntaxKind.PlusToken) {
      const a = constString(n.left, visited), b = constString(n.right, visited);
      if (a !== undefined && b !== undefined) return a + b;
    }
    if (ts.isIdentifier(n)) {
      const s = sym(n); if (!s || visited.has(s)) return undefined;
      const d = s.valueDeclaration;
      if (d && ts.isVariableDeclaration(d) && (d.parent.flags & ts.NodeFlags.Const)) return constString(d.initializer, new Set([...visited, s]));
    }
    return undefined;
  }
  function syntax(n) {
    if (!STRUCTURAL.has(n.kind) && !OPERATOR_TOKENS.has(n.kind)) add('S5_SYNTAX_UNKNOWN', n, `Unregistered SyntaxKind ${ts.SyntaxKind[n.kind]}`);
    if (n.modifiers?.some(m => ![ts.SyntaxKind.ExportKeyword, ts.SyntaxKind.ReadonlyKeyword].includes(m.kind))) add('S5_SYNTAX_UNKNOWN', n, 'Unregistered modifier');
    if (ts.isFunctionDeclaration(n) && (n.asteriskToken || !n.body)) add('S5_SYNTAX_UNKNOWN', n, 'Generator or ambient function');
    if ((ts.isVariableDeclaration(n) || ts.isParameter(n) || ts.isFunctionDeclaration(n)) && n.name && ts.isIdentifier(n.name) &&
        ['String','Number','Boolean','Math','Date','Set','Error','undefined','NaN','Infinity'].includes(n.name.text)) add('S5_GLOBAL_UNKNOWN', n, 'Shadowing a builtin identity is forbidden');
    if (ts.isForOfStatement(n) && n.awaitModifier) add('S5_SYNTAX_UNKNOWN', n, 'Async iteration');
    if (ts.isBreakOrContinueStatement(n) && n.label) add('S5_SYNTAX_UNKNOWN', n, 'Labelled control flow');
    if (ts.isTypeOperatorNode(n) && n.operator !== ts.SyntaxKind.ReadonlyKeyword) add('S5_TYPE_CAPABILITY', n, 'Unsupported type operator');
    if (ts.isTypeReferenceNode(n)) {
      const name = n.typeName.getText(sf), s = sym(n.typeName);
      const local = s?.declarations?.every(d => d.getSourceFile() === sf && ts.isTypeAliasDeclaration(d));
      const dto = policy.typeImports?.some(x => x.name === name);
      if (!(local || dto || ['Array','Set','Date','Record','const'].includes(name))) add('S5_TYPE_CAPABILITY', n, 'Unregistered type identity');
    }
    if (ts.isFunctionTypeNode(n) && !(ts.isParameter(n.parent) && n.parent.name.getText(sf) === 'value' && ['linePath','areaPath','isolatedPoints'].includes(n.parent.parent.name?.text) && policy.path.endsWith('/queueHistoryModel.ts'))) add('S5_TYPE_CAPABILITY', n, 'Function type is not a registered selector');
    if (ts.isAsExpression(n) && !(ts.isTypeReferenceNode(n.type) && n.type.typeName.getText(sf) === 'const')) add('S5_TYPE_CAPABILITY', n, 'Only as const is accepted; assertions cannot establish provenance');
    if (ts.isRegularExpressionLiteral(n) && (!REGEX.has(n.text) || !policy.regularExpressions?.includes(n.text))) add('S5_SYNTAX_UNKNOWN', n, 'Regular expression is not enrolled for this exact file');
    if (ts.isPropertyAccessExpression(n) && PROTOTYPE.has(n.name.text)) add('S5_PROTOTYPE_ACCESS', n, 'Prototype capability access');
    if (ts.isElementAccessExpression(n) && PROTOTYPE.has(constString(n.argumentExpression))) add('S5_PROTOTYPE_ACCESS', n, 'Computed prototype capability access');
    if ((ts.isPropertyAssignment(n) || ts.isPropertySignature(n) || ts.isMethodDeclaration(n) || ts.isGetAccessor(n) || ts.isSetAccessor(n) || ts.isBindingElement(n)) && n.name) {
      const keyNode = ts.isBindingElement(n) ? n.propertyName || n.name : n.name;
      const name = ts.isComputedPropertyName(keyNode) ? constString(keyNode.expression) : ts.isIdentifier(keyNode) ? keyNode.text : constString(keyNode);
      if (PROTOTYPE.has(name)) add('S5_PROTOTYPE_ACCESS', n, 'Special prototype key is forbidden');
    }
    if ([ts.SyntaxKind.AnyKeyword, ts.SyntaxKind.UnknownKeyword, ts.SyntaxKind.FunctionKeyword].includes(n.kind)) add('S5_TYPE_CAPABILITY', n, 'Unbounded/capability type');
    if (ts.isImportDeclaration(n)) {
      if (n.attributes) add('S5_IMPORT_UNKNOWN', n, 'Import attributes');
      const c = n.importClause;
      const moduleName = ts.isStringLiteral(n.moduleSpecifier) ? n.moduleSpecifier.text : '';
      if (!c || c.name || !c.namedBindings || !ts.isNamedImports(c.namedBindings)) add('S5_IMPORT_RUNTIME', n, 'Only named explicit type imports are accepted');
      else for (const s of c.namedBindings.elements) {
        if (!c.isTypeOnly && !s.isTypeOnly) add('S5_IMPORT_RUNTIME', s, 'Value import is forbidden even if erased');
        if (!(policy.typeImports || []).some(t => t.module === moduleName && t.name === (s.propertyName || s.name).text)) add('S5_IMPORT_UNKNOWN', s, 'Type import identity is not enrolled for this file');
      }
    }
    if (ts.isImportTypeNode(n)) add('S5_IMPORT_UNKNOWN', n, 'Import type query is not enrolled');
    if (ts.isExportDeclaration(n) || ts.isImportEqualsDeclaration(n)) add('S5_IMPORT_UNKNOWN', n, 'Re-export/import-equals is not enrolled');
    ts.forEachChild(n, syntax);
  }
  syntax(sf);
  if (sf.referencedFiles.length || sf.typeReferenceDirectives.length || sf.libReferenceDirectives.length) add('S5_IMPORT_UNKNOWN', sf, 'Triple-slash references are forbidden');

  function bind(name, value, scope, module = false) {
    if (ts.isIdentifier(name)) {
      const s = sym(name); if (s) { scope.set(s, value); if (module) moduleSymbols.add(s); }
      return;
    }
    if (ts.isArrayBindingPattern(name)) {
      if (value.tag !== 'array') add('S5_DATA_PROVENANCE', name, 'Array binding requires a proved ordinary array');
      for (const e of name.elements) if (ts.isBindingElement(e)) bind(e.name, e.initializer ? merge(value.element || unknown(e), expression(e.initializer, scope)) : value.element || unknown(e), scope, module);
      return;
    }
    if (ts.isObjectBindingPattern(name)) {
      if (value.tag !== 'object') add('S5_DATA_PROVENANCE', name, 'Object binding requires a proved plain-data object');
      for (const e of name.elements) {
      const k = e.propertyName?.text || e.name.text;
      const field = value.fields?.get(k) || unknown(e);
      if (field.tag === 'unknown') add('S5_DATA_PROVENANCE', e, 'Unknown field cannot establish a data binding');
      bind(e.name, e.initializer ? merge(field, expression(e.initializer, scope)) : field, scope, module);
      }
    }
  }
  function lookup(n, scope) {
    const s = sym(n);
    // A closure reads its captured bindings by reference: every use re-binds it
    // to the scope live at the reference site, so later reassignments are seen.
    if (scope.has(s)) { const v = scope.get(s); return v?.tag === 'closure' ? { ...v, captures: scope } : v; }
    if (env.has(s)) return env.get(s);
    if (locals.has(s)) return { tag: 'closure', node: locals.get(s), captures: scope, id: s };
    const name = n.text;
    if (['undefined','NaN','Infinity'].includes(name) && (!s || s.declarations?.every(d => ctx.program.isSourceFileDefaultLibrary(d.getSourceFile())))) return scalar(n);
    if (['String','Number','Boolean','Math','Date','Set','Error'].includes(name) && s?.declarations?.length && s.declarations.every(d => ctx.program.isSourceFileDefaultLibrary(d.getSourceFile()))) return { tag: 'builtin', id: name, type: type(n) };
    add('S5_GLOBAL_UNKNOWN', n, `Unregistered value ${name}`);
    return unknown(n);
  }
  function property(n, scope, method = false) {
    const receiver = expression(n.expression, scope);
    const key = ts.isPropertyAccessExpression(n) ? n.name.text : constString(n.argumentExpression);
    if (PROTOTYPE.has(key)) { add('S5_PROTOTYPE_ACCESS', n, 'Prototype capability access'); return unknown(n); }
    if (ts.isElementAccessExpression(n)) {
      const index = expression(n.argumentExpression, scope);
      if (receiver.tag === 'array') {
        primitive(index, n.argumentExpression, ts.TypeFlags.NumberLike, 'S5_SYNTAX_UNKNOWN');
        return receiver.element || unknown(n);
      }
      if (receiver.tag === 'object') {
        let keys = key === undefined ? undefined : [key];
        const kt = type(n.argumentExpression);
        if (!keys && kt?.isUnion() && kt.types.every(t => t.isStringLiteral())) keys = kt.types.map(t => t.value);
        if (keys?.length && keys.every(k => receiver.fields.has(k) && !PROTOTYPE.has(k))) return keys.map(k => receiver.fields.get(k)).reduce(merge);
      }
      add('S5_SYNTAX_UNKNOWN', n, 'Dynamic property is outside the finite data-key contract');
      return unknown(n);
    }
    if (method) return { receiver, key };
    if (receiver.tag === 'object' && receiver.fields?.has(key)) return receiver.fields.get(key);
    if ((receiver.tag === 'array' || (receiver.tag === 'scalar' && isType(type(n.expression), ts.TypeFlags.StringLike))) && key === 'length') return scalar(n);
    if (receiver.tag === 'builtin' && receiver.id === 'Number' && key === 'POSITIVE_INFINITY') return scalar(n);
    add('S5_CALL_UNKNOWN', n, `Unregistered data property or extracted method ${key}`);
    return unknown(n);
  }
  function mutate(target, value, scope, node) {
    if (ts.isIdentifier(target)) {
      const s = sym(target), before = scope.get(s) || env.get(s);
      const d = s?.valueDeclaration;
      let owner = d?.parent;
      while (owner && !ts.isFunctionLike(owner) && !ts.isSourceFile(owner)) owner = owner.parent;
      if (owner && ts.isFunctionLike(owner) && owner !== activeFunction) {
        add('S5_MUTATION_ESCAPE', node, 'Captured bindings are read-only'); return;
      }
      if (moduleSymbols.has(s) || !scope.has(s) || (d && ts.isVariableDeclaration(d) && (d.parent.flags & ts.NodeFlags.Const))) {
        add('S5_MUTATION_ESCAPE', node, 'Writes must target a mutable local binding'); return;
      }
      if (before?.tag === 'selector' || value?.tag === 'selector') add('S5_SELECTOR_UNKNOWN', node, 'Selector rebinding is forbidden');
      if (before?.tag === 'closure' || value?.tag === 'closure' || value?.tag === 'builtin') add('S5_MUTATION_ESCAPE', node, 'Callable rebinding/escape is forbidden');
      scope.set(s, merge(before, value));
      return;
    }
    if (ts.isPropertyAccessExpression(target) || ts.isElementAccessExpression(target)) {
      const receiver = expression(target.expression, scope);
      const before = property(target, scope);
      if (!receiver.fresh || receiver.module || !['object','array'].includes(receiver.tag)) add('S5_MUTATION_ESCAPE', node, 'Borrowed, nested borrowed and module data are read-only');
      if (before.tag !== 'scalar' || value.tag !== 'scalar') add('S5_MUTATION_ESCAPE', node, 'Field writes may not replace a source label or object graph');
      const owner = allocation(target.expression, node);
      // Arrays never shrink (no length writes, pop, shift or splice), so the
      // allocation literal's length is a lower bound: only an integer literal
      // index below it is in bounds. Length writes are never accepted.
      if (owner && receiver.tag === 'array') {
        const index = ts.isElementAccessExpression(target) ? target.argumentExpression : undefined;
        if (!index || !ts.isNumericLiteral(index) || !Number.isInteger(+index.text) || +index.text >= owner.length) add('S5_MUTATION_ESCAPE', node, 'Array writes must use a literal index within the allocated length; length writes are not accepted');
      }
      data(value, node);
      return;
    }
    add('S5_MUTATION_ESCAPE', node, 'Unregistered write target');
  }
  function callClosure(closure, args, argNodes, call) {
    if (closure.tag !== 'closure' || !closure.node) { add('S5_CALL_UNKNOWN', call, 'Not a unique local closure'); return unknown(call); }
    const fn = closure.node;
    if (!fn.body) { add('S5_SYNTAX_UNKNOWN', fn, 'Local callable must have an implementation body'); return unknown(call); }
    if (stack.includes(fn)) { add('S5_CALL_UNKNOWN', call, 'Recursive/cyclic local call graph'); return unknown(call); }
    if (stack.length >= 64) { add('S5_CALL_UNKNOWN', call, 'Local call graph exceeds finite analysis bound'); return unknown(call); }
    stack.push(fn);
    const previous = activeFunction; activeFunction = fn;
    const local = new Map([...(closure.captures || env)].map(([s, v]) => [s, readonly(v)]));
    for (let i = 0; i < fn.parameters.length; i++) {
      const p = fn.parameters[i];
      const defaultValue = p.initializer ? expression(p.initializer, local) : undefined;
      let value = args[i];
      if (!value) value = defaultValue || fromType(type(p.name));
      else if (defaultValue && nullable(value.type)) value = merge(value, defaultValue);
      bind(p.name, value, local);
    }
    const result = ts.isBlock(fn.body) ? block(fn.body, local) : expression(fn.body, local);
    activeFunction = previous; stack.pop();
    return result || scalar(call);
  }
  const count = (n, min, max) => {
    const length = n.arguments?.length || 0;
    if (length < min || length > max) add('S5_BUILTIN_ARGUMENT', n, `Expected ${min}..${max} arguments`);
  };
  function callback(v, args, nodes, call) {
    if (v.tag === 'builtin' && v.id === 'Boolean') { for (const a of args.slice(0, 1)) if (a.tag !== 'scalar' || !isScalarType(a.type)) add('S5_BUILTIN_ARGUMENT', call, 'Boolean predicate only accepts scalar/nullish elements'); return scalar(call); }
    if (v.tag !== 'closure') { add('S5_SELECTOR_UNKNOWN', call, 'Callback must be a unique local closure'); return unknown(call); }
    // Fresh data captured by a callback remains readable but cannot be mutated by it.
    const captures = new Map([...v.captures].map(([s, x]) => [s, readonly(x)]));
    return callClosure({ ...v, captures }, args, nodes, call);
  }
  function standardSignature(n) {
    const signature = checker.getResolvedSignature(n), d = signature?.declaration;
    if (!d || !ctx.program.isSourceFileDefaultLibrary(d.getSourceFile())) add('S5_CALL_UNKNOWN', n, 'Builtin dispatch must resolve to the fixed standard library');
    const target = symbolID(resolvedSymbol(checker, ts.isPropertyAccessExpression(n.expression) ? n.expression.name : n.expression), checker);
    const form = ts.isNewExpression(n) ? 'construct' : ts.isPropertyAccessExpression(n.expression) ? 'method' : 'call';
    if (!(policy.allowedCalls || []).some(rule => rule.form === form && canonical(rule.target) === canonical(target))) add('S5_CALL_UNKNOWN', n, 'Builtin member/form is outside this exact FilePolicy');
  }
  function call(n, scope) {
    const construct = ts.isNewExpression(n), args = [...(n.arguments || [])].map(x => expression(x, scope));
    const argNodes = [...(n.arguments || [])];
    if (n.expression.kind === ts.SyntaxKind.ImportKeyword) { add('S5_IMPORT_RUNTIME', n, 'Dynamic imports are forbidden'); return unknown(n); }
    if (ts.isIdentifier(n.expression)) {
      const callee = lookup(n.expression, scope);
      if (callee.tag === 'closure') {
        if (construct) add('S5_CALL_UNKNOWN', n, 'Local constructors are not permitted');
        return callClosure(callee, args, argNodes, n);
      }
      if (callee.tag === 'selector') {
        if (construct || args.length !== 1) add('S5_SELECTOR_UNKNOWN', n, 'Selector shape changed');
        return scalar(n); // exact origin, input and call-site edges are checked by contracts.cjs
      }
      if (callee.tag !== 'builtin') { add('S5_CALL_UNKNOWN', n, 'Unknown callee, including capability aliases'); return unknown(n); }
      standardSignature(n);
      const id = callee.id;
      if (!construct && ['String','Number','Boolean'].includes(id)) {
        count(n, 1, 1); args.forEach((v, i) => safeScalar(v, argNodes[i])); return scalar(n);
      }
      if (construct && id === 'Date') {
        if (args.length > 1) add('S5_BUILTIN_ARGUMENT', n, 'Date constructor accepts one primitive epoch number');
        if (args.length) primitive(args[0], argNodes[0], ts.TypeFlags.NumberLike);
        return { tag: 'date', fresh: true, type: type(n) }; // clock-site matching is independent
      }
      if (construct && id === 'Set') {
        count(n, 0, 1);
        const element = args[0]?.element || { tag: 'scalar', type: checker.getNumberType() };
        if (args.length && (args[0].tag !== 'array' || element.tag !== 'scalar' || !isScalarType(element.type))) add('S5_BUILTIN_ARGUMENT', n, 'Set input must be a proved scalar/nullish array');
        return { tag: 'set', fresh: true, type: type(n), element };
      }
      if (construct && id === 'Error') { count(n, 1, 1); if (args[0]) primitive(args[0], argNodes[0], ts.TypeFlags.StringLike); return { tag: 'error', type: type(n) }; }
      add('S5_CALL_UNKNOWN', n, `Unregistered builtin form ${construct ? 'new ' : ''}${id}`); return unknown(n);
    }
    if (!ts.isPropertyAccessExpression(n.expression)) {
      expression(n.expression, scope);
      add('S5_CALL_UNKNOWN', n, 'Computed/dynamic method dispatch is forbidden'); return unknown(n);
    }
    const { receiver: r, key: method } = property(n.expression, scope, true);
    if (!r) return unknown(n);
    standardSignature(n);
    const numberArgs = (min, max, optional = true) => { count(n, min, max); args.forEach((v, i) => primitive(v, argNodes[i], ts.TypeFlags.NumberLike, 'S5_BUILTIN_ARGUMENT', optional && i >= min)); };
    const noArgs = () => count(n, 0, 0);
    if (r.tag === 'builtin') {
      if (r.id === 'Number' && method === 'isFinite') { count(n, 1, 1); if (args[0]) primitive(args[0], argNodes[0], ts.TypeFlags.NumberLike); return scalar(n); }
      if (r.id === 'Math' && ['abs','floor','max','min','pow','log10','round'].includes(method)) {
        numberArgs(['max','min'].includes(method) ? 1 : method === 'pow' ? 2 : 1, ['max','min'].includes(method) ? 100 : method === 'pow' ? 2 : 1, false); return scalar(n);
      }
      if (r.id === 'Date' && method === 'now') { noArgs(); return scalar(n); }
      if (r.id === 'Date' && method === 'parse') { count(n, 1, 1); if (args[0]) primitive(args[0], argNodes[0], ts.TypeFlags.StringLike); return scalar(n); }
    }
    if (r.tag === 'scalar' && isType(type(n.expression.expression), ts.TypeFlags.StringLike)) {
      if (['trim','toLowerCase','toUpperCase'].includes(method)) { noArgs(); return scalar(n); }
      if (['startsWith','includes'].includes(method)) {
        count(n, 1, 2); args.forEach((v, i) => primitive(v, argNodes[i], i === 0 ? ts.TypeFlags.StringLike : ts.TypeFlags.NumberLike, 'S5_BUILTIN_ARGUMENT', i > 0)); return scalar(n);
      }
      if (method === 'slice') { numberArgs(0, 2); return scalar(n); }
      if (method === 'padStart') {
        count(n, 1, 2); args.forEach((v, i) => primitive(v, argNodes[i], i === 0 ? ts.TypeFlags.NumberLike : ts.TypeFlags.StringLike, 'S5_BUILTIN_ARGUMENT', i > 0)); return scalar(n);
      }
      if (method === 'split') {
        count(n, 0, 2);
        if (args[0] && args[0].tag !== 'regex') primitive(args[0], argNodes[0], ts.TypeFlags.StringLike, 'S5_BUILTIN_ARGUMENT', true);
        if (args[1]) primitive(args[1], argNodes[1], ts.TypeFlags.NumberLike, 'S5_BUILTIN_ARGUMENT', true);
        return { tag: 'array', fresh: true, element: { tag: 'scalar', type: checker.getStringType() }, type: type(n) };
      }
      if (method === 'match') {
        count(n, 1, 1); if (args[0]?.tag !== 'regex') add('S5_BUILTIN_ARGUMENT', n, 'match accepts only registered regex');
        return { tag: 'array', fresh: true, nullable: true, element: { tag: 'scalar', type: checker.getStringType() }, type: type(n) };
      }
    }
    if (r.tag === 'scalar' && isType(type(n.expression.expression), ts.TypeFlags.NumberLike) && method === 'toFixed') {
      numberArgs(1, 1);
      const v = argNodes[0];
      const validDigits = x => ts.isNumericLiteral(x) ? +x.text >= 0 && +x.text <= 100 : ts.isConditionalExpression(x) && validDigits(x.whenTrue) && validDigits(x.whenFalse);
      if (v && !validDigits(v)) add('S5_BUILTIN_ARGUMENT', v, 'toFixed digits must be statically within 0..100');
      return scalar(n);
    }
    if (r.tag === 'regex' && method === 'test') {
      count(n, 1, 1); if (r.pattern?.endsWith('/g') || r.pattern?.endsWith('/y')) add('S5_BUILTIN_ARGUMENT', n, 'Stateful regex test is forbidden');
      if (args[0]) primitive(args[0], argNodes[0], ts.TypeFlags.StringLike); return scalar(n);
    }
    if (r.tag === 'array') {
      if (['map','filter','find','every'].includes(method)) {
        count(n, 1, 1);
        const result = callback(args[0] || unknown(n), [r.element, { tag: 'scalar', type: checker.getNumberType() }], [n.expression.expression, n.expression.expression], n);
        if (method === 'every') return scalar(n);
        if (method === 'find') return { ...r.element, nullable: true };
        const element = method === 'map' ? result : r.element;
        data(element, n);
        return { tag: 'array', fresh: true, element, type: type(n) };
      }
      if (method === 'slice') { numberArgs(0, 2); return { ...r, fresh: true, module: false, type: type(n) }; }
      if (method === 'join') {
        count(n, 0, 1);
        if (r.element?.tag !== 'scalar' || !isScalarType(r.element.type)) add('S5_IMPLICIT_COERCION', n, 'join elements must all be scalar/nullish, including helper results');
        args.forEach((v, i) => safeScalar(v, argNodes[i])); return scalar(n);
      }
      if (method === 'push') {
        if (!r.fresh || r.module) add('S5_MUTATION_ESCAPE', n, 'push requires a fresh local array');
        args.forEach((v, i) => data(v, argNodes[i]));
        const owner = allocation(n.expression.expression, n);
        args.forEach((v, i) => { if (owner && !exact(owner.label.element, v, argNodes[i])) add('S5_BUILTIN_ARGUMENT', argNodes[i], 'push element must exactly fit the allocated element label'); });
        return scalar(n);
      }
      if (method === 'sort') {
        if (!r.fresh || r.module) add('S5_MUTATION_ESCAPE', n, 'sort requires a fresh local array');
        count(n, 1, 1);
        const comparator = args[0];
        if (comparator?.tag !== 'closure' || !ts.isArrowFunction(comparator.node) || !ts.isBinaryExpression(comparator.node.body) || comparator.node.body.operatorToken.kind !== ts.SyntaxKind.MinusToken) add('S5_BUILTIN_ARGUMENT', n, 'sort requires an explicit numeric difference comparator');
        if (comparator) callback(comparator, [r.element, r.element], [n, n], n);
        return r;
      }
    }
    if (r.tag === 'set' && ['has','add'].includes(method)) {
      count(n, 1, 1); args.forEach((v, i) => safeScalar(v, argNodes[i], 'S5_BUILTIN_ARGUMENT'));
      if (method === 'add' && (!r.fresh || r.module)) add('S5_MUTATION_ESCAPE', n, 'Set.add requires a fresh local set');
      if (method === 'add') allocation(n.expression.expression, n);
      return method === 'add' ? r : scalar(n);
    }
    if (r.tag === 'date') {
      if (['getTime','getUTCDay','getUTCHours','getUTCMinutes'].includes(method)) { noArgs(); return scalar(n); }
      if (['setUTCSeconds','setUTCMinutes'].includes(method)) {
        if (!r.fresh || r.module) add('S5_MUTATION_ESCAPE', n, 'Date setter requires a fresh local Date');
        numberArgs(1, method === 'setUTCSeconds' ? 2 : 3, false); return scalar(n);
      }
      if (method === 'toLocaleString') { noArgs(); return scalar(n); }
    }
    add('S5_CALL_UNKNOWN', n, `Unregistered ${r.tag} member ${method}`);
    if (r.tag === 'unknown') add('S5_MUTATION_ESCAPE', n, 'A method on an unproved receiver may write outside fresh local data');
    return unknown(n);
  }
  // Writes are accepted only as a whole statement (or a for-loop initializer /
  // incrementor). Assignments nested in conditional, short-circuit, call or
  // closure-body expressions are not a registered form.
  function statementWrite(n) {
    const p = n.parent;
    if (ts.isExpressionStatement(p) || (ts.isForStatement(p) && (p.incrementor === n || p.initializer === n))) return;
    add('S5_SYNTAX_UNKNOWN', n, 'Assignment/update is only permitted as a whole statement');
  }
  function expression(n, scope) {
    if (!n) return { tag: 'scalar', type: checker.getUndefinedType() };
    if (ts.isIdentifier(n)) return lookup(n, scope);
    if (ts.isLiteralExpression(n) || [ts.SyntaxKind.TrueKeyword, ts.SyntaxKind.FalseKeyword, ts.SyntaxKind.NullKeyword].includes(n.kind)) {
      if (ts.isRegularExpressionLiteral(n)) return { tag: 'regex', pattern: n.text, type: type(n) };
      return scalar(n);
    }
    if (ts.isParenthesizedExpression(n) || ts.isNonNullExpression(n) || ts.isAsExpression(n)) return expression(n.expression, scope);
    if (ts.isPropertyAccessExpression(n) || ts.isElementAccessExpression(n)) return property(n, scope);
    if (ts.isArrowFunction(n)) {
      const closure = { tag: 'closure', node: n, captures: new Map(scope), id: n };
      // Inspect even an unused closure; dead source is part of the content policy.
      callClosure(closure, n.parameters.map(p => fromType(type(p.name))), n.parameters.map(p => p.name), n);
      return closure;
    }
    if (ts.isCallExpression(n) || ts.isNewExpression(n)) return call(n, scope);
    if (ts.isArrayLiteralExpression(n)) {
      const values = n.elements.map(e => expression(e, scope));
      values.forEach((v, i) => data(v, n.elements[i]));
      const contextual = checker.getContextualType(n);
      const declaredElement = contextual && checker.getIndexTypeOfType(contextual, ts.IndexKind.Number);
      return { tag: 'array', fresh: true, type: type(n), element: values.length ? values.reduce(merge) : declaredElement ? fromType(declaredElement) : { tag: 'scalar', type: checker.getNeverType() } };
    }
    if (ts.isObjectLiteralExpression(n)) {
      const fields = new Map();
      for (const p of n.properties) {
        let key;
        if (ts.isPropertyAssignment(p)) {
          key = ts.isIdentifier(p.name) ? p.name.text : constString(p.name);
          const v = expression(p.initializer, scope); data(v, p); fields.set(key, v);
        } else if (ts.isShorthandPropertyAssignment(p)) {
          key = p.name.text;
          const s = checker.getShorthandAssignmentValueSymbol(p);
          const v = scope.get(s) || env.get(s) || unknown(p);
          data(v, p); fields.set(key, v);
        } else add('S5_DATA_PROVENANCE', p, 'Object methods/accessors/spreads are not plain-data construction');
        if (PROTOTYPE.has(key)) add('S5_PROTOTYPE_ACCESS', p, 'Special prototype key');
      }
      return { tag: 'object', fresh: true, fields, type: type(n) };
    }
    if (ts.isTemplateExpression(n)) { for (const span of n.templateSpans) safeScalar(expression(span.expression, scope), span.expression); return scalar(n); }
    if (ts.isConditionalExpression(n)) {
      expression(n.condition, scope);
      // Both branches update the live scope; every write is a weak update, so the
      // result is the merge over all paths.
      return merge(expression(n.whenTrue, scope), expression(n.whenFalse, scope));
    }
    if (ts.isBinaryExpression(n)) {
      const op = n.operatorToken.kind, a = expression(n.left, scope), b = expression(n.right, scope);
      if ([ts.SyntaxKind.EqualsToken, ts.SyntaxKind.PlusEqualsToken, ts.SyntaxKind.MinusEqualsToken].includes(op)) {
        statementWrite(n);
        if (op !== ts.SyntaxKind.EqualsToken) { safeScalar(a, n.left); safeScalar(b, n.right); }
        mutate(n.left, b, scope, n); return b;
      }
      if ([ts.SyntaxKind.AmpersandAmpersandToken, ts.SyntaxKind.BarBarToken, ts.SyntaxKind.QuestionQuestionToken].includes(op)) return merge(a, b);
      if ([ts.SyntaxKind.EqualsEqualsEqualsToken, ts.SyntaxKind.ExclamationEqualsEqualsToken].includes(op)) return scalar(n);
      if ([ts.SyntaxKind.EqualsEqualsToken, ts.SyntaxKind.ExclamationEqualsToken].includes(op)) {
        if (!isType(type(n.left), ts.TypeFlags.Null | ts.TypeFlags.Undefined) && !isType(type(n.right), ts.TypeFlags.Null | ts.TypeFlags.Undefined)) add('S5_IMPLICIT_COERCION', n, 'Loose equality is restricted to nullish checks');
        return scalar(n);
      }
      if (!OPERATOR_TOKENS.has(op)) add('S5_SYNTAX_UNKNOWN', n, 'Unregistered binary operator');
      safeScalar(a, n.left); safeScalar(b, n.right);
      if (![ts.SyntaxKind.PlusToken,ts.SyntaxKind.LessThanToken,ts.SyntaxKind.LessThanEqualsToken,ts.SyntaxKind.GreaterThanToken,ts.SyntaxKind.GreaterThanEqualsToken].includes(op)) {
        primitive(a, n.left, ts.TypeFlags.NumberLike, 'S5_IMPLICIT_COERCION'); primitive(b, n.right, ts.TypeFlags.NumberLike, 'S5_IMPLICIT_COERCION');
      } else {
        primitive(a, n.left, ts.TypeFlags.NumberLike | ts.TypeFlags.StringLike, 'S5_IMPLICIT_COERCION'); primitive(b, n.right, ts.TypeFlags.NumberLike | ts.TypeFlags.StringLike, 'S5_IMPLICIT_COERCION');
      }
      return scalar(n);
    }
    if (ts.isPrefixUnaryExpression(n) || ts.isPostfixUnaryExpression(n)) {
      const value = expression(n.operand, scope);
      if (n.operator !== ts.SyntaxKind.ExclamationToken) primitive(value, n.operand, ts.TypeFlags.NumberLike, 'S5_IMPLICIT_COERCION');
      if ([ts.SyntaxKind.PlusPlusToken, ts.SyntaxKind.MinusMinusToken].includes(n.operator)) { statementWrite(n); mutate(n.operand, value, scope, n); }
      if (![ts.SyntaxKind.ExclamationToken, ts.SyntaxKind.PlusToken, ts.SyntaxKind.MinusToken, ts.SyntaxKind.PlusPlusToken, ts.SyntaxKind.MinusMinusToken].includes(n.operator)) add('S5_SYNTAX_UNKNOWN', n, 'Unregistered unary operator');
      return scalar(n);
    }
    add('S5_SYNTAX_UNKNOWN', n, `Unregistered expression ${ts.SyntaxKind[n.kind]}`);
    // Still diagnose prohibited capabilities inside rejected syntax (never accept it).
    ts.forEachChild(n, c => { if (ts.isExpression(c)) expression(c, scope); });
    return unknown(n);
  }
  function variables(list, scope, isModule = false) {
    if (!(list.flags & (ts.NodeFlags.Const | ts.NodeFlags.Let))) add('S5_SYNTAX_UNKNOWN', list, 'var is not permitted');
    if (isModule && !(list.flags & ts.NodeFlags.Const)) add('S5_MUTATION_ESCAPE', list, 'Module bindings must be const');
    for (const d of list.declarations) {
      let v = d.initializer ? expression(d.initializer, scope) : fromType(type(d.name));
      if (isModule) {
        const literal = n => n && (ts.isLiteralExpression(n) && !ts.isRegularExpressionLiteral(n) || [ts.SyntaxKind.TrueKeyword,ts.SyntaxKind.FalseKeyword,ts.SyntaxKind.NullKeyword].includes(n.kind) ||
          (ts.isAsExpression(n) && literal(n.expression)) || (ts.isArrayLiteralExpression(n) && n.elements.every(literal)) ||
          (ts.isObjectLiteralExpression(n) && n.properties.every(p => ts.isPropertyAssignment(p) && literal(p.initializer))) ||
          (ts.isPrefixUnaryExpression(n) && [ts.SyntaxKind.MinusToken,ts.SyntaxKind.PlusToken].includes(n.operator) && ts.isNumericLiteral(n.operand)) ||
          (ts.isNewExpression(n) && ts.isIdentifier(n.expression) && n.expression.text === 'Set' && (n.arguments?.length || 0) <= 1 && (!n.arguments?.length || literal(n.arguments[0]))));
        if (!literal(d.initializer)) add('S5_SYNTAX_UNKNOWN', d, 'Module initializer must be literal data or a fixed scalar Set');
        const freeze = value => ({ ...value, fresh: false, module: true,
          ...(value.element ? { element: freeze(value.element) } : {}),
          ...(value.fields ? { fields: new Map([...value.fields].map(([k, x]) => [k, freeze(x)])) } : {}) });
        v = freeze(v);
      }
      if (v.tag === 'selector' || v.tag === 'builtin') add(v.tag === 'selector' ? 'S5_SELECTOR_UNKNOWN' : 'S5_MUTATION_ESCAPE', d, 'Callable alias is not an enrolled binding');
      if (v.tag === 'closure' && (!d.initializer || !ts.isArrowFunction(d.initializer))) add('S5_MUTATION_ESCAPE', d, 'Function alias escape is forbidden');
      const init = d.initializer;
      if (!isModule && ts.isIdentifier(d.name) && (list.flags & ts.NodeFlags.Const) && init &&
          (ts.isArrayLiteralExpression(init) || ts.isObjectLiteralExpression(init) || (ts.isNewExpression(init) && ts.isIdentifier(init.expression) && init.expression.text === 'Set'))) {
        allocations.set(sym(d.name), { fn: activeFunction, label: v, length: ts.isArrayLiteralExpression(init) ? init.elements.length : 0 });
      }
      bind(d.name, v, scope, isModule);
    }
  }
  function mergeScopes(target, branches) {
    for (const key of target.keys()) {
      const values = branches.map(b => b.get(key)).filter(Boolean);
      if (values.length) target.set(key, values.reduce(merge));
    }
  }
  function statement(n, scope) {
    if (ts.isBlock(n)) return block(n, scope);
    if (ts.isReturnStatement(n)) { const v = expression(n.expression, scope); data(v, n); return v; }
    if (ts.isVariableStatement(n)) { variables(n.declarationList, scope); return; }
    if (ts.isExpressionStatement(n)) {
      const expr = n.expression;
      if (!(ts.isBinaryExpression(expr) && [ts.SyntaxKind.EqualsToken,ts.SyntaxKind.PlusEqualsToken,ts.SyntaxKind.MinusEqualsToken].includes(expr.operatorToken.kind)) &&
          !ts.isCallExpression(expr) && !ts.isPostfixUnaryExpression(expr) && !ts.isPrefixUnaryExpression(expr)) add('S5_SYNTAX_UNKNOWN', n, 'Expression statement must be a local write/mutation');
      expression(expr, scope); return;
    }
    if (ts.isIfStatement(n)) {
      expression(n.expression, scope);
      const yes = new Map(scope), no = new Map(scope);
      const a = statement(n.thenStatement, yes), b = n.elseStatement && statement(n.elseStatement, no);
      mergeScopes(scope, [yes, no]); return merge(a, b);
    }
    if (ts.isSwitchStatement(n)) {
      expression(n.expression, scope);
      const branches = [], values = [];
      for (const c of n.caseBlock.clauses) {
        if (ts.isCaseClause(c)) expression(c.expression, scope);
        // A clause is also reached by falling through from the previous one.
        const branch = new Map(scope); if (branches.length) mergeScopes(branch, [scope, branches[branches.length - 1]]);
        branches.push(branch);
        for (const s of c.statements) { const v = statement(s, branch); if (v) values.push(v); }
      }
      mergeScopes(scope, branches); return values.reduce(merge, undefined);
    }
    if (ts.isForStatement(n) || ts.isForOfStatement(n) || ts.isWhileStatement(n)) {
      // Snapshot complete nested labels before analysis: mutators may update a
      // value reachable through several aliases in the same lexical environment.
      const beforeLabels = new Map([...scope].map(([s, v]) => [s, labelKey(v)]));
      const loop = new Map(scope);
      if (ts.isForOfStatement(n)) {
        const input = expression(n.expression, loop);
        if (input.tag !== 'array') add('S5_DATA_PROVENANCE', n, 'Only proved ordinary arrays may be iterated');
        if (ts.isVariableDeclarationList(n.initializer)) for (const d of n.initializer.declarations) bind(d.name, input.element || unknown(n), loop);
        else add('S5_MUTATION_ESCAPE', n, 'Iteration target must be a new local binding');
      } else if (ts.isForStatement(n)) {
        if (n.initializer) ts.isVariableDeclarationList(n.initializer) ? variables(n.initializer, loop) : expression(n.initializer, loop);
        if (n.condition) expression(n.condition, loop);
      } else expression(n.expression, loop);
      const result = statement(n.statement, loop);
      if (ts.isForStatement(n) && n.incrementor) expression(n.incrementor, loop);
      // Labels must be invariant over an unbounded number of iterations. A changing label
      // is rejected instead of pretending one iteration proves a fixed point.
      for (const [s, label] of beforeLabels) {
        const after = loop.get(s);
        if (after && label !== labelKey(after)) add('S5_DATA_PROVENANCE', n, 'Loop changes a nested source label; no general fixed-point analysis');
      }
      mergeScopes(scope, [scope, loop]); return result;
    }
    if (ts.isTryStatement(n)) {
      const yes = new Map(scope);
      const a = block(n.tryBlock, yes);
      // The catch may start at any point of the try: weak writes make the merge of
      // the pre-try and end-of-try labels an upper bound of every such point.
      const no = new Map(scope); mergeScopes(no, [scope, yes]);
      let b;
      if (n.catchClause) {
        if (n.catchClause.variableDeclaration) bind(n.catchClause.variableDeclaration.name, { tag: 'error' }, no);
        b = block(n.catchClause.block, no);
      }
      mergeScopes(scope, [yes, no]);
      const c = n.finallyBlock && block(n.finallyBlock, scope);
      return merge(merge(a, b), c);
    }
    if (ts.isThrowStatement(n)) {
      if (expression(n.expression, scope).tag !== 'error') add('S5_TYPE_CAPABILITY', n, 'Only a local Error may be thrown'); return;
    }
    if (ts.isBreakOrContinueStatement(n)) return;
    if (ts.isFunctionDeclaration(n)) {
      const s = sym(n.name), closure = { tag: 'closure', node: n, captures: scope, id: s };
      locals.set(s, n); scope.set(s, closure);
      // Inspect even an uncalled nested declaration, with its enclosing scope.
      callClosure(closure, n.parameters.map(p => fromType(type(p.name))), n.parameters.map(p => p.name), n);
      return;
    }
    add('S5_SYNTAX_UNKNOWN', n, `Unregistered statement ${ts.SyntaxKind[n.kind]}`);
  }
  function block(n, scope) {
    const returns = [];
    for (const s of n.statements) { const value = statement(s, scope); if (value) returns.push(value); }
    return returns.reduce(merge, undefined);
  }
  for (const s of sf.statements) if (ts.isFunctionDeclaration(s) && s.name) locals.set(sym(s.name), s);
  for (const s of sf.statements) {
    if (ts.isVariableStatement(s)) variables(s.declarationList, env, true);
    else if (![ts.SyntaxKind.FunctionDeclaration, ts.SyntaxKind.TypeAliasDeclaration, ts.SyntaxKind.ImportDeclaration].includes(s.kind)) add('S5_SYNTAX_UNKNOWN', s, 'Unregistered module statement');
  }
  // Only module-level declarations; nested ones are inspected where declared.
  for (const [id, fn] of [...locals]) {
    const args = fn.parameters.map(p => {
      if (p.name.getText(sf) === 'value' && ['linePath','areaPath','isolatedPoints'].includes(fn.name?.text) && policy.path.endsWith('/queueHistoryModel.ts')) return { tag: 'selector', id: `${fn.name.text}:value`, type: type(p.name) };
      const value = fromType(type(p.name));
      if (value.tag === 'unknown') add('S5_TYPE_CAPABILITY', p, 'Parameter has an unknown or capability-bearing type');
      return value;
    });
    callClosure({ tag: 'closure', node: fn, captures: env, id }, args, fn.parameters.map(p => p.name), fn);
  }
  return findings;
}
module.exports = { analyze };
