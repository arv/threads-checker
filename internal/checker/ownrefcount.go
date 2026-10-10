package checker

import (
	"slices"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/diagnostics"
	"github.com/microsoft/typescript-go/internal/printer"
)

// Reference counting of the own/shared threads extension (only with `--ownRuntime`; the design is
// REFCOUNT.md in porffor's threads/escape-proto).
//
// A shared struct or SharedArray is freed when nothing refers to it. Its block counts the references
// stored in the shared heap (reference fields) and the references threads hold; the emit (see
// tstransforms/ownlayout.go) adds the increments and decrements, and the checker decides where:
//
//   - Every raw address expression is owned (+1: `new`, a call to a function that returns a counted
//     value), a stable borrow (a parameter, a local, an element of a stable SharedArray, `__unbox`) or
//     an unstable borrow (a reference field read, valid until the thread's next quiescent point).
//     getOwnCount classifies an expression.
//   - A local is a borrow when it is never reassigned and is initialized from a stable borrow that
//     cannot change while the local lives (holdable: the borrowed variable is never reassigned either);
//     every other local is owned and released when its block ends. A parameter borrows unless it is
//     reassigned, has a default value, or belongs to an async function or a generator (which outlive
//     their caller's borrow). getOwnLocal classifies a variable or parameter.
//   - A function returns +1. A call is owned only when its signature's own (uninstantiated) return type
//     is counted and it is declared in a source file: a generic or ambient function does not follow
//     the convention, so its result is an unstable borrow (retained where it is kept: at worst a leak).
//   - A closure holds what it captures (`__hold`), so a captured counted variable cannot be reassigned.
//     A method or a class cannot hold what it captures, so it cannot capture a counted local.
//   - A loop that reads or stores a reference field ends each iteration at a quiescent point.
//   - A raw address cannot be stored where counting cannot see it: in an ordinary array or object, a
//     destructured binding, or a for-of variable.

type ownRefCountState struct {
	counts    map[*ast.Node]printer.OwnCount
	locals    map[*ast.Symbol]printer.OwnLocal
	assigned  map[*ast.Symbol]bool
	captures  map[*ast.SourceFile]*ownCaptureInfo
	quiescent map[*ast.Node]bool
}

// The counted variables that functions capture, for a file.
type ownCaptureInfo struct {
	captured map[*ast.Symbol]bool      // referenced from a function other than the declaring one (any function, for a module's variable)
	captures map[*ast.Node][]*ast.Node // per function, arrow function or function declaration: the declarations of the counted locals of other functions it references
	members  []*ast.Node               // references to counted locals of another function from a method or a class (an error)
}

// How the value of an expression of the original tree is counted where it flows: after its box,
// unbox or null conversion.
func (c *Checker) getOwnCount(expr *ast.Node) printer.OwnCount {
	if c.compilerOptions.OwnRuntime == "" || expr == nil || !isOwnValueExpression(expr) {
		return printer.OwnCountNone
	}
	switch c.getOwnConversion(expr) {
	case printer.OwnConversionUnbox:
		return printer.OwnCountStable // `__unbox(E)`
	case printer.OwnConversionBox:
		return printer.OwnCountNone // a handle, which holds its own count
	case printer.OwnConversionZero:
		return printer.OwnCountNull
	}
	return c.getOwnRawCount(expr)
}

// How the value of an expression of the original tree is counted before its conversion (for a boxed
// expression, the address that `__box` takes; for an unboxed one, nothing).
func (c *Checker) getOwnRawCount(expr *ast.Node) printer.OwnCount {
	if c.compilerOptions.OwnRuntime == "" || expr == nil || !isOwnValueExpression(expr) {
		return printer.OwnCountNone
	}
	if count, ok := c.ownLayout.refs.counts[expr]; ok {
		return count
	}
	if c.ownLayout.refs.counts == nil {
		c.ownLayout.refs.counts = map[*ast.Node]printer.OwnCount{}
	}
	c.ownLayout.refs.counts[expr] = printer.OwnCountUnstable // provisional
	count := c.computeOwnRawCount(expr)
	c.ownLayout.refs.counts[expr] = count
	return count
}

