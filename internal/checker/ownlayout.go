package checker

import (
	"math"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/diagnostics"
	"github.com/microsoft/typescript-go/internal/jsnum"
	"github.com/microsoft/typescript-go/internal/printer"
)

// Typed layouts of the own/shared threads extension (only with `--ownRuntime`).
//
// A value whose static type is a shared struct or a SharedArray is, at run time, the byte address of
// its block in the shared heap of the runtime, and the emit turns field and element accesses into
// typed-array accesses (see tstransforms/ownlayout.go). The checker supplies the layouts and keeps
// that representation from leaking:
//
//   - A shared struct with fields f0..f(n-1) is a header word (written by the runtime) and n 8-byte
//     slots: field k is at byte offset 8 + 8*k. A field must be a number (stored as f64), a shared
//     struct or a SharedArray (stored as an address).
//   - A SharedArray<E> (the `SharedArray` interface declared in the module that `ownRuntime` names) is
//     a 16-byte header followed by its elements; E must be a number or a shared struct (stored inline,
//     header included).
//   - Where such a value flows to or from a place typed otherwise, the emit boxes or unboxes it (see
//     "Boxing" below).

type ownLayoutState struct {
	runtimeFiles    map[string]bool                     // the files `ownRuntime` resolves to
	conversions     map[*ast.Node]printer.OwnConversion // box and unbox conversions by expression
	conversionFiles map[*ast.SourceFile]bool            // files whose conversions are computed
	refs            ownRefCountState                    // reference counting (ownrefcount.go)
}

// The files that the `ownRuntime` specifier resolves to (from any file that imports it).
func (c *Checker) isOwnRuntimeFile(file *ast.SourceFile) bool {
	if c.ownLayout.runtimeFiles == nil {
		c.ownLayout.runtimeFiles = map[string]bool{}
		if specifier := c.compilerOptions.OwnRuntime; specifier != "" {
			for _, resolutions := range c.program.GetResolvedModules() {
				for key, resolved := range resolutions {
					if key.Name == specifier && resolved != nil && resolved.IsResolved() {
						c.ownLayout.runtimeFiles[resolved.ResolvedFileName] = true
					}
				}
			}
		}
	}
	return file != nil && c.ownLayout.runtimeFiles[file.FileName()]
}

// The shared struct whose instances have type t, if any (not the struct's constructor).
func (c *Checker) getOwnStructOfType(t *Type) *ast.Symbol {
	if t == nil || t.flags&TypeFlagsObject == 0 || t.symbol == nil || !core.Some(t.symbol.Declarations, ast.IsSharedStructDeclaration) {
		return nil
	}
	if c.getDeclaredTypeOfSymbol(t.symbol) != t {
		return nil
	}
	return t.symbol
}

// The element type of a SharedArray type, if t is one.
func (c *Checker) getOwnSharedArrayElementType(t *Type) *Type {
	if c.compilerOptions.OwnRuntime == "" || t == nil || t.objectFlags&ObjectFlagsReference == 0 {
		return nil
	}
	target := t.Target()
	if target == nil || target.symbol == nil || target.symbol.Name != "SharedArray" {
		return nil
	}
	if !core.Some(target.symbol.Declarations, func(d *ast.Node) bool {
		return ast.IsInterfaceDeclaration(d) && c.isOwnRuntimeFile(ast.GetSourceFileOfNode(d))
	}) {
		return nil
	}
	if args := c.getTypeArguments(t); len(args) > 0 {
		return args[0]
	}
	return nil
}

// The non-nullish members of a type.
func (c *Checker) ownNonNullishMembers(t *Type) []*Type {
	if t.flags&TypeFlagsUnion != 0 {
		return core.Filter(t.Types(), func(m *Type) bool { return m.flags&TypeFlagsNullable == 0 })
	}
	if t.flags&TypeFlagsNullable != 0 {
		return nil
	}
	return []*Type{t}
}

