package checker

import (
	"slices"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/diagnostics"
	"github.com/microsoft/typescript-go/internal/printer"
)

// The plain TypeScript API of the own/shared threads extension (only with `--ownRuntime`).
//
// Programs need not use the extension's syntax: the module that `ownRuntime` names (threads.d.mts in
// porffor's threads/escape-proto) exports markers that stock TypeScript accepts, and the checker
// gives them the syntax's meaning:
//
//	Sending<T>                  sending T (an alias of T: on a parameter, a rest parameter, a result)
//	Shared<T>                   shared T (deeply readonly)
//	consume(x)                  consume x (emitted as x)
//	a type assignable to Cell   a cell: shared and region-less, as `shared BarrierCell` was
//	const S = struct({ a: f64, next: ref<T>() })
//	const S = struct<I>({ ... })
//	                            shared struct S { a: number; next: T | null } (emitted as
//	                            `__sharedStruct("S", ["a", "next"], [1])`): its fields in the order of
//	                            the object literal, `f64` a number field and `ref<T>()` a reference
//	                            field (and `bool`, `str`, `i32` and `u8`, see OwnFieldKind). Its values
//	                            have type `Struct<typeof S>` (the fields & Cell) or, with an interface,
//	                            I (& Cell, | null).
//	i32, u8                     integer types: `number & { readonly [int32Brand]?: never }`, a number
//	                            with a phantom brand, which plain numbers are assignable to
//	atomic.add(s, "count", 1)   Atomics.add on i32 field `count` of a shared struct, or on an element of
//	atomic.add(a, i, 1)         a SharedArray<i32> (and load, store, sub, and, or, xor, exchange,
//	                            compareExchange, wait, notify)
//
// A marker is recognized by its declaration: an export of the runtime module, by name.

// The name of the runtime export a symbol is (or is an alias of), or "" if it is none.
func (c *Checker) getOwnRuntimeExportName(symbol *ast.Symbol) string {
	if c.compilerOptions.OwnRuntime == "" || symbol == nil {
		return ""
	}
	if symbol.Flags&ast.SymbolFlagsAlias != 0 {
		symbol = c.resolveAlias(symbol)
		if symbol == nil || symbol == c.unknownSymbol {
			return ""
		}
	}
	if core.Some(symbol.Declarations, func(d *ast.Node) bool { return c.isOwnRuntimeFile(ast.GetSourceFileOfNode(d)) }) {
		return symbol.Name
	}
	return ""
}

// The marker a type node refers to: "Sending" or "Shared" for `Sending<T>` and `Shared<T>`, or "".
func (c *Checker) getOwnTypeMarker(t *ast.Node) string {
	if t == nil || !ast.IsTypeReferenceNode(t) || len(t.TypeArguments()) != 1 || c.compilerOptions.OwnRuntime == "" {
		return ""
	}
	switch name := c.getOwnRuntimeExportName(c.getSymbolAtLocation(t.AsTypeReferenceNode().TypeName, true /*ignoreErrors*/)); name {
	case "Sending", "Shared":
		return name
	}
	return ""
}

// The marker function a call calls: "consume", "struct", "ref", or "".
func (c *Checker) getOwnCallMarker(call *ast.Node) string {
	if c.compilerOptions.OwnRuntime == "" || call == nil || !ast.IsCallExpression(call) {
		return ""
	}
	callee := ast.SkipParentheses(call.Expression())
	if !ast.IsIdentifier(callee) && !ast.IsPropertyAccessExpression(callee) {
		return ""
	}
	// Check the name first: resolving every call's signature would be wasteful.
	var name string
	if ast.IsPropertyAccessExpression(callee) {
		name = callee.Name().Text()
	} else {
		name = callee.Text()
	}
	switch name {
	case "consume", "struct", "ref":
	default:
		return ""
	}
	signature := c.getResolvedSignature(call, nil, CheckModeNormal)
	if signature == nil || signature.declaration == nil || !ast.IsFunctionDeclaration(signature.declaration) ||
		!c.isOwnRuntimeFile(ast.GetSourceFileOfNode(signature.declaration)) || signature.declaration.Name() == nil {
		return ""
	}
	switch declared := signature.declaration.Name().Text(); declared {
	case "consume", "struct", "ref":
		return declared
	}
	return ""
}

// The operations of the runtime's `atomic`, which are those of `Atomics` by the same names.
var ownAtomicOperations = []string{"load", "store", "add", "sub", "and", "or", "xor", "exchange", "compareExchange", "wait", "notify"}