func (c *Checker) computeOwnRawCount(expr *ast.Node) printer.OwnCount {
	if ast.IsParenthesizedExpression(expr) {
		return c.getOwnCount(expr.Expression())
	}
	t := c.getTypeOfExpression(expr)
	if !c.isOwnCellType(t) {
		if t.flags&TypeFlagsNullable != 0 && len(c.ownNonNullishMembers(t)) == 0 {
			return printer.OwnCountNull
		}
		return printer.OwnCountNone
	}
	if c.isOwnNarrowedReference(expr) {
		return printer.OwnCountNone // a handle
	}
	switch expr.Kind {
	case ast.KindIdentifier:
		if symbol := c.ownReferencedVariable(expr); symbol != nil && c.isOwnOwnedLocalReference(expr, symbol) {
			return printer.OwnCountOwnedLocal
		}
		return printer.OwnCountStable
	case ast.KindNewExpression:
		return printer.OwnCountOwned
	case ast.KindCallExpression:
		if c.isOwnCountedCall(expr) {
			return printer.OwnCountOwned
		}
		return printer.OwnCountUnstable
	case ast.KindElementAccessExpression:
		object := expr.Expression()
		if c.getOwnCellTypeOfExpression(object).Kind == printer.OwnCellArrayOfStruct {
			// An element lives as long as its array.
			switch c.getOwnCount(object) {
			case printer.OwnCountStable, printer.OwnCountOwnedLocal:
				return printer.OwnCountStable
			}
		}
		return printer.OwnCountUnstable
	case ast.KindAsExpression, ast.KindSatisfiesExpression, ast.KindNonNullExpression, ast.KindTypeAssertionExpression, ast.KindConsumeExpression:
		if inner := c.getOwnCount(expr.Expression()); inner != printer.OwnCountNone {
			return inner
		}
		return printer.OwnCountUnstable
	case ast.KindConditionalExpression:
		n := expr.AsConditionalExpression()
		return ownJoinCounts(c.getOwnCount(n.WhenTrue), c.getOwnCount(n.WhenFalse))
	case ast.KindBinaryExpression:
		n := expr.AsBinaryExpression()
		switch n.OperatorToken.Kind {
		case ast.KindBarBarToken, ast.KindQuestionQuestionToken:
			return ownJoinCounts(c.getOwnCount(n.Left), c.getOwnCount(n.Right))
		case ast.KindAmpersandAmpersandToken:
			return ownJoinCounts(printer.OwnCountNull, c.getOwnCount(n.Right))
		case ast.KindCommaToken:
			return c.getOwnCount(n.Right)
		case ast.KindEqualsToken, ast.KindBarBarEqualsToken, ast.KindQuestionQuestionEqualsToken, ast.KindAmpersandAmpersandEqualsToken:
			if !c.isOwnCellType(c.getOwnAssignmentTargetType(n.Left)) {
				return printer.OwnCountNone // the value was boxed for its target
			}
			if ast.IsIdentifier(ast.SkipParentheses(n.Left)) {
				return printer.OwnCountStable // the variable's value
			}
		}
	}
	return printer.OwnCountUnstable
}

// The count of a value that is one of two: owned if either is (the emit retains the other), else an
// unstable borrow if either is.
func ownJoinCounts(a printer.OwnCount, b printer.OwnCount) printer.OwnCount {
	rank := func(count printer.OwnCount) int {
		switch count {
		case printer.OwnCountNull:
			return 0
		case printer.OwnCountStable, printer.OwnCountOwnedLocal:
			return 1
		case printer.OwnCountOwned:
			return 3
		}
		return 2 // unstable, or not counted (conservatively)
	}
	switch max(rank(a), rank(b)) {
	case 0:
		return printer.OwnCountNull
	case 1:
		return printer.OwnCountStable
	case 3:
		return printer.OwnCountOwned
	}
	return printer.OwnCountUnstable
}

// Reports whether a call returns +1: its signature's own return type is counted and it is declared in
// a source file (generic and ambient functions do not follow the convention).
func (c *Checker) isOwnCountedCall(call *ast.Node) bool {
	signature := c.getResolvedSignature(call, nil, CheckModeNormal)
	if signature == nil || signature == c.unknownSignature || signature == c.anySignature || signature == c.resolvingSignature || signature.declaration == nil {
		return false
	}
	decl := signature.declaration
	if decl.Flags&ast.NodeFlagsAmbient != 0 {
		return false
	}
	if file := ast.GetSourceFileOfNode(decl); file == nil || file.IsDeclarationFile {
		return false
	}
	for signature.target != nil {
		signature = signature.target
	}
	return c.isOwnCellType(c.getReturnTypeOfSignature(signature))
}