// Classifies a type for the layout emit: a shared struct, a SharedArray of numbers or a SharedArray of
// a shared struct (looking through null and undefined).
func (c *Checker) getOwnCellType(t *Type) printer.OwnCellType {
	if t == nil || c.compilerOptions.OwnRuntime == "" {
		return printer.OwnCellType{}
	}
	members := c.ownNonNullishMembers(t)
	if len(members) != 1 {
		return printer.OwnCellType{}
	}
	t = members[0]
	if s := c.getOwnStructOfType(t); s != nil {
		fields, refs := c.ownStructLayout(s)
		return printer.OwnCellType{Kind: printer.OwnCellStruct, Fields: fields, Refs: refs}
	}
	if element := c.getOwnSharedArrayElementType(t); element != nil {
		if element.flags&TypeFlagsNumber != 0 {
			return printer.OwnCellType{Kind: printer.OwnCellArrayOfNumber}
		}
		if s := c.getOwnStructOfType(element); s != nil {
			fields, refs := c.ownStructLayout(s)
			return printer.OwnCellType{Kind: printer.OwnCellArrayOfStruct, Fields: fields, Refs: refs}
		}
	}
	return printer.OwnCellType{}
}

// The names of a shared struct's fields, in slot order, and which of them are reference fields (a
// shared struct or a SharedArray, possibly with null or undefined) rather than numbers.
func (c *Checker) ownStructLayout(s *ast.Symbol) (fields []string, refs []bool) {
	decl := core.Find(s.Declarations, ast.IsSharedStructDeclaration)
	for _, member := range decl.Members() {
		if ast.IsPropertySignatureDeclaration(member) && member.Name() != nil {
			fields = append(fields, member.Name().Text())
			isRef := false
			if symbol := c.getSymbolOfDeclaration(member); symbol != nil {
				isRef = c.isOwnCellType(c.getTypeOfSymbol(symbol))
			}
			refs = append(refs, isRef)
		}
	}
	return fields, refs
}

// Reports whether a value of type t is a shared struct or a SharedArray (possibly with null or
// undefined), which is represented by an address.
func (c *Checker) isOwnCellType(t *Type) bool {
	if t == nil || c.compilerOptions.OwnRuntime == "" {
		return false
	}
	members := c.ownNonNullishMembers(t)
	return len(members) > 0 && core.Every(members, func(m *Type) bool {
		return c.getOwnStructOfType(m) != nil || c.getOwnSharedArrayElementType(m) != nil
	})
}

// The layout type of an expression (or of the instances of a shared struct declaration), for the
// emit resolver.
func (c *Checker) getOwnCellTypeOfExpression(expr *ast.Node) printer.OwnCellType {
	if c.compilerOptions.OwnRuntime == "" {
		return printer.OwnCellType{}
	}
	if ast.IsSharedStructDeclaration(expr) {
		if symbol := c.getSymbolOfDeclaration(expr); symbol != nil {
			return c.getOwnCellType(c.getDeclaredTypeOfSymbol(symbol))
		}
		return printer.OwnCellType{}
	}
	if !ast.IsExpression(expr) {
		return printer.OwnCellType{}
	}
	return c.getOwnCellType(c.getTypeOfExpression(expr))
}

// The constructor of a shared struct: `new S()` makes a zeroed instance.
func (c *Checker) getTypeOfSharedStructValue(symbol *ast.Symbol) *Type {
	instance := c.getDeclaredTypeOfSymbol(symbol)
	signature := c.newSignature(SignatureFlagsNone, nil /*declaration*/, nil /*typeParameters*/, nil /*thisParameter*/, nil /*parameters*/, instance, nil /*resolvedTypePredicate*/, 0 /*minArgumentCount*/)
	return c.newAnonymousType(symbol, ast.SymbolTable{}, nil, []*Signature{signature}, nil)
}

// Checks the fields of a shared struct for the layout: numbers, and reference fields (a shared struct
// or a SharedArray, possibly with null or undefined).
func (c *Checker) checkSharedStructLayout(node *ast.Node) {
	if c.compilerOptions.OwnRuntime == "" || node.Flags&ast.NodeFlagsAmbient != 0 {
		return
	}
	for _, member := range node.Members() {
		if !ast.IsPropertySignatureDeclaration(member) {
			continue
		}
		t := c.getTypeOfSymbol(c.getSymbolOfDeclaration(member))
		if t.flags&TypeFlagsNumber != 0 && t.flags&TypeFlagsUnion == 0 || len(c.ownNonNullishMembers(t)) == 1 && c.isOwnCellType(t) {
			continue
		}
		location := member.Type()
		if location == nil {
			location = member.Name()
		}
		c.error(location, diagnostics.A_shared_struct_field_must_be_a_number_a_shared_struct_or_a_SharedArray)
	}
}