// The atomic operation a call is: (nil, true) if it is not one, (the operation, true) if it acts on
// an i32 field of a shared struct named by a string literal, or on an element of a SharedArray<i32>,
// which the emit lowers to `Atomics`, and (the operation, false) if it acts on anything else, which is
// an error: there is nothing to lower it to.
func (c *Checker) getOwnAtomicCall(call *ast.Node) (*printer.OwnAtomicCall, bool) {
	if c.compilerOptions.OwnRuntime == "" || call == nil || !ast.IsCallExpression(call) {
		return nil, true
	}
	callee := ast.SkipParentheses(call.Expression())
	if !ast.IsPropertyAccessExpression(callee) || !slices.Contains(ownAtomicOperations, callee.Name().Text()) ||
		c.getOwnRuntimeExportName(c.getSymbolAtLocation(callee.Expression(), true /*ignoreErrors*/)) != "atomic" {
		return nil, true
	}
	atomic := &printer.OwnAtomicCall{Op: callee.Name().Text(), Field: -1}
	args := call.Arguments()
	if len(args) < 2 {
		return atomic, false
	}
	switch cell := c.getOwnCellType(c.getTypeOfExpression(args[0])); cell.Kind {
	case printer.OwnCellArrayOfI32:
		return atomic, true
	case printer.OwnCellStruct:
		if key := ast.SkipParentheses(args[1]); ast.IsStringLiteral(key) || ast.IsNoSubstitutionTemplateLiteral(key) {
			k := slices.Index(cell.Fields, key.Text())
			if cell.FieldKind(k) == printer.OwnFieldI32 {
				atomic.Field = k
				return atomic, true
			}
		}
	}
	return atomic, false
}

// The runtime's calls that block their thread, by declaring class and name, and what an async
// function calls instead ("" for none).
var ownBlockingCalls = map[string]string{
	"Channel.send": "sendAsync", "Channel.recv": "recvAsync", "ChannelCell.send": "sendAsync", "ChannelCell.recv": "recvAsync",
	"Thread.join": "joinAsync", "Barrier.wait": "waitAsync", "select": "selectAsync", "scope": "",
}

// A call of the runtime's that blocks its thread: its name, what an async function calls instead (""
// for none), and whether it is one.
func (c *Checker) getOwnBlockingCall(call *ast.Node) (string, string, bool) {
	if c.compilerOptions.OwnRuntime == "" || !ast.IsCallExpression(call) {
		return "", "", false
	}
	if atomic, _ := c.getOwnAtomicCall(call); atomic != nil {
		return "atomic." + atomic.Op, "", atomic.Op == "wait"
	}
	signature := c.getResolvedSignature(call, nil, CheckModeNormal)
	if signature == nil || signature.declaration == nil || !c.isOwnRuntimeFile(ast.GetSourceFileOfNode(signature.declaration)) {
		return "", "", false
	}
	decl := signature.declaration
	name := decl.Name()
	if name == nil || !ast.IsIdentifier(name) {
		return "", "", false
	}
	key := name.Text()
	if (ast.IsMethodDeclaration(decl) || ast.IsMethodSignatureDeclaration(decl)) && decl.Parent != nil && decl.Parent.Name() != nil {
		key = decl.Parent.Name().Text() + "." + key
	}
	instead, ok := ownBlockingCalls[key]
	return name.Text(), instead, ok
}

// Reports whether an expression is `consume(x)`.
func (c *Checker) isOwnConsumeCall(expr *ast.Node) bool {
	return ast.IsCallExpression(expr) && len(expr.Arguments()) == 1 && c.getOwnCallMarker(expr) == "consume"
}

// The runtime's interface or type of a given name, if it exports one.
func (c *Checker) getOwnRuntimeType(name string) *Type {
	if t, ok := c.ownLayout.runtimeTypes[name]; ok {
		return t
	}
	var result *Type
	c.isOwnRuntimeFile(nil) // computes the runtime files
	for _, file := range c.files {
		if !c.ownLayout.runtimeFiles[file.FileName()] || file.Symbol == nil {
			continue
		}
		if symbol := file.Symbol.Exports[name]; symbol != nil && symbol.Flags&(ast.SymbolFlagsInterface|ast.SymbolFlagsClass) != 0 {
			result = c.getDeclaredTypeOfSymbol(symbol)
			break
		}
	}
	if c.ownLayout.runtimeTypes == nil {
		c.ownLayout.runtimeTypes = map[string]*Type{}
	}
	c.ownLayout.runtimeTypes[name] = result
	return result
}

// Reports whether a type is a cell of the plain API: assignable to the runtime's `Cell` (Barrier,
// Slots, Channel, Thread, Scope, SharedArray, a struct's values), or a struct type or a field marker
// (immutable, `IntegerField` included). Its values are shared, with no region.
func (c *Checker) isOwnCellBranded(t *Type) bool {
	if c.compilerOptions.OwnRuntime == "" || t == nil || t.flags&(TypeFlagsAnyOrUnknown|TypeFlagsNever|TypeFlagsPrimitive) != 0 {
		return false
	}
	if t.flags&TypeFlagsObject != 0 {
		if target := t.Target(); target != nil && target.symbol != nil && (target.symbol.Name == "StructType" || target.symbol.Name == "Field" || target.symbol.Name == "IntegerField") &&
			c.getOwnRuntimeExportName(target.symbol) != "" {
			return true
		}
	}
	cell := c.getOwnRuntimeType("Cell")
	return cell != nil && t.flags&(TypeFlagsObject|TypeFlagsIntersection) != 0 && c.isTypeAssignableTo(t, cell)
}