// Reports whether a borrowed value stays valid as long as a local initialized from it: it is not read
// through a variable that may be reassigned (which releases the old value).
func (c *Checker) isOwnHoldable(expr *ast.Node) bool {
	expr = ast.SkipParentheses(expr)
	switch c.getOwnConversion(expr) {
	case printer.OwnConversionUnbox:
		// The handle stays alive until the thread's next task, unless the function is suspended.
		fn := ast.GetContainingFunction(expr)
		return fn == nil || ast.GetFunctionFlags(fn)&(ast.FunctionFlagsAsync|ast.FunctionFlagsGenerator) == 0
	case printer.OwnConversionZero:
		return true
	case printer.OwnConversionBox:
		return false
	}
	if t := c.getTypeOfExpression(expr); t.flags&TypeFlagsNullable != 0 && len(c.ownNonNullishMembers(t)) == 0 {
		return true
	}
	switch expr.Kind {
	case ast.KindIdentifier:
		symbol := c.ownReferencedVariable(expr)
		return symbol != nil && !c.isOwnAssigned(symbol)
	case ast.KindAsExpression, ast.KindSatisfiesExpression, ast.KindNonNullExpression, ast.KindTypeAssertionExpression, ast.KindConsumeExpression:
		return c.isOwnHoldable(expr.Expression())
	case ast.KindElementAccessExpression:
		return c.getOwnCellTypeOfExpression(expr.Expression()).Kind == printer.OwnCellArrayOfStruct && c.isOwnHoldable(expr.Expression())
	case ast.KindConditionalExpression:
		n := expr.AsConditionalExpression()
		return c.isOwnHoldable(n.WhenTrue) && c.isOwnHoldable(n.WhenFalse)
	case ast.KindBinaryExpression:
		n := expr.AsBinaryExpression()
		switch n.OperatorToken.Kind {
		case ast.KindBarBarToken, ast.KindQuestionQuestionToken:
			return c.isOwnHoldable(n.Left) && c.isOwnHoldable(n.Right)
		case ast.KindAmpersandAmpersandToken, ast.KindCommaToken:
			return c.isOwnHoldable(n.Right)
		}
	}
	return false
}

// How a variable or parameter of the original tree (or the one an identifier refers to) holds a
// struct or SharedArray value.
func (c *Checker) getOwnLocal(decl *ast.Node) printer.OwnLocal {
	if c.compilerOptions.OwnRuntime == "" || decl == nil {
		return printer.OwnLocalNone
	}
	if ast.IsIdentifier(decl) {
		symbol := c.ownReferencedVariable(decl)
		if symbol == nil {
			return printer.OwnLocalNone
		}
		decl = symbol.ValueDeclaration
	}
	if !ast.IsVariableDeclaration(decl) && !ast.IsParameterDeclaration(decl) || decl.Name() == nil || !ast.IsIdentifier(decl.Name()) {
		return printer.OwnLocalNone
	}
	symbol := c.getSymbolOfDeclaration(decl)
	if symbol == nil {
		return printer.OwnLocalNone
	}
	if local, ok := c.ownLayout.refs.locals[symbol]; ok {
		return local
	}
	if c.ownLayout.refs.locals == nil {
		c.ownLayout.refs.locals = map[*ast.Symbol]printer.OwnLocal{}
	}
	c.ownLayout.refs.locals[symbol] = printer.OwnLocalOwned // provisional
	local := c.computeOwnLocal(decl, symbol)
	c.ownLayout.refs.locals[symbol] = local
	return local
}