// Checks the element type of a SharedArray type reference: numbers or a shared struct.
func (c *Checker) checkSharedArrayElementType(node *ast.Node, t *Type) {
	if c.compilerOptions.OwnRuntime == "" || node.Flags&ast.NodeFlagsAmbient != 0 {
		return
	}
	element := c.getOwnSharedArrayElementType(t)
	if element == nil || element.flags&TypeFlagsNumber != 0 && element.flags&TypeFlagsUnion == 0 || c.getOwnStructOfType(element) != nil {
		return
	}
	c.error(node, diagnostics.The_elements_of_a_SharedArray_must_be_numbers_or_a_shared_struct)
}

// Boxing
//
// Code typed with shared structs and SharedArrays works on raw addresses. Where such a value flows
// into a place typed otherwise (`unknown`, `any`, an interface, a union with other types, a string
// context, `typeof`, ...), the emit boxes it with `__box(E)`, which returns the thread's canonical
// handle object for the address. Where a value that is not typed as a struct or SharedArray flows
// into a place typed as one (`x as Body`, an `any` argument to a `Body` parameter, a variable of
// type `unknown` narrowed to `Body`), the emit unboxes it with `__unbox(E)`. The checker decides
// which expressions are converted (GetOwnConversion in the emit resolver):
//
//   - The "source" representation of an expression is raw if its type is a struct or SharedArray,
//     except for a variable or property declared with another type and narrowed to one, which holds
//     a handle.
//   - The "target" representation is raw if the type the context expects is a struct or SharedArray
//     (or a union of those with null and undefined) and a handle otherwise. Targets are the parameter
//     types of the resolved (instantiated) signature, declared variable, property and return types,
//     contextual types of literal elements, asserted types, and places that need an object (strings,
//     `typeof`, `in`, spread, equality with a non-struct value, properties a struct does not declare).
//
// Generic code is decided by the instantiated signature, so `spawn(f, ...args: sending A)` passes
// struct arguments raw; inside a generic function such a value is a `T` and is not boxed.

// Reports whether an expression holds a handle although its type is a struct or SharedArray: a
// variable or property declared with another type and narrowed to one.
func (c *Checker) isOwnNarrowedReference(expr *ast.Node) bool {
	var symbol *ast.Symbol
	switch {
	case ast.IsIdentifier(expr):
		symbol = c.getResolvedSymbolOrNil(expr)
		if symbol == nil {
			symbol = c.getResolvedSymbol(expr)
		}
		if symbol != nil && symbol != c.unknownSymbol {
			symbol = c.getExportSymbolOfValueSymbolIfExported(symbol)
		}
	case ast.IsPropertyAccessExpression(expr):
		symbol = c.getSymbolAtLocation(expr.Name(), true /*ignoreErrors*/)
	}
	if symbol == nil || symbol == c.unknownSymbol || symbol.Flags&(ast.SymbolFlagsVariable|ast.SymbolFlagsProperty) == 0 {
		return false
	}
	return !c.isOwnCellType(c.getTypeOfSymbol(symbol))
}

// Records the conversion of an expression flowing into a place whose representation is raw
// (targetRaw) or a handle. Conditional and logical expressions pass the target on to their operands.
func (c *Checker) setOwnTarget(expr *ast.Node, targetRaw bool) {
	if expr == nil {
		return
	}
	switch expr.Kind {
	case ast.KindParenthesizedExpression:
		c.setOwnTarget(expr.Expression(), targetRaw)
		return
	case ast.KindConditionalExpression:
		c.setOwnTarget(expr.AsConditionalExpression().WhenTrue, targetRaw)
		c.setOwnTarget(expr.AsConditionalExpression().WhenFalse, targetRaw)
		return
	case ast.KindBinaryExpression:
		switch expr.AsBinaryExpression().OperatorToken.Kind {
		case ast.KindAmpersandAmpersandToken, ast.KindBarBarToken, ast.KindQuestionQuestionToken:
			c.setOwnTarget(expr.AsBinaryExpression().Left, targetRaw)
			c.setOwnTarget(expr.AsBinaryExpression().Right, targetRaw)
			return
		case ast.KindCommaToken:
			c.setOwnTarget(expr.AsBinaryExpression().Right, targetRaw)
			return
		}
	}
	if !ast.IsExpression(expr) {
		return
	}
	t := c.getTypeOfExpression(expr)
	sourceCell := c.isOwnCellType(t)
	sourceRaw := sourceCell && !c.isOwnNarrowedReference(expr)
	switch {
	case targetRaw && !sourceRaw && len(c.ownNonNullishMembers(t)) > 0:
		// A handle (or any value that is not typed as a struct) into a struct-typed place.
		c.ownLayout.conversions[expr] = printer.OwnConversionUnbox
	case targetRaw && t.flags&TypeFlagsNullable != 0 && len(c.ownNonNullishMembers(t)) == 0:
		// null and undefined are 0 where an address is expected.
		c.ownLayout.conversions[expr] = printer.OwnConversionZero
	case !targetRaw && sourceRaw:
		c.ownLayout.conversions[expr] = printer.OwnConversionBox
	default:
		delete(c.ownLayout.conversions, expr)
	}
}