// Shared structs declared with `struct({...})`

// A shared struct: its name, fields in slot order, and how each holds its value.
type ownStructInfo struct {
	decl   *ast.Node // the `shared struct` declaration, or the `struct(...)` call
	name   string
	fields []string
	kinds  []printer.OwnFieldKind
}

// The `struct(...)` calls of the program's source files, by the type of their structs' values
// (StructType<T>'s T).
func (c *Checker) getOwnStructRegistry() map[*Type]*ast.Node {
	if c.ownLayout.structs != nil {
		return c.ownLayout.structs
	}
	c.ownLayout.structs = map[*Type]*ast.Node{}
	for _, file := range c.files {
		if file.IsDeclarationFile {
			continue
		}
		var visit ast.Visitor
		visit = func(node *ast.Node) bool {
			if ast.IsCallExpression(node) && c.getOwnCallMarker(node) == "struct" {
				if t := c.getOwnStructTypeOfCall(node); t != nil {
					c.ownLayout.structs[t] = node
				}
			}
			return node.ForEachChild(visit)
		}
		file.AsNode().ForEachChild(visit)
	}
	return c.ownLayout.structs
}

// The type of the values of the struct a `struct(...)` call declares: T in StructType<T>.
func (c *Checker) getOwnStructTypeOfCall(call *ast.Node) *Type {
	t := c.getTypeOfExpression(call)
	if t == nil || t.objectFlags&ObjectFlagsReference == 0 {
		return nil
	}
	if args := c.getTypeArguments(t); len(args) > 0 {
		return args[0]
	}
	return nil
}

// The layout of the struct a `struct(...)` call declares: its fields in the order of the object
// literal, and their kinds by their types (see getOwnFieldKind).
func (c *Checker) getOwnStructOfCall(call *ast.Node) *ownStructInfo {
	if info, ok := c.ownLayout.structCalls[call]; ok {
		return info
	}
	if c.ownLayout.structCalls == nil {
		c.ownLayout.structCalls = map[*ast.Node]*ownStructInfo{}
	}
	var info *ownStructInfo
	if t := c.getOwnStructTypeOfCall(call); t != nil && len(call.Arguments()) >= 1 && ast.IsObjectLiteralExpression(ast.SkipParentheses(call.Arguments()[0])) {
		info = &ownStructInfo{decl: call, name: "Struct"}
		c.ownLayout.structCalls[call] = info // before its fields: a field may refer to the struct itself
		if parent := ast.WalkUpParenthesizedExpressions(call.Parent); parent != nil && ast.IsVariableDeclaration(parent) && ast.IsIdentifier(parent.Name()) {
			info.name = parent.Name().Text()
		}
		for _, property := range ast.SkipParentheses(call.Arguments()[0]).Properties() {
			name := property.Name()
			if name == nil || !(ast.IsPropertyAssignment(property) || ast.IsShorthandPropertyAssignment(property)) || !ast.IsPropertyNameLiteral(name) {
				continue // reported by checkOwnStructCall
			}
			field := name.Text()
			kind := printer.OwnFieldF64
			if symbol := c.getPropertyOfType(t, field); symbol != nil {
				kind = c.getOwnFieldKind(c.getTypeOfSymbol(symbol))
			}
			info.fields = append(info.fields, field)
			info.kinds = append(info.kinds, kind)
		}
	}
	c.ownLayout.structCalls[call] = info
	return info
}

// Checks a `struct(...)` call: its fields are given in an object literal, by name, and each is a
// number, a boolean, a string or a reference (a shared struct or a SharedArray, possibly null), as for
// `shared struct`.
func (c *Checker) checkOwnStructCall(call *ast.Node) {
	t := c.getOwnStructTypeOfCall(call)
	if t == nil || len(call.Arguments()) < 1 {
		return
	}
	literal := ast.SkipParentheses(call.Arguments()[0])
	if !ast.IsObjectLiteralExpression(literal) {
		c.error(call.Arguments()[0], diagnostics.A_shared_struct_field_must_be_a_number_a_boolean_a_string_a_shared_struct_a_SharedArray_or_a_Channel)
		return
	}
	for _, property := range literal.Properties() {
		name := property.Name()
		if name == nil || !(ast.IsPropertyAssignment(property) || ast.IsShorthandPropertyAssignment(property)) || !ast.IsPropertyNameLiteral(name) || ast.IsNumericLiteral(name) {
			c.error(property, diagnostics.A_shared_struct_field_must_be_a_number_a_boolean_a_string_a_shared_struct_a_SharedArray_or_a_Channel)
			continue
		}
		symbol := c.getPropertyOfType(t, name.Text())
		if symbol == nil {
			continue
		}
		if c.isOwnFieldType(c.getTypeOfSymbol(symbol)) {
			continue
		}
		c.error(property, diagnostics.A_shared_struct_field_must_be_a_number_a_boolean_a_string_a_shared_struct_a_SharedArray_or_a_Channel)
	}
}