func (c *Checker) computeOwnLocal(decl *ast.Node, symbol *ast.Symbol) printer.OwnLocal {
	if !c.isOwnCellType(c.getTypeOfSymbol(symbol)) {
		return printer.OwnLocalNone
	}
	if ast.IsParameterDeclaration(decl) {
		fn := decl.Parent
		if fn == nil || !ast.IsFunctionLikeDeclaration(fn) || fn.Body() == nil || decl.AsParameterDeclaration().DotDotDotToken != nil {
			return printer.OwnLocalBorrowed
		}
		if init := decl.Initializer(); init != nil && !c.isOwnBorrowable(init) || c.isOwnAssigned(symbol) || ast.GetFunctionFlags(fn)&(ast.FunctionFlagsAsync|ast.FunctionFlagsGenerator) != 0 {
			return c.getOwnOwnedLocal(decl, symbol)
		}
		return printer.OwnLocalBorrowed
	}
	list := decl.Parent
	if list == nil || !ast.IsVariableDeclarationList(list) {
		return printer.OwnLocalNone
	}
	statement := list.Parent
	if ast.IsForInOrOfStatement(statement) {
		return printer.OwnLocalBorrowed // an error (checkOwnRefCounts)
	}
	if ast.IsVariableStatement(statement) && (ast.IsSourceFile(statement.Parent) || ast.IsModuleBlock(statement.Parent)) {
		return printer.OwnLocalModule
	}
	if !c.isOwnAssigned(symbol) {
		if init := decl.Initializer(); init == nil || c.isOwnBorrowable(init) {
			return printer.OwnLocalBorrowed // (always 0 without an initializer)
		}
	}
	return c.getOwnOwnedLocal(decl, symbol)
}

// Reports whether a local (or a parameter's default value) can borrow a value: null, or a stable
// borrow that stays valid as long as the local.
func (c *Checker) isOwnBorrowable(init *ast.Node) bool {
	switch c.getOwnCount(init) {
	case printer.OwnCountNull:
		return true
	case printer.OwnCountStable, printer.OwnCountOwnedLocal:
		return c.isOwnHoldable(init)
	}
	return false
}

// An owned local, which a return moves out (rather than retaining it) if no closure captures it.
func (c *Checker) getOwnOwnedLocal(decl *ast.Node, symbol *ast.Symbol) printer.OwnLocal {
	if fn := ownDeclaringFunction(decl); fn != nil && fn.Body() != nil && !c.isOwnCaptured(symbol) && c.isOwnReturnedLocal(fn.Body(), symbol) {
		return printer.OwnLocalOwnedMoved
	}
	return printer.OwnLocalOwned
}

// Reports whether a return statement of a function (not of the functions in it) returns a variable
// itself as its raw value (through parentheses, assertions and the branches of conditional and
// logical expressions).
func (c *Checker) isOwnReturnedLocal(body *ast.Node, symbol *ast.Symbol) bool {
	var returns func(expr *ast.Node) bool
	returns = func(expr *ast.Node) bool {
		if expr == nil || c.getOwnConversion(expr) != printer.OwnConversionNone {
			return false
		}
		switch expr.Kind {
		case ast.KindIdentifier:
			return c.ownReferencedVariable(expr) == symbol
		case ast.KindParenthesizedExpression, ast.KindAsExpression, ast.KindSatisfiesExpression, ast.KindNonNullExpression, ast.KindTypeAssertionExpression, ast.KindConsumeExpression:
			return returns(expr.Expression())
		case ast.KindConditionalExpression:
			n := expr.AsConditionalExpression()
			return returns(n.WhenTrue) || returns(n.WhenFalse)
		case ast.KindBinaryExpression:
			n := expr.AsBinaryExpression()
			switch n.OperatorToken.Kind {
			case ast.KindBarBarToken, ast.KindQuestionQuestionToken:
				return returns(n.Left) || returns(n.Right)
			case ast.KindAmpersandAmpersandToken, ast.KindCommaToken:
				return returns(n.Right)
			}
		}
		return false
	}
	found := false
	var visit ast.Visitor
	visit = func(node *ast.Node) bool {
		if found || ast.IsTypeNode(node) || ast.IsFunctionLike(node) || ast.IsClassLike(node) {
			return found
		}
		if ast.IsReturnStatement(node) && returns(node.Expression()) {
			found = true
			return true
		}
		return node.ForEachChild(visit)
	}
	body.ForEachChild(visit)
	return found
}

// Reports whether an identifier refers to an owned local (or owned parameter) of the function it is
// in, which a return can move out.
func (c *Checker) isOwnOwnedLocalReference(id *ast.Node, symbol *ast.Symbol) bool {
	decl := symbol.ValueDeclaration
	switch c.getOwnLocal(decl) {
	case printer.OwnLocalOwned, printer.OwnLocalOwnedMoved:
	default:
		return false
	}
	return ownDeclaringFunction(decl) == ast.GetContainingFunction(id) && !c.isOwnCaptured(symbol)
}