// Records the conversion of an expression flowing into a place of the given type.
func (c *Checker) setOwnTargetType(expr *ast.Node, target *Type) {
	if expr == nil || target == nil {
		return
	}
	c.setOwnTarget(expr, c.isOwnCellType(target))
}

// The conversion of an expression for the emit (with `--ownRuntime`).
func (c *Checker) getOwnConversion(expr *ast.Node) printer.OwnConversion {
	if c.compilerOptions.OwnRuntime == "" {
		return printer.OwnConversionNone
	}
	file := ast.GetSourceFileOfNode(expr)
	if file == nil {
		return printer.OwnConversionNone
	}
	if !c.ownLayout.conversionFiles[file] {
		c.computeOwnConversions(file, false /*report*/)
	}
	return c.ownLayout.conversions[expr]
}

// Computes the box and unbox conversions of a file and, when reporting, the layout errors that remain
// (assigning an inline struct element, optional chaining, destructuring a struct parameter).
func (c *Checker) computeOwnConversions(file *ast.SourceFile, report bool) {
	if c.ownLayout.conversions == nil {
		c.ownLayout.conversions = map[*ast.Node]printer.OwnConversion{}
		c.ownLayout.conversionFiles = map[*ast.SourceFile]bool{}
	}
	c.ownLayout.conversionFiles[file] = true
	var visit ast.Visitor
	visit = func(node *ast.Node) bool {
		if ast.IsTypeNode(node) {
			return false
		}
		c.computeOwnConversion(node, report)
		node.ForEachChild(visit)
		return false
	}
	file.AsNode().ForEachChild(visit)
}

func (c *Checker) computeOwnConversion(node *ast.Node, report bool) {
	switch node.Kind {
	case ast.KindCallExpression, ast.KindNewExpression:
		if report && ast.IsNewExpression(node) {
			c.checkSharedArrayElementType(node, c.getTypeOfExpression(node))
		}
		if node.ArgumentList() == nil || len(node.Arguments()) == 0 || ast.IsImportCall(node) {
			return
		}
		signature := c.getResolvedSignature(node, nil, CheckModeNormal)
		for i, arg := range node.Arguments() {
			if ast.IsSpreadElement(arg) {
				c.setOwnTarget(arg.Expression(), false)
				continue
			}
			if signature == nil || signature == c.unknownSignature || signature == c.anySignature || signature == c.resolvingSignature {
				c.setOwnTarget(arg, false)
				continue
			}
			c.setOwnTargetType(arg, c.tryGetTypeAtPosition(signature, i))
		}
	case ast.KindVariableDeclaration, ast.KindPropertyDeclaration, ast.KindParameter:
		init := node.Initializer()
		if ast.IsBindingPattern(node.Name()) {
			if init != nil {
				c.setOwnTarget(init, false) // destructured through the handle
			} else if report && (ast.IsParameterDeclaration(node) || ast.IsVariableDeclaration(node) && ast.IsForOfStatement(node.Parent.Parent)) {
				// A parameter or loop variable cannot be boxed before it is destructured.
				if t := c.getTypeForVariableLikeDeclaration(node, false /*includeOptionality*/, CheckModeNormal); c.isOwnCellType(t) {
					c.error(node.Name(), diagnostics.X_0_cannot_be_destructured_here_Colon_its_value_is_an_address_in_the_shared_heap, c.typeToString(t, nil))
				}
			}
			return
		}
		if init != nil {
			if symbol := c.getSymbolOfDeclaration(node); symbol != nil {
				c.setOwnTargetType(init, c.getTypeOfSymbol(symbol))
			}
		}
	case ast.KindBinaryExpression:
		c.computeOwnBinaryConversion(node, report)
	case ast.KindReturnStatement:
		if expr := node.Expression(); expr != nil {
			c.setOwnTargetType(expr, c.getOwnReturnTargetType(ast.GetContainingFunction(node)))
		}
	case ast.KindArrowFunction:
		if body := node.Body(); body != nil && !ast.IsBlock(body) {
			c.setOwnTargetType(body, c.getOwnReturnTargetType(node))
		}
	case ast.KindArrayLiteralExpression:
		if ast.IsAssignmentTarget(node) {
			return
		}
		literalType := c.getTypeOfExpression(node)
		for i, element := range node.Elements() {
			if ast.IsSpreadElement(element) {
				c.setOwnTarget(element.Expression(), false)
				continue
			}
			if ast.IsOmittedExpression(element) {
				continue
			}
			target := c.getContextualType(element, ContextFlagsNone)
			if target == nil {
				if isTupleType(literalType) {
					target = c.getTupleElementType(literalType, i)
				} else {
					target = c.getIndexTypeOfType(literalType, c.numberType)
				}
			}
			c.setOwnTargetType(element, target)
		}
	case ast.KindPropertyAssignment:
		if !ast.IsAssignmentTarget(node.Parent) {
			c.setOwnTargetType(node.Initializer(), c.getContextualType(node.Initializer(), ContextFlagsNone))
		}
	case ast.KindShorthandPropertyAssignment:
		if !ast.IsAssignmentTarget(node.Parent) {
			c.setOwnTargetType(node.Name(), c.getContextualType(node.Name(), ContextFlagsNone))
		}
	case ast.KindSpreadAssignment, ast.KindTemplateSpan, ast.KindTypeOfExpression:
		c.setOwnTarget(node.Expression(), false)
	case ast.KindAsExpression, ast.KindTypeAssertionExpression:
		c.setOwnTargetType(node.Expression(), c.getTypeFromTypeNode(node.Type()))
	case ast.KindForOfStatement, ast.KindForInStatement:
		c.setOwnTarget(node.Expression(), false)
	case ast.KindCaseClause:
		// `case null:` against an address compares with 0.
		if switchExpression := node.Parent.Parent.Expression(); c.isOwnCellExpression(switchExpression) && !c.isOwnNarrowedReference(switchExpression) {
			if t := c.getTypeOfExpression(node.Expression()); t.flags&TypeFlagsNullable != 0 && len(c.ownNonNullishMembers(t)) == 0 {
				c.setOwnTarget(node.Expression(), true)
			}
		}
	case ast.KindPropertyAccessExpression:
		object := node.Expression()
		t := c.getTypeOfExpression(object)
		if !c.isOwnCellType(t) {
			return
		}
		if ast.IsOptionalChain(node) {
			if report {
				c.error(node, diagnostics.Optional_chaining_is_not_supported_on_0_Colon_its_value_is_an_address_in_the_shared_heap, c.typeToString(t, nil))
			}
			return
		}
		name := node.Name().Text()
		cell := c.getOwnCellType(t)
		switch cell.Kind {
		case printer.OwnCellStruct:
			// A field is read through the layout; anything else through the handle.
			c.setOwnTarget(object, core.Some(cell.Fields, func(f string) bool { return f == name }))
		case printer.OwnCellArrayOfNumber, printer.OwnCellArrayOfStruct:
			c.setOwnTarget(object, name == "length")
		default:
			c.setOwnTarget(object, false)
		}
	case ast.KindElementAccessExpression:
		object := node.Expression()
		t := c.getTypeOfExpression(object)
		if !c.isOwnCellType(t) {
			return
		}
		if ast.IsOptionalChain(node) {
			if report {
				c.error(node, diagnostics.Optional_chaining_is_not_supported_on_0_Colon_its_value_is_an_address_in_the_shared_heap, c.typeToString(t, nil))
			}
			return
		}
		kind := c.getOwnCellType(t).Kind
		c.setOwnTarget(object, kind == printer.OwnCellArrayOfNumber || kind == printer.OwnCellArrayOfStruct)
	}
}