// The function that declares a variable or parameter; nil for a module's variables.
func ownDeclaringFunction(decl *ast.Node) *ast.Node {
	if ast.IsParameterDeclaration(decl) {
		return decl.Parent
	}
	return ast.GetContainingFunction(decl)
}

// The variable or parameter (with an identifier name) that an identifier in an expression refers to.
func (c *Checker) ownReferencedVariable(id *ast.Node) *ast.Symbol {
	if !ast.IsIdentifier(id) || !isOwnReferencePosition(id) {
		return nil
	}
	symbol := c.getResolvedSymbol(id)
	if symbol == nil || symbol == c.unknownSymbol {
		return nil
	}
	symbol = c.getExportSymbolOfValueSymbolIfExported(symbol)
	decl := symbol.ValueDeclaration
	if decl == nil || !ast.IsVariableDeclaration(decl) && !ast.IsParameterDeclaration(decl) || decl.Name() == nil || !ast.IsIdentifier(decl.Name()) {
		return nil
	}
	return symbol
}

// Reports whether an expression is a value (not a property name, a declaration name, a label, ...).
func isOwnValueExpression(node *ast.Node) bool {
	if node.Parent == nil || !ast.IsExpression(node) {
		return false
	}
	if ast.IsIdentifier(node) || ast.IsPrivateIdentifier(node) {
		return isOwnReferencePosition(node)
	}
	return true
}

func isOwnReferencePosition(id *ast.Node) bool {
	parent := id.Parent
	if parent == nil {
		return false
	}
	switch parent.Kind {
	case ast.KindPropertyAccessExpression:
		if parent.Name() == id {
			return false
		}
	case ast.KindMetaProperty, ast.KindQualifiedName:
		return false
	}
	return !ast.IsDeclarationName(id) && !ast.IsPartOfTypeNode(id) && ast.IsExpressionNode(id)
}

// Reports whether a variable or parameter is assigned anywhere (nested functions included), other
// than by its declaration.
func (c *Checker) isOwnAssigned(symbol *ast.Symbol) bool {
	decl := symbol.ValueDeclaration
	if decl == nil || ast.IsVariableDeclaration(decl) && ast.GetCombinedNodeFlags(decl)&ast.NodeFlagsConstant != 0 {
		return false
	}
	if assigned, ok := c.ownLayout.refs.assigned[symbol]; ok {
		return assigned
	}
	scope := ast.FindAncestor(decl.Parent, ast.IsFunctionOrSourceFile)
	assigned := scope != nil && c.isOwnAssignedIn(scope, symbol)
	if c.ownLayout.refs.assigned == nil {
		c.ownLayout.refs.assigned = map[*ast.Symbol]bool{}
	}
	c.ownLayout.refs.assigned[symbol] = assigned
	return assigned
}

// Reports whether a function other than the one that declares a counted variable refers to it.
func (c *Checker) isOwnCaptured(symbol *ast.Symbol) bool {
	decl := symbol.ValueDeclaration
	if decl == nil {
		return false
	}
	return c.getOwnCaptureInfo(ast.GetSourceFileOfNode(decl)).captured[symbol]
}

// The declarations of the counted locals of other functions that a function, arrow function or
// function declaration refers to, in the order of their first reference.
func (c *Checker) getOwnCaptures(fn *ast.Node) []*ast.Node {
	if c.compilerOptions.OwnRuntime == "" {
		return nil
	}
	return c.getOwnCaptureInfo(ast.GetSourceFileOfNode(fn)).captures[fn]
}

func (c *Checker) getOwnCaptureInfo(file *ast.SourceFile) *ownCaptureInfo {
	if file == nil {
		return &ownCaptureInfo{}
	}
	if info, ok := c.ownLayout.refs.captures[file]; ok {
		return info
	}
	info := &ownCaptureInfo{captured: map[*ast.Symbol]bool{}, captures: map[*ast.Node][]*ast.Node{}}
	if c.ownLayout.refs.captures == nil {
		c.ownLayout.refs.captures = map[*ast.SourceFile]*ownCaptureInfo{}
	}
	c.ownLayout.refs.captures[file] = info
	var scopes []*ast.Node // the functions and classes around the current node, innermost last
	var visit ast.Visitor
	visit = func(node *ast.Node) bool {
		if ast.IsTypeNode(node) {
			return false
		}
		if ast.IsFunctionLikeDeclaration(node) || ast.IsClassStaticBlockDeclaration(node) || ast.IsClassLike(node) {
			scopes = append(scopes, node)
			node.ForEachChild(visit)
			scopes = scopes[:len(scopes)-1]
			return false
		}
		if ast.IsIdentifier(node) {
			c.recordOwnCapture(info, node, scopes)
			return false
		}
		return node.ForEachChild(visit)
	}
	file.AsNode().ForEachChild(visit)
	return info
}