func (c *Checker) computeOwnBinaryConversion(node *ast.Node, report bool) {
	n := node.AsBinaryExpression()
	switch op := n.OperatorToken.Kind; op {
	case ast.KindEqualsToken:
		if ast.IsArrayLiteralExpression(n.Left) || ast.IsObjectLiteralExpression(n.Left) {
			c.setOwnTarget(n.Right, false) // destructured through the handle
			return
		}
		if report && ast.IsElementAccessExpression(n.Left) && c.getOwnCellTypeOfExpression(n.Left.Expression()).Kind == printer.OwnCellArrayOfStruct {
			c.error(n.Left, diagnostics.The_elements_of_a_SharedArray_of_shared_structs_are_stored_inline_Colon_assign_their_fields_instead)
			return
		}
		c.setOwnTargetType(n.Right, c.getOwnAssignmentTargetType(n.Left))
	case ast.KindBarBarEqualsToken, ast.KindAmpersandAmpersandEqualsToken, ast.KindQuestionQuestionEqualsToken:
		c.setOwnTargetType(n.Right, c.getOwnAssignmentTargetType(n.Left))
	case ast.KindPlusToken, ast.KindPlusEqualsToken:
		// String concatenation (arithmetic on a struct is already a type error).
		if c.isOwnCellExpression(n.Left) && op == ast.KindPlusToken {
			c.setOwnTarget(n.Left, false)
		}
		if c.isOwnCellExpression(n.Right) {
			c.setOwnTarget(n.Right, false)
		}
	case ast.KindEqualsEqualsEqualsToken, ast.KindExclamationEqualsEqualsToken, ast.KindEqualsEqualsToken, ast.KindExclamationEqualsToken:
		leftType, rightType := c.getTypeOfExpression(n.Left), c.getTypeOfExpression(n.Right)
		leftCell, rightCell := c.isOwnCellType(leftType), c.isOwnCellType(rightType)
		switch {
		case leftCell && rightCell:
			c.setOwnTarget(n.Left, true)
			c.setOwnTarget(n.Right, true)
		case leftCell:
			// Compare addresses with null and undefined (as 0), handles with anything else.
			rightNullish := rightType.flags&TypeFlagsNullable != 0 && len(c.ownNonNullishMembers(rightType)) == 0
			c.setOwnTarget(n.Left, rightNullish)
			if rightNullish {
				c.setOwnTarget(n.Right, true)
			}
		case rightCell:
			leftNullish := leftType.flags&TypeFlagsNullable != 0 && len(c.ownNonNullishMembers(leftType)) == 0
			c.setOwnTarget(n.Right, leftNullish)
			if leftNullish {
				c.setOwnTarget(n.Left, true)
			}
		}
	case ast.KindInKeyword:
		c.setOwnTarget(n.Right, false)
	case ast.KindInstanceOfKeyword:
		c.setOwnTarget(n.Left, false)
	}
}

// The type a function's returned values flow into: its declared, contextual or inferred return type.
func (c *Checker) getOwnReturnTargetType(fn *ast.Node) *Type {
	if fn == nil || !ast.IsFunctionLike(fn) {
		return nil
	}
	flags := ast.GetFunctionFlags(fn)
	if flags&ast.FunctionFlagsGenerator != 0 {
		return nil
	}
	if fn.Type() != nil {
		return c.unwrapReturnType(c.getTypeFromTypeNode(fn.Type()), flags)
	}
	if signature := c.getContextualSignatureForFunctionLikeDeclaration(fn); signature != nil {
		return c.unwrapReturnType(c.getReturnTypeOfSignature(signature), flags)
	}
	return c.unwrapReturnType(c.getReturnTypeOfSignature(c.getSignatureFromDeclaration(fn)), flags)
}

// The declared type of an assignment target.
func (c *Checker) getOwnAssignmentTargetType(target *ast.Node) *Type {
	target = ast.SkipParentheses(target)
	if ast.IsIdentifier(target) {
		if symbol := c.getResolvedSymbol(target); symbol != nil && symbol != c.unknownSymbol {
			return c.getTypeOfSymbol(c.getExportSymbolOfValueSymbolIfExported(symbol))
		}
	}
	if ast.IsPropertyAccessExpression(target) {
		if symbol := c.getSymbolAtLocation(target.Name(), true /*ignoreErrors*/); symbol != nil {
			return c.getTypeOfSymbol(symbol)
		}
	}
	return c.getTypeOfExpression(target)
}

func (c *Checker) isOwnCellExpression(expr *ast.Node) bool {
	return expr != nil && ast.IsExpression(expr) && c.isOwnCellType(c.getTypeOfExpression(expr))
}

// Bounds-check elimination
//
// The emit may drop the bounds check of a SharedArray element access `A[I]` (and access the element
// directly) when the checker proves that I is an integer in [0, A.length). The proof is purely
// syntactic and rests on exactly these conditions:
//
//  1. The access is `A[I]` with A and I plain identifiers, A's type a SharedArray, and no `?.`.
//  2. A is a `const`, or a parameter or `let`/`var` variable that is never assigned (anywhere in the
//     function or file that declares it, nested functions included). A SharedArray's length never
//     changes, so A.length is the same at every read.
//  3. The access is inside the body (not the head) of a `for (let I = <init>; I < <bound>; <update>)`
//     loop that declares I, with no function boundary between the loop and the access, where
//     - <init> has a non-negative integer literal type below 2^31 (a literal, or a `const` of one),
//     - <bound> is `A.length`, or an identifier declared `const n = A.length`, for the same A,
//     - <update> is `I++`, `++I` or `I += 1`,
//     - and I is assigned nowhere else in the loop's head or body, nested functions included.
//
// Then I starts at a non-negative integer, only grows by one, and the body only runs after `I <
// A.length` held for the current value of I. Anything else keeps the check.

// Reports whether the emit may access the element of `A[I]` without a bounds check.
func (c *Checker) isOwnElementAccessInBounds(access *ast.Node) bool {
	if c.compilerOptions.OwnRuntime == "" || !ast.IsElementAccessExpression(access) || ast.IsOptionalChain(access) {
		return false
	}
	array := ast.SkipParentheses(access.Expression())
	index := ast.SkipParentheses(access.AsElementAccessExpression().ArgumentExpression)
	if !ast.IsIdentifier(array) || !ast.IsIdentifier(index) {
		return false
	}
	arraySymbol, indexSymbol := c.getResolvedSymbol(array), c.getResolvedSymbol(index)
	if arraySymbol == c.unknownSymbol || indexSymbol == c.unknownSymbol || !c.isOwnUnassignedVariable(arraySymbol) {
		return false
	}
	if c.getOwnSharedArrayElementType(c.getTypeOfSymbol(arraySymbol)) == nil {
		return false
	}
	for node := access.Parent; node != nil; node = node.Parent {
		if ast.IsFunctionLike(node) || ast.IsClassStaticBlockDeclaration(node) || ast.IsSourceFile(node) {
			return false
		}
		if ast.IsForStatement(node) && ownIsWithin(access, node.AsForStatement().Statement) && c.isOwnCountingLoop(node, indexSymbol, arraySymbol) {
			return true
		}
	}
	return false
}

// A `const`, or a parameter or variable that is never assigned.
func (c *Checker) isOwnUnassignedVariable(symbol *ast.Symbol) bool {
	decl := symbol.ValueDeclaration
	if decl == nil || symbol.Flags&ast.SymbolFlagsAlias != 0 {
		return false
	}
	switch {
	case ast.IsVariableDeclaration(decl) && ast.GetCombinedNodeFlags(decl)&ast.NodeFlagsConstant != 0:
		return true
	case ast.IsVariableDeclaration(decl) || ast.IsParameterDeclaration(decl):
		return c.isParameterOrMutableLocalVariable(symbol) && !c.isSymbolAssigned(symbol)
	}
	return false
}