func (c *Checker) recordOwnCapture(info *ownCaptureInfo, id *ast.Node, scopes []*ast.Node) {
	symbol := c.ownReferencedVariable(id)
	if symbol == nil || !c.isOwnCellType(c.getTypeOfSymbol(symbol)) {
		return
	}
	declaring := ownDeclaringFunction(symbol.ValueDeclaration)
	member := false
	for i := len(scopes) - 1; i >= 0 && scopes[i] != declaring; i-- {
		scope := scopes[i]
		info.captured[symbol] = true
		if declaring == nil {
			continue // a module's variable lives as long as the module
		}
		switch scope.Kind {
		case ast.KindArrowFunction, ast.KindFunctionExpression, ast.KindFunctionDeclaration:
			if !slices.Contains(info.captures[scope], symbol.ValueDeclaration) {
				info.captures[scope] = append(info.captures[scope], symbol.ValueDeclaration)
			}
		default:
			member = true
		}
	}
	if member {
		info.members = append(info.members, id)
	}
}

// Reports whether a loop reads or stores a reference field (in its condition, update or body, outside
// the functions in it), so that each iteration ends at a quiescent point.
func (c *Checker) isOwnQuiescentLoop(loop *ast.Node) bool {
	if c.compilerOptions.OwnRuntime == "" {
		return false
	}
	if quiescent, ok := c.ownLayout.refs.quiescent[loop]; ok {
		return quiescent
	}
	var parts []*ast.Node
	switch loop.Kind {
	case ast.KindForStatement:
		n := loop.AsForStatement()
		parts = []*ast.Node{n.Condition, n.Incrementor, n.Statement}
	case ast.KindForInStatement, ast.KindForOfStatement:
		parts = []*ast.Node{loop.AsForInOrOfStatement().Statement}
	case ast.KindWhileStatement, ast.KindDoStatement:
		parts = []*ast.Node{loop.Expression(), loop.Statement()}
	}
	found := false
	var visit ast.Visitor
	visit = func(node *ast.Node) bool {
		if found || ast.IsTypeNode(node) || ast.IsFunctionLike(node) || ast.IsClassLike(node) {
			return found
		}
		if ast.IsPropertyAccessExpression(node) && c.isOwnReferenceField(node) {
			found = true
			return true
		}
		return node.ForEachChild(visit)
	}
	for _, part := range parts {
		if part != nil && !found {
			if !visit(part) {
				part.ForEachChild(visit)
			}
		}
	}
	if c.ownLayout.refs.quiescent == nil {
		c.ownLayout.refs.quiescent = map[*ast.Node]bool{}
	}
	c.ownLayout.refs.quiescent[loop] = found
	return found
}

// Reports whether a property access reads or writes a reference field of a shared struct.
func (c *Checker) isOwnReferenceField(access *ast.Node) bool {
	cell := c.getOwnCellTypeOfExpression(access.Expression())
	if cell.Kind != printer.OwnCellStruct {
		return false
	}
	k := slices.Index(cell.Fields, access.Name().Text())
	return k >= 0 && cell.Refs[k]
}

// Reports whether an argument is a counted argument of the runtime's `spawn`, which the emit passes
// as `__ref(E)`: the thread holds a count of its own.
func (c *Checker) isOwnSpawnArgument(arg *ast.Node) bool {
	call := arg.Parent
	if c.compilerOptions.OwnRuntime == "" || call == nil || !ast.IsCallExpression(call) || slices.Index(call.Arguments(), arg) < 1 {
		return false
	}
	if !c.isOwnCellExpression(arg) || c.getOwnConversion(arg) == printer.OwnConversionBox {
		return false
	}
	signature := c.getResolvedSignature(call, nil, CheckModeNormal)
	if signature == nil || signature.declaration == nil || !c.isOwnRuntimeFile(ast.GetSourceFileOfNode(signature.declaration)) {
		return false
	}
	name := signature.declaration.Name()
	return name != nil && ast.IsIdentifier(name) && name.Text() == "spawn"
}