// `for (let I = <init>; I < <bound>; <update>)` as described above.
func (c *Checker) isOwnCountingLoop(loop *ast.Node, indexSymbol *ast.Symbol, arraySymbol *ast.Symbol) bool {
	n := loop.AsForStatement()
	if n.Initializer == nil || !ast.IsVariableDeclarationList(n.Initializer) || n.Initializer.Flags&ast.NodeFlagsLet == 0 {
		return false
	}
	var declaration *ast.Node
	for _, decl := range n.Initializer.AsVariableDeclarationList().Declarations.Nodes {
		if ast.IsIdentifier(decl.Name()) && c.getSymbolOfDeclaration(decl) == indexSymbol {
			declaration = decl
		}
	}
	if declaration == nil || declaration.Initializer() == nil || !c.isOwnNonNegativeIntegerLiteral(declaration.Initializer()) {
		return false
	}
	// I < <bound>
	if n.Condition == nil || n.Incrementor == nil {
		return false
	}
	condition := ast.SkipParentheses(n.Condition)
	if !ast.IsBinaryExpression(condition) || condition.AsBinaryExpression().OperatorToken.Kind != ast.KindLessThanToken ||
		!c.isOwnReferenceTo(condition.AsBinaryExpression().Left, indexSymbol) || !c.isOwnLengthOf(condition.AsBinaryExpression().Right, arraySymbol) {
		return false
	}
	// I++, ++I or I += 1
	update := ast.SkipParentheses(n.Incrementor)
	switch {
	case ast.IsPrefixUnaryExpression(update):
		if update.AsPrefixUnaryExpression().Operator != ast.KindPlusPlusToken || !c.isOwnReferenceTo(update.AsPrefixUnaryExpression().Operand, indexSymbol) {
			return false
		}
	case ast.IsPostfixUnaryExpression(update):
		if update.AsPostfixUnaryExpression().Operator != ast.KindPlusPlusToken || !c.isOwnReferenceTo(update.AsPostfixUnaryExpression().Operand, indexSymbol) {
			return false
		}
	case ast.IsBinaryExpression(update):
		b := update.AsBinaryExpression()
		if b.OperatorToken.Kind != ast.KindPlusEqualsToken || !c.isOwnReferenceTo(b.Left, indexSymbol) ||
			!ast.IsNumericLiteral(ast.SkipParentheses(b.Right)) || ast.SkipParentheses(b.Right).Text() != "1" {
			return false
		}
	default:
		return false
	}
	// No other writes to I in the head or the body.
	return !c.isOwnAssignedIn(n.Initializer, indexSymbol) && !c.isOwnAssignedIn(n.Condition, indexSymbol) && !c.isOwnAssignedIn(n.Statement, indexSymbol)
}

func (c *Checker) isOwnReferenceTo(expr *ast.Node, symbol *ast.Symbol) bool {
	expr = ast.SkipParentheses(expr)
	return ast.IsIdentifier(expr) && c.getResolvedSymbol(expr) == symbol
}

// `A.length`, or an identifier declared `const n = A.length`.
func (c *Checker) isOwnLengthOf(expr *ast.Node, arraySymbol *ast.Symbol) bool {
	expr = ast.SkipParentheses(expr)
	if ast.IsIdentifier(expr) {
		symbol := c.getResolvedSymbol(expr)
		decl := symbol.ValueDeclaration
		if symbol == c.unknownSymbol || decl == nil || !ast.IsVariableDeclaration(decl) || ast.GetCombinedNodeFlags(decl)&ast.NodeFlagsConstant == 0 || decl.Initializer() == nil {
			return false
		}
		expr = ast.SkipParentheses(decl.Initializer())
	}
	return ast.IsPropertyAccessExpression(expr) && expr.Name().Text() == "length" && c.isOwnReferenceTo(expr.Expression(), arraySymbol)
}

// A non-negative integer literal type below 2^31: a literal, or a `const` initialized with one.
func (c *Checker) isOwnNonNegativeIntegerLiteral(expr *ast.Node) bool {
	t := c.getTypeOfExpression(expr)
	if t.flags&TypeFlagsNumberLiteral == 0 {
		return false
	}
	value, ok := t.AsLiteralType().Value().(jsnum.Number)
	return ok && value >= 0 && value < 1<<31 && value == jsnum.Number(math.Trunc(float64(value)))
}

// Reports whether a symbol is assigned (or updated) anywhere within node, nested functions included.
func (c *Checker) isOwnAssignedIn(node *ast.Node, symbol *ast.Symbol) bool {
	if node == nil {
		return false
	}
	found := false
	var visit ast.Visitor
	visit = func(n *ast.Node) bool {
		if found || ast.IsTypeNode(n) {
			return found
		}
		if ast.IsIdentifier(n) {
			if getAssignmentTargetKind(n) != AssignmentKindNone && c.getResolvedSymbol(n) == symbol {
				found = true
			}
			return found
		}
		return n.ForEachChild(visit)
	}
	if ast.IsIdentifier(node) {
		visit(node)
	} else {
		node.ForEachChild(visit)
	}
	return found
}