// Reports the reference counting errors of a file: a captured counted variable that is reassigned, a
// raw address stored in an ordinary array or object, bound by destructuring or used as a for-of
// variable, and a counted local captured by a method or a class.
func (c *Checker) checkOwnRefCounts(file *ast.SourceFile) {
	info := c.getOwnCaptureInfo(file)
	for _, id := range info.members {
		c.error(id, diagnostics.X_0_cannot_be_captured_by_a_method_or_a_class_Colon_its_value_is_a_counted_address_in_the_shared_heap_which_only_functions_and_arrow_functions_can_hold, id.Text())
	}
	stored := func(expr *ast.Node) {
		if expr == nil {
			return
		}
		switch c.getOwnCount(expr) {
		case printer.OwnCountNone, printer.OwnCountNull:
			return
		}
		c.error(expr, diagnostics.A_value_of_type_0_cannot_be_stored_in_an_ordinary_array_or_object_Colon_reference_counting_cannot_see_it_there_Use_a_SharedArray_or_a_shared_struct_field_or_a_place_typed_unknown_which_holds_a_handle, c.typeToString(c.getTypeOfExpression(expr), nil))
	}
	destructured := func(name *ast.Node, t *Type) {
		if c.isOwnCellType(t) {
			c.error(name, diagnostics.X_0_cannot_be_bound_by_destructuring_Colon_its_value_is_an_address_in_the_shared_heap, name.Text())
		}
	}
	var checkPattern func(pattern *ast.Node)
	checkPattern = func(pattern *ast.Node) {
		// An assignment pattern: its targets must not be counted.
		elements := pattern.Elements()
		if ast.IsObjectLiteralExpression(pattern) {
			elements = pattern.Properties()
		}
		for _, element := range elements {
			target := element
			switch element.Kind {
			case ast.KindSpreadElement, ast.KindSpreadAssignment:
				target = element.Expression()
			case ast.KindPropertyAssignment:
				target = element.Initializer()
			case ast.KindShorthandPropertyAssignment:
				target = element.Name()
			case ast.KindOmittedExpression:
				continue
			}
			if ast.IsBinaryExpression(target) && target.AsBinaryExpression().OperatorToken.Kind == ast.KindEqualsToken {
				target = target.AsBinaryExpression().Left
			}
			target = ast.SkipParentheses(target)
			if ast.IsArrayLiteralExpression(target) || ast.IsObjectLiteralExpression(target) {
				checkPattern(target)
				continue
			}
			if ast.IsIdentifier(target) || ast.IsAccessExpression(target) {
				if c.isOwnCellType(c.getOwnAssignmentTargetType(target)) {
					c.error(target, diagnostics.X_0_cannot_be_bound_by_destructuring_Colon_its_value_is_an_address_in_the_shared_heap, ownTargetName(target))
				}
			}
		}
	}
	var visit ast.Visitor
	visit = func(node *ast.Node) bool {
		if ast.IsTypeNode(node) {
			return false
		}
		switch node.Kind {
		case ast.KindIdentifier:
			if getAssignmentTargetKind(node) != AssignmentKindNone {
				if symbol := c.ownReferencedVariable(node); symbol != nil && info.captured[symbol] && c.isOwnCellType(c.getTypeOfSymbol(symbol)) {
					c.error(node, diagnostics.X_0_cannot_be_reassigned_Colon_a_closure_captures_it_and_its_value_is_a_counted_address_in_the_shared_heap, node.Text())
				}
			}
			return false
		case ast.KindArrayLiteralExpression:
			if ast.IsAssignmentTarget(node) {
				checkPattern(node)
			} else {
				for _, element := range node.Elements() {
					if !ast.IsSpreadElement(element) && !ast.IsOmittedExpression(element) {
						stored(element)
					}
				}
			}
		case ast.KindObjectLiteralExpression:
			if ast.IsAssignmentTarget(node) {
				checkPattern(node)
			} else {
				for _, property := range node.Properties() {
					switch property.Kind {
					case ast.KindPropertyAssignment:
						stored(property.Initializer())
					case ast.KindShorthandPropertyAssignment:
						if t := c.getTypeOfExpression(property.Name()); c.isOwnCellType(t) && c.getOwnConversion(property.Name()) != printer.OwnConversionBox {
							c.error(property.Name(), diagnostics.A_value_of_type_0_cannot_be_stored_in_an_ordinary_array_or_object_Colon_reference_counting_cannot_see_it_there_Use_a_SharedArray_or_a_shared_struct_field_or_a_place_typed_unknown_which_holds_a_handle, c.typeToString(t, nil))
						}
					}
				}
			}
		case ast.KindBinaryExpression:
			n := node.AsBinaryExpression()
			switch n.OperatorToken.Kind {
			case ast.KindEqualsToken, ast.KindBarBarEqualsToken, ast.KindQuestionQuestionEqualsToken, ast.KindAmpersandAmpersandEqualsToken:
				left := ast.SkipParentheses(n.Left)
				if ast.IsAccessExpression(left) && !c.isOwnCellExpression(left.Expression()) {
					stored(n.Right)
				}
			}
		case ast.KindPropertyDeclaration:
			stored(node.Initializer())
		case ast.KindParameter:
			if ast.IsIdentifier(node.Name()) && node.Parent != nil && ast.IsFunctionLikeDeclaration(node.Parent) && node.Parent.Body() != nil {
				t := c.getTypeOfSymbol(c.getSymbolOfDeclaration(node))
				switch {
				case node.AsParameterDeclaration().DotDotDotToken != nil && c.isArrayType(t):
					// The arguments are stored in an ordinary array.
					if element := c.getElementTypeOfArrayType(t); c.isOwnCellType(element) {
						c.error(node.Name(), diagnostics.A_value_of_type_0_cannot_be_stored_in_an_ordinary_array_or_object_Colon_reference_counting_cannot_see_it_there_Use_a_SharedArray_or_a_shared_struct_field_or_a_place_typed_unknown_which_holds_a_handle, c.typeToString(element, nil))
					}
				case !c.isOwnCellType(t):
				case ast.IsParameterPropertyDeclaration(node, node.Parent):
					c.error(node.Name(), diagnostics.A_value_of_type_0_cannot_be_stored_in_an_ordinary_array_or_object_Colon_reference_counting_cannot_see_it_there_Use_a_SharedArray_or_a_shared_struct_field_or_a_place_typed_unknown_which_holds_a_handle, c.typeToString(t, nil))
				case ast.GetFunctionFlags(node.Parent)&ast.FunctionFlagsGenerator != 0:
					// A generator's body (and so the retain of an owned parameter) runs at its first `next()`.
					c.error(node.Name(), diagnostics.A_generator_s_parameter_cannot_have_type_0_Colon_the_generator_runs_after_its_caller_s_borrow_ends, c.typeToString(t, nil))
				}
			}
		case ast.KindBindingElement:
			if name := node.Name(); name != nil && ast.IsIdentifier(name) {
				if symbol := c.getSymbolOfDeclaration(node); symbol != nil {
					destructured(name, c.getTypeOfSymbol(symbol))
				}
			}
		case ast.KindForOfStatement, ast.KindForInStatement:
			init := node.AsForInOrOfStatement().Initializer
			var t *Type
			if ast.IsVariableDeclarationList(init) {
				if decls := init.AsVariableDeclarationList().Declarations.Nodes; len(decls) == 1 && ast.IsIdentifier(decls[0].Name()) {
					t = c.getTypeOfSymbol(c.getSymbolOfDeclaration(decls[0]))
					init = decls[0].Name()
				}
			} else if ast.IsIdentifier(ast.SkipParentheses(init)) || ast.IsAccessExpression(ast.SkipParentheses(init)) {
				t = c.getOwnAssignmentTargetType(init)
			}
			if t != nil && c.isOwnCellType(t) {
				c.error(init, diagnostics.A_for_of_or_for_in_variable_cannot_have_type_0_Colon_its_value_is_an_address_in_the_shared_heap, c.typeToString(t, nil))
			}
		}
		return node.ForEachChild(visit)
	}
	file.AsNode().ForEachChild(visit)
}

func ownTargetName(target *ast.Node) string {
	if ast.IsPropertyAccessExpression(target) {
		return target.Name().Text()
	}
	if ast.IsIdentifier(target) {
		return target.Text()
	}
	return "element"
}
