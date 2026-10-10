package tstransforms

import (
	"slices"
	"strconv"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/printer"
	"github.com/microsoft/typescript-go/internal/transformers"
)

// Lowers the typed layouts of the own/shared threads extension (with `--ownRuntime`), and counts
// their references. A value whose type is a shared struct or a SharedArray is the byte address of
// its block in the runtime's shared heap, which `__F64` (a Float64Array) and `__I32` (an Int32Array)
// view:
//
//	shared struct S { f0; ...; f(n-1) }   [0: i32 kind][4: i32 count][8: f0][16: f1]...    size 8 + 8n
//	SharedArray<E>                        [0: i32 kind][4: i32 length][8: i32 element size][12: i32 count]
//	                                      then the elements from byte 16: f64s, or whole struct blocks
//
//	new S()                    __newStruct(S)
//	new SharedArray(S, n)      __newSharedArray(S, n)
//	new SharedArray(Number, n) __newSharedArray(8, n)
//	e.fk (number field)        __F64[(E >> 3) + 1 + k]                  (1 + k folded)
//	e.fk (reference field)     __I32[(E >> 2) + 2 + 2 * k]              (2 + 2 * k folded; 0 for null)
//	e.fk = V (reference field) __store((E >> 2) + 2 + 2 * k, V)
//	a.length                   __I32[(A + 4) >> 2]
//	a[i]  (struct elements)    CHECK ? A + 16 + I * size : __OOB[I]
//	a[i]  (number, read)       CHECK ? __F64[((A + 16) >> 3) + I] : __OOB[I]
//	a[i]  (number, write)      __F64[((A + 16) >> 3) + (CHECK ? I : __OOB[I])]
//	CHECK                      (I >>> 0) === I && I < __I32[(A + 4) >> 2]
//
// A and I are copied to temporaries when they are not simple. The failure path reads `__OOB[I]`, a
// Proxy that throws, rather than calling a function: a call, even one that is never taken, keeps
// V8's TurboFan from optimizing the loops these accesses sit in. Where the checker proves that I is
// an integer in [0, A.length) (see "Bounds-check elimination" in checker/ownlayout.go: I counts up
// from a non-negative integer in a `for` loop bounded by A.length, and neither A nor I is assigned),
// the element is accessed with no check at all. Where the checker decides that a value
// crosses between the raw representation and a place typed otherwise, the expression is wrapped in
// `__box(E)` or `__unbox(E)` (see checker/ownlayout.go); null and undefined are 0 where an address is
// expected. The helpers are imported from the `ownRuntime` module by the ES module transform.
//
// Reference counting (REFCOUNT.md in porffor's threads/escape-proto; the checker's decisions are in
// checker/ownrefcount.go). Every raw address is visited in a context: used at once (an operand, the
// object of an access, a helper's argument), passed to a function (which may reach a quiescent
// point), taken (+1 for an owned local, a reference field or a result), or discarded. Its count
// (owned, a stable or an unstable borrow) decides the code:
//
//	                at once           argument                   taken          discarded
//	owned           (_a = E) *        (_a = E) *                 E              __release(E)
//	stable borrow   E                 E                          __retain(E)    E
//	unstable borrow E                 (_a = __retain(E)) *       __retain(E)    E
//
// * `__release(_a)` follows the full expression (or the statement). A return moves an owned local's
// count out (`(_t = x, x = 0, _t)`). An owned local is released when its block ends:
// `const x = E; try { ...rest of the block } finally { __release(x) }`, and its reassignment retains
// the new value before releasing the old (`_t = x, x = E', __release(_t)`). An owned parameter is
// retained on entry. A counted argument of `spawn` is `__ref(E)`, a closure that captures counted
// variables is `__hold(fn, x, ...)`, and a loop that reads or stores a reference field ends each
// iteration (and each `continue`) with `__quiescent()`.
type OwnLayoutTransformer struct {
	transformers.Transformer
	compilerOptions           *core.CompilerOptions
	emitResolver              printer.EmitResolver
	getEmitModuleFormatOfFile func(file ast.HasFileName) core.ModuleKind
	ctx                       ownContext              // the context of the next expression visited (see visitIn)
	frames                    []*ownFrame             // the full expressions being visited, innermost last; a nil frame fences off a function or class
	conditional               int                     // how deeply the expression being visited is evaluated conditionally, within the innermost frame
	paramInits                map[*ast.Node]*ast.Node // default values of owned parameters (by original parameter), moved into the body
}

// The context of a raw address (see above).
type ownContext int

const (
	ownAtOnce   ownContext = iota // used at once: an operand, the object of an access, a helper's argument
	ownArgument                   // passed to a function, which may reach a quiescent point
	ownTake                       // taken: needs +1
	ownReturn                     // a function's result: +1, and an owned local's count moves out
	ownDiscard                    // the value is not used
)

// A full expression: the temporaries holding owned values, which are released after it.
type ownFrame struct {
	released    []*ast.Node
	zeroed      []*ast.Node // those assigned only conditionally: set to 0 before the full expression
	conditional int         // the enclosing frame's depth
}

func NewOwnLayoutTransformer(opt *transformers.TransformOptions) *transformers.Transformer {
	tx := &OwnLayoutTransformer{compilerOptions: opt.CompilerOptions, emitResolver: opt.EmitResolver, getEmitModuleFormatOfFile: opt.GetEmitModuleFormatOfFile}
	return tx.NewTransformer(tx.visit, opt.Context)
}

func (tx *OwnLayoutTransformer) visit(node *ast.Node) *ast.Node {
	ctx := tx.ctx
	tx.ctx = ownAtOnce
	switch node.Kind {
	case ast.KindSourceFile:
		if tx.emitResolver == nil || !transformers.UsesOwnRuntime(tx.EmitContext(), tx.compilerOptions, node.AsSourceFile(), tx.getEmitModuleFormatOfFile) {
			return node
		}
		tx.paramInits = map[*ast.Node]*ast.Node{}
		return tx.Visitor().VisitEachChild(node)
	case ast.KindBlock:
		return tx.visitBlock(node)
	case ast.KindCaseClause, ast.KindDefaultClause:
		n := node.AsCaseOrDefaultClause()
		expression := tx.fullExpression(n.Expression, ownAtOnce)
		statements := tx.visitStatements(n.Statements.Nodes)
		return tx.Factory().UpdateCaseOrDefaultClause(n, expression, tx.statementList(statements, n.Statements))
	case ast.KindExpressionStatement:
		return tx.visitExpressionStatement(node)
	case ast.KindReturnStatement:
		return tx.Factory().UpdateReturnStatement(node.AsReturnStatement(), tx.fullExpression(node.Expression(), ownReturn))
	case ast.KindIfStatement:
		n := node.AsIfStatement()
		expression := tx.fullExpression(n.Expression, ownAtOnce)
		return tx.Factory().UpdateIfStatement(n, expression, tx.Visitor().VisitEmbeddedStatement(n.ThenStatement), tx.Visitor().VisitEmbeddedStatement(n.ElseStatement))
	case ast.KindSwitchStatement:
		n := node.AsSwitchStatement()
		expression := tx.fullExpression(n.Expression, ownAtOnce)
		return tx.Factory().UpdateSwitchStatement(n, expression, tx.Visitor().VisitNode(n.CaseBlock))
	case ast.KindThrowStatement:
		return tx.Factory().UpdateThrowStatement(node.AsThrowStatement(), tx.fullExpression(node.Expression(), ownAtOnce))
	case ast.KindWithStatement:
		n := node.AsWithStatement()
		expression := tx.fullExpression(n.Expression, ownAtOnce)
		return tx.Factory().UpdateWithStatement(n, expression, tx.Visitor().VisitEmbeddedStatement(n.Statement))
	case ast.KindWhileStatement:
		n := node.AsWhileStatement()
		expression := tx.fullExpression(n.Expression, ownAtOnce)
		return tx.Factory().UpdateWhileStatement(n, expression, tx.visitLoopBody(n.Statement, node))
	case ast.KindDoStatement:
		n := node.AsDoStatement()
		statement := tx.visitLoopBody(n.Statement, node)
		return tx.Factory().UpdateDoStatement(n, statement, tx.fullExpression(n.Expression, ownAtOnce))
	case ast.KindForStatement:
		return tx.visitForStatement(node, nil /*label*/)
	case ast.KindForInStatement, ast.KindForOfStatement:
		n := node.AsForInOrOfStatement()
		initializer := tx.Visitor().VisitNode(n.Initializer)
		expression := tx.fullExpression(n.Expression, ownAtOnce)
		return tx.Factory().UpdateForInOrOfStatement(n, n.AwaitModifier, initializer, expression, tx.visitLoopBody(n.Statement, node))
	case ast.KindLabeledStatement:
		n := node.AsLabeledStatement()
		if ast.IsForStatement(n.Statement) && len(tx.ownedLocalsOfFor(n.Statement)) > 0 {
			return tx.visitForStatement(n.Statement, n.Label)
		}
	case ast.KindContinueStatement:
		if loop := ownContinueTarget(tx.original(node)); loop != nil && tx.emitResolver.IsOwnQuiescentLoop(loop) {
			return tx.Factory().NewSyntaxList([]*ast.Node{tx.quiescent(), tx.Visitor().VisitEachChild(node)})
		}
	case ast.KindVariableDeclarationList:
		return tx.visitVariableDeclarationList(node)
	case ast.KindVariableDeclaration:
		return tx.visitVariableDeclaration(node)
	case ast.KindParameter:
		return tx.visitParameter(node)
	case ast.KindPropertyDeclaration:
		n := node.AsPropertyDeclaration()
		initializer := tx.fullExpression(n.Initializer, ownAtOnce)
		return tx.Factory().UpdatePropertyDeclaration(n, tx.Visitor().VisitModifiers(n.Modifiers()), tx.Visitor().VisitNode(n.Name()), n.PostfixToken, n.Type, initializer)
	case ast.KindExportAssignment:
		n := node.AsExportAssignment()
		return tx.Factory().UpdateExportAssignment(n, tx.Visitor().VisitModifiers(n.Modifiers()), n.IsExportEquals, n.Type, tx.fullExpression(n.Expression, ownAtOnce))
	case ast.KindArrowFunction, ast.KindFunctionExpression:
		return tx.visitClosure(node)
	case ast.KindShorthandPropertyAssignment:
		return tx.lower(node, ctx)
	case ast.KindFunctionDeclaration, ast.KindMethodDeclaration, ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor,
		ast.KindClassStaticBlockDeclaration, ast.KindClassDeclaration, ast.KindClassExpression:
		tx.pushFrame(nil /*fence*/)
		result := tx.Visitor().VisitEachChild(node)
		tx.popFrame()
		return result
	}
	if ast.IsExpression(node) {
		if fn := tx.conciseBodyOf(node); fn != nil {
			return tx.visitConciseBody(node, fn)
		}
		return tx.visitExpression(node, ctx)
	}
	return tx.Visitor().VisitEachChild(node)
}

// The parse tree node a (possibly transformed) node came from.
func (tx *OwnLayoutTransformer) original(node *ast.Node) *ast.Node {
	original := tx.EmitContext().MostOriginal(node)
	if original == nil || original.Flags&ast.NodeFlagsSynthesized != 0 {
		return nil
	}
	return original
}

// Visits an expression in a context.
func (tx *OwnLayoutTransformer) visitIn(node *ast.Node, ctx ownContext) *ast.Node {
	if node == nil {
		return nil
	}
	tx.ctx = ctx
	result := tx.Visitor().VisitNode(node)
	tx.ctx = ownAtOnce
	return result
}

// Visits an expression (applying its box, unbox or null conversion) in a context.
func (tx *OwnLayoutTransformer) visitExpression(node *ast.Node, ctx ownContext) *ast.Node {
	original := tx.original(node)
	if original == nil {
		return tx.visitValue(node, nil, ctx)
	}
	switch tx.emitResolver.GetOwnConversion(original) {
	case printer.OwnConversionBox:
		// The raw value is used at once by `__box`; the handle holds a count of its own.
		return tx.finish(tx.call(transformers.OwnRuntimeBoxHelper, tx.visitValue(node, original, ownAtOnce)), node)
	case printer.OwnConversionUnbox:
		// `__unbox(E)` is a stable borrow.
		unboxed := tx.finish(tx.call(transformers.OwnRuntimeUnboxHelper, tx.visitValue(node, original, ownAtOnce)), node)
		return tx.count(unboxed, printer.OwnCountStable, ctx)
	case printer.OwnConversionZero:
		return tx.zero(node, original)
	}
	return tx.visitValue(node, original, ctx)
}

// Visits an expression without its conversion: lowers it, and counts it in its context.
func (tx *OwnLayoutTransformer) visitValue(node *ast.Node, original *ast.Node, ctx ownContext) *ast.Node {
	switch node.Kind {
	case ast.KindParenthesizedExpression:
		return tx.Factory().UpdateParenthesizedExpression(node.AsParenthesizedExpression(), tx.visitIn(node.Expression(), ctx))
	case ast.KindPartiallyEmittedExpression:
		return tx.Factory().UpdatePartiallyEmittedExpression(node.AsPartiallyEmittedExpression(), tx.visitIn(node.Expression(), ctx))
	}
	count := printer.OwnCountNone
	if original != nil {
		count = tx.emitResolver.GetOwnCount(original)
	}
	switch node.Kind {
	case ast.KindConditionalExpression:
		return tx.visitConditional(node, count, ctx)
	case ast.KindBinaryExpression:
		switch node.AsBinaryExpression().OperatorToken.Kind {
		case ast.KindBarBarToken, ast.KindAmpersandAmpersandToken, ast.KindQuestionQuestionToken:
			return tx.visitLogical(node, count, ctx)
		case ast.KindCommaToken:
			n := node.AsBinaryExpression()
			left := tx.visitIn(n.Left, ownDiscard)
			return tx.Factory().UpdateBinaryExpression(n, nil /*modifiers*/, left, nil /*typeNode*/, n.OperatorToken, tx.visitIn(n.Right, ctx))
		}
	}
	return tx.count(tx.lower(node, ctx), count, ctx)
}

// The contexts of the operands of a conditional or logical expression: an owned result takes each
// operand (+1) and is counted as a whole; a borrowed one is counted as a whole.
func ownOperandContext(count printer.OwnCount, ctx ownContext) (operands ownContext, whole bool) {
	switch ctx {
	case ownReturn:
		return ownReturn, false
	case ownTake:
		if count == printer.OwnCountOwned {
			return ownTake, false
		}
		return ownAtOnce, true
	}
	if count == printer.OwnCountOwned {
		return ownTake, true
	}
	return ownAtOnce, true
}

func (tx *OwnLayoutTransformer) visitConditional(node *ast.Node, count printer.OwnCount, ctx ownContext) *ast.Node {
	n := node.AsConditionalExpression()
	operands, whole := ownOperandContext(count, ctx)
	condition := tx.visitIn(n.Condition, ownAtOnce)
	tx.conditional++
	whenTrue := tx.visitIn(n.WhenTrue, operands)
	whenFalse := tx.visitIn(n.WhenFalse, operands)
	tx.conditional--
	result := tx.Factory().UpdateConditionalExpression(n, condition, n.QuestionToken, whenTrue, n.ColonToken, whenFalse)
	if whole {
		return tx.count(result, count, ctx)
	}
	return result
}

// `a || b`, `a ?? b` (`||` on an address, where null is 0) and `a && b` (whose result is a only when
// it is falsy).
func (tx *OwnLayoutTransformer) visitLogical(node *ast.Node, count printer.OwnCount, ctx ownContext) *ast.Node {
	n := node.AsBinaryExpression()
	operator := n.OperatorToken
	operands, whole := ownOperandContext(count, ctx)
	leftContext := operands
	if operator.Kind == ast.KindAmpersandAmpersandToken {
		leftContext = ownAtOnce
	}
	if operator.Kind == ast.KindQuestionQuestionToken {
		if original := tx.original(n.Left); original != nil && tx.emitResolver.GetOwnCount(original) != printer.OwnCountNone && tx.emitResolver.GetOwnConversion(original) == printer.OwnConversionNone {
			operator = tx.Factory().NewToken(ast.KindBarBarToken)
		}
	}
	left := tx.visitIn(n.Left, leftContext)
	tx.conditional++
	right := tx.visitIn(n.Right, operands)
	tx.conditional--
	result := tx.Factory().UpdateBinaryExpression(n, nil /*modifiers*/, left, nil /*typeNode*/, operator, right)
	if whole {
		return tx.count(result, count, ctx)
	}
	return result
}

// Counts a (lowered) raw address in its context (see the table above).
func (tx *OwnLayoutTransformer) count(expr *ast.Node, count printer.OwnCount, ctx ownContext) *ast.Node {
	switch count {
	case printer.OwnCountOwned:
		switch ctx {
		case ownTake, ownReturn:
			return expr
		case ownDiscard:
			return tx.call(transformers.OwnRuntimeReleaseHelper, expr)
		}
		return tx.temporary(expr)
	case printer.OwnCountStable, printer.OwnCountOwnedLocal:
		switch ctx {
		case ownTake:
			return tx.call(transformers.OwnRuntimeRetainHelper, expr)
		case ownReturn:
			if count == printer.OwnCountOwnedLocal && ast.IsIdentifier(expr) {
				return tx.move(expr)
			}
			return tx.call(transformers.OwnRuntimeRetainHelper, expr)
		}
	case printer.OwnCountUnstable:
		switch ctx {
		case ownTake, ownReturn:
			return tx.call(transformers.OwnRuntimeRetainHelper, expr)
		case ownArgument:
			return tx.temporary(tx.call(transformers.OwnRuntimeRetainHelper, expr))
		}
	}
	return expr
}

// `(_a = E)`, released after the full expression. Without a full expression around (a decorator, a
// computed property name), E is left uncounted.
func (tx *OwnLayoutTransformer) temporary(expr *ast.Node) *ast.Node {
	if len(tx.frames) == 0 || tx.frames[len(tx.frames)-1] == nil {
		return expr
	}
	frame := tx.frames[len(tx.frames)-1]
	temp := tx.Factory().NewTempVariable()
	tx.EmitContext().AddVariableDeclaration(temp)
	frame.released = append(frame.released, temp)
	if tx.conditional > 0 {
		frame.zeroed = append(frame.zeroed, temp)
	}
	return tx.paren(tx.Factory().NewAssignmentExpression(temp, expr))
}

// Drops the parentheses around a temporary's assignment where it is an argument.
func (tx *OwnLayoutTransformer) unparen(expr *ast.Node) *ast.Node {
	if ast.IsParenthesizedExpression(expr) && tx.original(expr) == nil && ast.IsBinaryExpression(expr.Expression()) &&
		expr.Expression().AsBinaryExpression().OperatorToken.Kind == ast.KindEqualsToken {
		return expr.Expression()
	}
	return expr
}

// `(_t = x, x = 0, _t)`: an owned local's count moves out.
func (tx *OwnLayoutTransformer) move(x *ast.Node) *ast.Node {
	temp := tx.Factory().NewTempVariable()
	tx.EmitContext().AddVariableDeclaration(temp)
	return tx.paren(tx.Factory().InlineExpressions([]*ast.Node{
		tx.Factory().NewAssignmentExpression(temp, x),
		tx.Factory().NewAssignmentExpression(tx.dup(x), tx.number(0)),
		temp,
	}))
}

// null or undefined where an address is expected: 0 (after any side effects).
func (tx *OwnLayoutTransformer) zero(node *ast.Node, original *ast.Node) *ast.Node {
	inner := ast.SkipOuterExpressions(original, ast.OEKAll)
	switch {
	case inner.Kind == ast.KindNullKeyword, ast.IsIdentifier(inner) && inner.Text() == "undefined",
		ast.IsVoidExpression(inner) && ast.IsLiteralExpression(ast.SkipParentheses(inner.Expression())):
		return tx.finish(tx.number(0), node)
	}
	return tx.paren(tx.Factory().NewCommaExpression(tx.visitValue(node, original, ownDiscard), tx.number(0)))
}

// Visits an expression that is a full expression of a statement or declaration: the temporaries in
// it are released after it, as `(_r = E, __release(_a), _r)`.
func (tx *OwnLayoutTransformer) fullExpression(node *ast.Node, ctx ownContext) *ast.Node {
	if node == nil {
		return nil
	}
	frame := tx.pushFrame(&ownFrame{})
	result := tx.visitIn(node, ctx)
	tx.popFrame()
	return tx.releaseAfter(result, frame, ctx != ownDiscard)
}

func (tx *OwnLayoutTransformer) releaseAfter(expr *ast.Node, frame *ownFrame, keepValue bool) *ast.Node {
	if len(frame.released) == 0 {
		return expr
	}
	var expressions []*ast.Node
	for _, temp := range frame.zeroed {
		expressions = append(expressions, tx.Factory().NewAssignmentExpression(temp, tx.number(0)))
	}
	var result *ast.Node
	if keepValue {
		result = tx.Factory().NewTempVariable()
		tx.EmitContext().AddVariableDeclaration(result)
		expressions = append(expressions, tx.Factory().NewAssignmentExpression(result, expr))
	} else {
		expressions = append(expressions, expr)
	}
	for _, temp := range frame.released {
		expressions = append(expressions, tx.call(transformers.OwnRuntimeReleaseHelper, temp))
	}
	if result != nil {
		expressions = append(expressions, result)
	}
	return tx.paren(tx.Factory().InlineExpressions(expressions))
}

func (tx *OwnLayoutTransformer) pushFrame(frame *ownFrame) *ownFrame {
	if frame != nil {
		frame.conditional = tx.conditional
	}
	tx.frames = append(tx.frames, frame)
	tx.conditional = 0
	return frame
}

func (tx *OwnLayoutTransformer) popFrame() {
	frame := tx.frames[len(tx.frames)-1]
	tx.frames = tx.frames[:len(tx.frames)-1]
	if frame != nil {
		tx.conditional = frame.conditional
	} else {
		tx.conditional = 0
	}
}

// `E;`: an owned result is released; the temporaries are released by statements that follow.
func (tx *OwnLayoutTransformer) visitExpressionStatement(node *ast.Node) *ast.Node {
	n := node.AsExpressionStatement()
	frame := tx.pushFrame(&ownFrame{})
	expression := tx.visitIn(n.Expression, ownDiscard)
	tx.popFrame()
	statement := tx.Factory().UpdateExpressionStatement(n, expression)
	if len(frame.released) == 0 {
		return statement
	}
	var statements []*ast.Node
	for _, temp := range frame.zeroed {
		statements = append(statements, tx.Factory().NewExpressionStatement(tx.Factory().NewAssignmentExpression(temp, tx.number(0))))
	}
	statements = append(statements, statement)
	for _, temp := range frame.released {
		statements = append(statements, tx.Factory().NewExpressionStatement(tx.call(transformers.OwnRuntimeReleaseHelper, temp)))
	}
	return tx.Factory().NewSyntaxList(statements)
}

// A variable declaration: an owned local takes its initial value (+1), and a counted local without
// one starts at 0.
func (tx *OwnLayoutTransformer) visitVariableDeclaration(node *ast.Node) *ast.Node {
	n := node.AsVariableDeclaration()
	local := printer.OwnLocalNone
	inForInOrOf := false
	if original := tx.original(node); original != nil && ast.IsVariableDeclaration(original) {
		local = tx.emitResolver.GetOwnLocal(original)
		inForInOrOf = original.Parent != nil && ast.IsForInOrOfStatement(original.Parent.Parent)
	}
	ctx := ownAtOnce
	switch local {
	case printer.OwnLocalOwned, printer.OwnLocalOwnedMoved, printer.OwnLocalModule:
		ctx = ownTake
	}
	initializer := tx.fullExpression(n.Initializer, ctx)
	name := tx.Visitor().VisitNode(n.Name())
	if initializer == nil && local != printer.OwnLocalNone && !inForInOrOf {
		initializer = tx.number(0)
		if ast.IsIdentifier(name) {
			// A name without a position, so that the printer does not look for comments after it.
			fresh := tx.Factory().NewIdentifier(name.Text())
			tx.EmitContext().SetOriginal(fresh, name)
			name = fresh
		}
	}
	return tx.Factory().UpdateVariableDeclaration(n, name, n.ExclamationToken, n.Type, initializer)
}

// `const` becomes `let` when a return moves an owned local's count out (and zeroes the local).
func (tx *OwnLayoutTransformer) visitVariableDeclarationList(node *ast.Node) *ast.Node {
	result := tx.Visitor().VisitEachChild(node)
	if node.Flags&ast.NodeFlagsConst == 0 {
		return result
	}
	for _, decl := range node.AsVariableDeclarationList().Declarations.Nodes {
		if original := tx.original(decl); original != nil && tx.emitResolver.GetOwnLocal(original) == printer.OwnLocalOwnedMoved {
			flags := (result.Flags &^ ast.NodeFlagsBlockScoped) | ast.NodeFlagsLet
			n := result.AsVariableDeclarationList()
			updated := tx.Factory().UpdateVariableDeclarationList(n, n.Declarations, flags)
			updated.Flags = flags // an update keeps the flags of the node it updates
			return updated
		}
	}
	return result
}

// A parameter: the default value of an owned parameter moves into the function's body.
func (tx *OwnLayoutTransformer) visitParameter(node *ast.Node) *ast.Node {
	n := node.AsParameterDeclaration()
	if original := tx.original(node); original != nil && n.Initializer != nil {
		switch tx.emitResolver.GetOwnLocal(original) {
		case printer.OwnLocalOwned, printer.OwnLocalOwnedMoved:
			tx.paramInits[original] = n.Initializer
			return tx.Factory().UpdateParameterDeclaration(n, tx.Visitor().VisitModifiers(n.Modifiers()), n.DotDotDotToken, tx.Visitor().VisitNode(n.Name()), n.QuestionToken, n.Type, nil /*initializer*/)
		}
	}
	return tx.Factory().UpdateParameterDeclaration(n, tx.Visitor().VisitModifiers(n.Modifiers()), n.DotDotDotToken, tx.Visitor().VisitNode(n.Name()), n.QuestionToken, n.Type, tx.fullExpression(n.Initializer, ownAtOnce))
}

// The function whose concise body an expression is, if any.
func (tx *OwnLayoutTransformer) conciseBodyOf(node *ast.Node) *ast.Node {
	original := tx.original(node)
	if original == nil || original.Parent == nil || !ast.IsArrowFunction(original.Parent) || original.Parent.Body() != original {
		return nil
	}
	return original.Parent
}

// `=> E`: a function's result, or a block if the function owns a parameter.
func (tx *OwnLayoutTransformer) visitConciseBody(node *ast.Node, fn *ast.Node) *ast.Node {
	prologue, owned := tx.functionPrologue(fn)
	frame := tx.pushFrame(&ownFrame{})
	result := tx.visitExpression(node, ownReturn)
	tx.popFrame()
	result = tx.releaseAfter(result, frame, true /*keepValue*/)
	if len(prologue) == 0 {
		return result
	}
	statements := []*ast.Node{tx.Factory().NewReturnStatement(result)}
	if len(owned) > 0 {
		statements = []*ast.Node{tx.tryFinally(statements, owned)}
	}
	return tx.Factory().NewBlock(tx.Factory().NewNodeList(append(prologue, statements...)), true /*multiLine*/)
}

// The statements a function body starts with for its counted parameters: an owned parameter is
// retained (or takes its default value), and an optional one that is omitted is 0. Returns the
// names of the owned parameters, which the body releases.
func (tx *OwnLayoutTransformer) functionPrologue(fn *ast.Node) (prologue []*ast.Node, owned []*ast.Node) {
	for _, param := range fn.Parameters() {
		name := param.Name()
		if name == nil || !ast.IsIdentifier(name) {
			continue
		}
		switch tx.emitResolver.GetOwnLocal(param) {
		case printer.OwnLocalOwned, printer.OwnLocalOwnedMoved:
			value := tx.call(transformers.OwnRuntimeRetainHelper, tx.name(name))
			if init := tx.paramInits[param]; init != nil {
				value = tx.cond(tx.Factory().NewStrictEqualityExpression(tx.name(name), tx.Factory().NewVoidZeroExpression()), tx.fullExpression(init, ownTake), value)
			}
			prologue = append(prologue, tx.Factory().NewExpressionStatement(tx.Factory().NewAssignmentExpression(tx.name(name), value)))
			owned = append(owned, name)
		case printer.OwnLocalBorrowed:
			if param.AsParameterDeclaration().QuestionToken != nil {
				assign := tx.binary(tx.name(name), ast.KindQuestionQuestionEqualsToken, tx.number(0))
				prologue = append(prologue, tx.Factory().NewExpressionStatement(assign))
			}
		}
	}
	return prologue, owned
}

// A block: owned locals are released when it ends, and a function body retains and releases its
// owned parameters.
func (tx *OwnLayoutTransformer) visitBlock(node *ast.Node) *ast.Node {
	n := node.AsBlock()
	var prologue, owned []*ast.Node
	statements := n.Statements.Nodes
	if original := tx.original(node); original != nil && original.Parent != nil && ast.IsFunctionLikeDeclaration(original.Parent) && original.Parent.Body() == original {
		prologue, owned = tx.functionPrologue(original.Parent)
	}
	var directives []*ast.Node
	if len(prologue) > 0 {
		for len(statements) > 0 && ast.IsPrologueDirective(statements[0]) {
			directives = append(directives, statements[0])
			statements = statements[1:]
		}
	}
	result := tx.visitStatements(statements)
	if len(owned) > 0 {
		functions, rest := ownSplitFunctionDeclarations(result)
		result = append(functions, tx.tryFinally(rest, owned))
	}
	if len(directives) > 0 || len(prologue) > 0 {
		result = append(append(directives, prologue...), result...)
	}
	return tx.Factory().UpdateBlock(n, tx.statementList(result, n.Statements), n.MultiLine)
}

// Visits a block's statements: the statements after a declaration of owned locals are wrapped in
// `try { ... } finally { __release(x) }`, and a nested function declaration that captures counted
// variables is held (`__hold(f, x)`) as soon as they are declared.
func (tx *OwnLayoutTransformer) visitStatements(statements []*ast.Node) []*ast.Node {
	holds := tx.holdStatements(statements)
	type level struct {
		statements []*ast.Node
		owned      []*ast.Node
	}
	levels := []*level{{}}
	top := levels[0]
	top.statements = append(top.statements, holds[-1]...)
	for i, statement := range statements {
		visited := tx.visit(statement)
		if visited != nil {
			if visited.Kind == ast.KindSyntaxList {
				top.statements = append(top.statements, visited.AsSyntaxList().Children...)
			} else {
				top.statements = append(top.statements, visited)
			}
		}
		if owned := tx.ownedLocalsOf(statement); len(owned) > 0 {
			top = &level{owned: owned}
			levels = append(levels, top)
		}
		top.statements = append(top.statements, holds[i]...)
	}
	for i := len(levels) - 1; i > 0; i-- {
		inner, outer := levels[i], levels[i-1]
		functions, rest := ownSplitFunctionDeclarations(inner.statements)
		outer.statements = append(outer.statements, functions...)
		if len(rest) == 0 {
			outer.statements = append(outer.statements, tx.releases(inner.owned)...)
		} else {
			outer.statements = append(outer.statements, tx.tryFinally(rest, inner.owned))
		}
	}
	return levels[0].statements
}

// Function declarations move ahead of a `try` (they are hoisted anyway).
func ownSplitFunctionDeclarations(statements []*ast.Node) (functions []*ast.Node, rest []*ast.Node) {
	for _, statement := range statements {
		if ast.IsFunctionDeclaration(statement) {
			functions = append(functions, statement)
		} else {
			rest = append(rest, statement)
		}
	}
	return functions, rest
}

// The names of the owned locals a statement declares.
func (tx *OwnLayoutTransformer) ownedLocalsOf(statement *ast.Node) []*ast.Node {
	original := tx.original(statement)
	if original == nil || !ast.IsVariableStatement(original) {
		return nil
	}
	return tx.ownedLocalsOfList(original.AsVariableStatement().DeclarationList)
}

func (tx *OwnLayoutTransformer) ownedLocalsOfList(list *ast.Node) []*ast.Node {
	var owned []*ast.Node
	for _, decl := range list.AsVariableDeclarationList().Declarations.Nodes {
		switch tx.emitResolver.GetOwnLocal(decl) {
		case printer.OwnLocalOwned, printer.OwnLocalOwnedMoved:
			owned = append(owned, decl.Name())
		}
	}
	return owned
}

// The `__hold(f, x, ...)` statements for the function declarations among statements that capture
// counted variables, by the index of the statement they follow (-1: the start): after the last of
// those variables declared among the statements.
func (tx *OwnLayoutTransformer) holdStatements(statements []*ast.Node) map[int][]*ast.Node {
	var holds map[int][]*ast.Node
	for _, statement := range statements {
		original := tx.original(statement)
		if original == nil || !ast.IsFunctionDeclaration(original) || original.Name() == nil {
			continue
		}
		captures := tx.emitResolver.GetOwnCaptures(original)
		if len(captures) == 0 {
			continue
		}
		after := -1
		for i, s := range statements {
			if o := tx.original(s); o != nil && ast.IsVariableStatement(o) {
				for _, decl := range o.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes {
					if slices.Contains(captures, decl) {
						after = max(after, i)
					}
				}
			}
		}
		args := []*ast.Node{tx.name(original.Name())}
		for _, decl := range captures {
			args = append(args, tx.name(decl.Name()))
		}
		if holds == nil {
			holds = map[int][]*ast.Node{}
		}
		holds[after] = append(holds[after], tx.Factory().NewExpressionStatement(tx.call(transformers.OwnRuntimeHoldHelper, args...)))
	}
	return holds
}

func (tx *OwnLayoutTransformer) releases(owned []*ast.Node) []*ast.Node {
	var statements []*ast.Node
	for i := len(owned) - 1; i >= 0; i-- {
		statements = append(statements, tx.Factory().NewExpressionStatement(tx.call(transformers.OwnRuntimeReleaseHelper, tx.name(owned[i]))))
	}
	return statements
}

func (tx *OwnLayoutTransformer) tryFinally(statements []*ast.Node, owned []*ast.Node) *ast.Node {
	return tx.Factory().NewTryStatement(
		tx.Factory().NewBlock(tx.Factory().NewNodeList(statements), true /*multiLine*/),
		nil, /*catchClause*/
		tx.Factory().NewBlock(tx.Factory().NewNodeList(tx.releases(owned)), true /*multiLine*/),
	)
}

func (tx *OwnLayoutTransformer) statementList(statements []*ast.Node, original *ast.NodeList) *ast.NodeList {
	list := tx.Factory().NewNodeList(statements)
	if original != nil {
		list.Loc = original.Loc
	}
	return list
}

// The owned locals a `for` statement declares in its head.
func (tx *OwnLayoutTransformer) ownedLocalsOfFor(node *ast.Node) []*ast.Node {
	original := tx.original(node)
	if original == nil || !ast.IsForStatement(original) {
		return nil
	}
	init := original.AsForStatement().Initializer
	if init == nil || !ast.IsVariableDeclarationList(init) {
		return nil
	}
	return tx.ownedLocalsOfList(init)
}

// `for (init; cond; update) body`. Owned locals declared in the head move out of the loop, which is
// wrapped in `try { ... } finally { __release(x) }`.
func (tx *OwnLayoutTransformer) visitForStatement(node *ast.Node, label *ast.Node) *ast.Node {
	n := node.AsForStatement()
	owned := tx.ownedLocalsOfFor(node)
	var initializer *ast.Node
	if n.Initializer != nil && ast.IsVariableDeclarationList(n.Initializer) {
		initializer = tx.Visitor().VisitNode(n.Initializer)
	} else {
		initializer = tx.fullExpression(n.Initializer, ownDiscard)
	}
	condition := tx.fullExpression(n.Condition, ownAtOnce)
	incrementor := tx.fullExpression(n.Incrementor, ownDiscard)
	body := tx.visitLoopBody(n.Statement, node)
	if len(owned) == 0 {
		loop := tx.Factory().UpdateForStatement(n, initializer, condition, incrementor, body)
		if label != nil {
			return tx.Factory().NewLabeledStatement(label, loop)
		}
		return loop
	}
	loop := tx.Factory().UpdateForStatement(n, nil /*initializer*/, condition, incrementor, body)
	if label != nil {
		loop = tx.Factory().NewLabeledStatement(label, loop)
	}
	declarations := tx.Factory().NewVariableStatement(nil /*modifiers*/, initializer)
	return tx.Factory().NewBlock(tx.Factory().NewNodeList([]*ast.Node{declarations, tx.tryFinally([]*ast.Node{loop}, owned)}), true /*multiLine*/)
}

// A loop's body, which ends at a quiescent point if the loop reads or stores a reference field.
func (tx *OwnLayoutTransformer) visitLoopBody(body *ast.Node, loop *ast.Node) *ast.Node {
	visited := tx.EmitContext().VisitIterationBody(body, tx.Visitor())
	original := tx.original(loop)
	if original == nil || !tx.emitResolver.IsOwnQuiescentLoop(original) {
		return visited
	}
	if ast.IsBlock(visited) {
		return tx.Factory().UpdateBlock(visited.AsBlock(), tx.statementList(append(slices.Clone(visited.Statements()), tx.quiescent()), visited.StatementList()), true /*multiLine*/)
	}
	return tx.Factory().NewBlock(tx.Factory().NewNodeList([]*ast.Node{visited, tx.quiescent()}), true /*multiLine*/)
}

func (tx *OwnLayoutTransformer) quiescent() *ast.Node {
	return tx.Factory().NewExpressionStatement(tx.call(transformers.OwnRuntimeQuiescentHelper))
}

// The loop a `continue` statement of the original tree continues.
func ownContinueTarget(node *ast.Node) *ast.Node {
	if node == nil {
		return nil
	}
	label := node.AsContinueStatement().Label
	for parent := node.Parent; parent != nil && !ast.IsFunctionLike(parent) && !ast.IsSourceFile(parent); parent = parent.Parent {
		if label != nil {
			if ast.IsLabeledStatement(parent) && parent.AsLabeledStatement().Label.Text() == label.Text() {
				statement := parent.AsLabeledStatement().Statement
				for ast.IsLabeledStatement(statement) {
					statement = statement.AsLabeledStatement().Statement
				}
				return statement
			}
		} else if ast.IsIterationStatement(parent, false /*lookInLabeledStatements*/) {
			return parent
		}
	}
	return nil
}

// An arrow function or function expression; one that captures counted variables is held:
// `__hold(fn, x, ...)`.
func (tx *OwnLayoutTransformer) visitClosure(node *ast.Node) *ast.Node {
	tx.pushFrame(nil /*fence*/)
	result := tx.Visitor().VisitEachChild(node)
	tx.popFrame()
	original := tx.original(node)
	if original == nil {
		return result
	}
	captures := tx.emitResolver.GetOwnCaptures(original)
	if len(captures) == 0 {
		return result
	}
	args := []*ast.Node{result}
	for _, decl := range captures {
		args = append(args, tx.name(decl.Name()))
	}
	return tx.finish(tx.call(transformers.OwnRuntimeHoldHelper, args...), node)
}

func (tx *OwnLayoutTransformer) lower(node *ast.Node, ctx ownContext) *ast.Node {
	switch node.Kind {
	case ast.KindNewExpression:
		if lowered := tx.lowerNew(node); lowered != nil {
			return tx.finish(lowered, node)
		}
		n := node.AsNewExpression()
		return tx.Factory().UpdateNewExpression(n, tx.Visitor().VisitNode(n.Expression), nil /*typeArguments*/, tx.visitArguments(n.Arguments, false /*conditional*/))
	case ast.KindCallExpression:
		n := node.AsCallExpression()
		callee := tx.Visitor().VisitNode(n.Expression)
		original := tx.original(node)
		args := tx.visitArguments(n.Arguments, original != nil && ast.IsOptionalChain(original))
		return tx.Factory().UpdateCallExpression(n, callee, n.QuestionDotToken, nil /*typeArguments*/, args, n.Flags)
	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		if lowered := tx.lowerAccess(node, false /*isWrite*/, nil /*value*/); lowered != nil {
			return tx.finish(lowered, node)
		}
	case ast.KindBinaryExpression:
		n := node.AsBinaryExpression()
		if ast.IsAssignmentOperator(n.OperatorToken.Kind) {
			if lowered := tx.lowerAssignment(node, ctx); lowered != nil {
				return lowered
			}
		}
	case ast.KindPrefixUnaryExpression:
		n := node.AsPrefixUnaryExpression()
		if n.Operator == ast.KindPlusPlusToken || n.Operator == ast.KindMinusMinusToken {
			if operand := tx.lowerAccess(n.Operand, true /*isWrite*/, nil /*value*/); operand != nil {
				return tx.Factory().UpdatePrefixUnaryExpression(n, n.Operator, tx.finish(operand, n.Operand))
			}
		}
	case ast.KindPostfixUnaryExpression:
		n := node.AsPostfixUnaryExpression()
		if operand := tx.lowerAccess(n.Operand, true /*isWrite*/, nil /*value*/); operand != nil {
			return tx.Factory().UpdatePostfixUnaryExpression(n, tx.finish(operand, n.Operand), n.Operator)
		}
	case ast.KindVoidExpression:
		return tx.Factory().UpdateVoidExpression(node.AsVoidExpression(), tx.visitIn(node.Expression(), ownDiscard))
	case ast.KindShorthandPropertyAssignment:
		// `{ b }` where b is boxed or unboxed: `{ b: __box(b) }`.
		n := node.AsShorthandPropertyAssignment()
		if original := tx.original(n.Name()); original != nil && tx.emitResolver.GetOwnConversion(original) != printer.OwnConversionNone {
			value := tx.visitExpressionOf(n.Name())
			assignment := tx.Factory().NewPropertyAssignment(nil /*modifiers*/, n.Name().Clone(tx.Factory()), nil /*postfixToken*/, nil /*typeNode*/, value)
			return tx.finish(assignment, node)
		}
	}
	return tx.Visitor().VisitEachChild(node)
}

// The converted value of a shorthand property's name (`__box(b)` for `{ b }`).
func (tx *OwnLayoutTransformer) visitExpressionOf(name *ast.Node) *ast.Node {
	clone := name.Clone(tx.Factory())
	tx.EmitContext().SetOriginal(clone, name)
	return tx.visitExpression(clone, ownAtOnce)
}

// The arguments of a call: a counted argument of `spawn` is `__ref(E)`; an unstable borrow is retained
// around the call.
func (tx *OwnLayoutTransformer) visitArguments(args *ast.NodeList, conditional bool) *ast.NodeList {
	if args == nil {
		return nil
	}
	if conditional {
		tx.conditional++
		defer func() { tx.conditional-- }()
	}
	var visited []*ast.Node
	for _, arg := range args.Nodes {
		switch original := tx.original(arg); {
		case ast.IsSpreadElement(arg):
			visited = append(visited, tx.Visitor().VisitNode(arg))
		case original != nil && tx.emitResolver.IsOwnSpawnArgument(original):
			visited = append(visited, tx.finish(tx.call(transformers.OwnRuntimeRefHelper, tx.visitIn(arg, ownAtOnce)), arg))
		default:
			visited = append(visited, tx.unparen(tx.visitIn(arg, ownArgument)))
		}
	}
	list := tx.Factory().NewNodeList(visited)
	list.Loc = args.Loc
	return list
}

// Assignments to struct fields, SharedArray elements and counted variables; nil for any other.
func (tx *OwnLayoutTransformer) lowerAssignment(node *ast.Node, ctx ownContext) *ast.Node {
	n := node.AsBinaryExpression()
	operator := n.OperatorToken.Kind
	left := ast.SkipParentheses(n.Left)
	originalLeft := tx.original(left)
	if originalLeft != nil && ast.IsPropertyAccessExpression(originalLeft) && tx.isReferenceField(originalLeft) {
		// __store((E >> 2) + 2 + 2k, V'), V' taking a count
		access := tx.lowerAccess(left, true /*isWrite*/, n.Right)
		if access == nil {
			return nil
		}
		index := access.AsElementAccessExpression().ArgumentExpression
		switch operator {
		case ast.KindEqualsToken:
			return tx.finish(tx.call(transformers.OwnRuntimeStoreHelper, index, tx.visitIn(n.Right, ownTake)), node)
		case ast.KindBarBarEqualsToken, ast.KindQuestionQuestionEqualsToken, ast.KindAmpersandAmpersandEqualsToken:
			var temps []*ast.Node
			i := tx.copiable(index, &temps)
			tx.conditional++
			value := tx.visitIn(n.Right, ownTake)
			tx.conditional--
			logical := ast.KindBarBarToken
			if operator == ast.KindAmpersandAmpersandEqualsToken {
				logical = ast.KindAmpersandAmpersandToken
			}
			read := tx.i32(tx.dup(i))
			return tx.finish(tx.sequence(temps, tx.binary(read, logical, tx.call(transformers.OwnRuntimeStoreHelper, tx.dup(i), value))), node)
		}
		return nil
	}
	if lowered := tx.lowerAccess(left, true /*isWrite*/, n.Right); lowered != nil {
		lowered = tx.finish(lowered, n.Left)
		right := tx.Visitor().VisitNode(n.Right)
		return tx.Factory().UpdateBinaryExpression(n, nil /*modifiers*/, lowered, nil /*typeNode*/, n.OperatorToken, right)
	}
	if originalLeft != nil && ast.IsIdentifier(left) {
		switch tx.emitResolver.GetOwnLocal(originalLeft) {
		case printer.OwnLocalOwned, printer.OwnLocalOwnedMoved, printer.OwnLocalModule:
			return tx.lowerLocalAssignment(node, left, ctx)
		}
	}
	return nil
}

// `x = E` for an owned local: the new value is retained before the old is released,
// `_t = x, x = E', __release(_t)`. `x ||= E` is `x || (x = E')` (x is 0 when it is null).
func (tx *OwnLayoutTransformer) lowerLocalAssignment(node *ast.Node, x *ast.Node, ctx ownContext) *ast.Node {
	n := node.AsBinaryExpression()
	operator := n.OperatorToken.Kind
	replace := func(value *ast.Node, keepValue bool) *ast.Node {
		old := tx.Factory().NewTempVariable()
		tx.EmitContext().AddVariableDeclaration(old)
		expressions := []*ast.Node{
			tx.Factory().NewAssignmentExpression(old, tx.dup(x)),
			tx.Factory().NewAssignmentExpression(tx.dup(x), value),
			tx.call(transformers.OwnRuntimeReleaseHelper, old),
		}
		if keepValue {
			expressions = append(expressions, tx.dup(x))
		}
		return tx.Factory().InlineExpressions(expressions)
	}
	switch operator {
	case ast.KindEqualsToken:
		result := replace(tx.visitIn(n.Right, ownTake), ctx != ownDiscard)
		if ctx != ownDiscard {
			result = tx.paren(result)
		}
		return tx.finish(result, node)
	case ast.KindBarBarEqualsToken, ast.KindQuestionQuestionEqualsToken:
		tx.conditional++
		value := tx.visitIn(n.Right, ownTake)
		tx.conditional--
		return tx.finish(tx.binary(tx.dup(x), ast.KindBarBarToken, tx.paren(tx.Factory().NewAssignmentExpression(tx.dup(x), value))), node)
	case ast.KindAmpersandAmpersandEqualsToken:
		tx.conditional++
		value := tx.visitIn(n.Right, ownTake)
		tx.conditional--
		return tx.finish(tx.binary(tx.dup(x), ast.KindAmpersandAmpersandToken, tx.paren(replace(value, true /*keepValue*/))), node)
	}
	return nil
}

func (tx *OwnLayoutTransformer) finish(lowered *ast.Node, node *ast.Node) *ast.Node {
	tx.EmitContext().SetOriginal(lowered, node)
	tx.EmitContext().AssignCommentAndSourceMapRanges(lowered, node)
	return lowered
}

// `new S()` and `new SharedArray(S | Number, n)`.
func (tx *OwnLayoutTransformer) lowerNew(node *ast.Node) *ast.Node {
	original := tx.original(node)
	if original == nil {
		return nil
	}
	n := node.AsNewExpression()
	switch cell := tx.emitResolver.GetOwnCellType(original); cell.Kind {
	case printer.OwnCellStruct:
		return tx.call(transformers.OwnRuntimeNewStructHelper, tx.Visitor().VisitNode(n.Expression))
	case printer.OwnCellArrayOfStruct, printer.OwnCellArrayOfNumber:
		if n.Arguments == nil || len(n.Arguments.Nodes) != 2 {
			return nil
		}
		elementType := tx.number(8)
		if cell.Kind == printer.OwnCellArrayOfStruct {
			elementType = tx.Visitor().VisitNode(n.Arguments.Nodes[0])
		}
		return tx.call(transformers.OwnRuntimeNewSharedArrayHelper, elementType, tx.Visitor().VisitNode(n.Arguments.Nodes[1]))
	}
	return nil
}

// Reports whether a property access of the original tree is a reference field of a shared struct.
func (tx *OwnLayoutTransformer) isReferenceField(access *ast.Node) bool {
	cell := tx.emitResolver.GetOwnCellType(access.Expression())
	if cell.Kind != printer.OwnCellStruct {
		return false
	}
	k := slices.Index(cell.Fields, access.Name().Text())
	return k >= 0 && cell.Refs[k]
}

// Lowers `e.f`, `a.length` and `a[i]` on structs and SharedArrays; nil if the access is not one. For a
// write, value is the assigned expression: when it contains a call, an unstable object is retained
// first (the call may reach a quiescent point), and so it is when the index of an element contains
// one.
func (tx *OwnLayoutTransformer) lowerAccess(node *ast.Node, isWrite bool, value *ast.Node) *ast.Node {
	node = ast.SkipParentheses(node)
	if !ast.IsPropertyAccessExpression(node) && !ast.IsElementAccessExpression(node) {
		return nil
	}
	original := tx.original(node)
	if original == nil || original.Kind != node.Kind || ast.IsOptionalChain(original) {
		return nil
	}
	cell := tx.emitResolver.GetOwnCellType(original.Expression())
	if cell.Kind == printer.OwnCellNone {
		return nil
	}
	objectContext := ownAtOnce
	if isWrite && tx.containsCall(value) || ast.IsElementAccessExpression(original) && tx.containsCall(original.AsElementAccessExpression().ArgumentExpression) {
		objectContext = ownArgument
	}
	if ast.IsPropertyAccessExpression(node) {
		name := original.Name().Text()
		switch cell.Kind {
		case printer.OwnCellStruct:
			k := slices.Index(cell.Fields, name)
			if k < 0 {
				return nil
			}
			object := tx.objectOf(node, objectContext)
			if ast.IsBinaryExpression(object) {
				object = tx.paren(object) // an unchecked element address: ((A + 16 + I * size) >> 3)
			}
			if cell.Refs[k] {
				return tx.i32(tx.add(tx.paren(tx.binary(object, ast.KindGreaterThanGreaterThanToken, tx.number(2))), tx.number(2+2*k)))
			}
			return tx.f64(tx.add(tx.paren(tx.binary(object, ast.KindGreaterThanGreaterThanToken, tx.number(3))), tx.number(1+k)))
		case printer.OwnCellArrayOfNumber, printer.OwnCellArrayOfStruct:
			if name != "length" || isWrite {
				return nil
			}
			return tx.length(tx.objectOf(node, objectContext))
		}
		return nil
	}
	if (cell.Kind == printer.OwnCellArrayOfStruct || cell.Kind == printer.OwnCellArrayOfNumber) && tx.emitResolver.IsOwnElementAccessInBounds(original) {
		// Proven in bounds (A and I are identifiers): no check, no temporaries.
		if cell.Kind == printer.OwnCellArrayOfStruct && isWrite {
			return nil // reported by the checker
		}
		a := tx.objectOf(node, objectContext)
		i := tx.Visitor().VisitNode(node.AsElementAccessExpression().ArgumentExpression)
		if cell.Kind == printer.OwnCellArrayOfStruct {
			return tx.add(tx.add(a, tx.number(16)), tx.binary(i, ast.KindAsteriskToken, tx.number(8+8*len(cell.Fields))))
		}
		return tx.f64(tx.add(tx.paren(tx.binary(tx.paren(tx.add(a, tx.number(16))), ast.KindGreaterThanGreaterThanToken, tx.number(3))), i))
	}
	switch cell.Kind {
	case printer.OwnCellArrayOfStruct:
		if isWrite {
			return nil // reported by the checker
		}
		var temps []*ast.Node
		a := tx.copiable(tx.objectOf(node, objectContext), &temps)
		i := tx.copiable(tx.Visitor().VisitNode(node.AsElementAccessExpression().ArgumentExpression), &temps)
		size := 8 + 8*len(cell.Fields)
		address := tx.cond(tx.inBounds(a, i), tx.add(tx.add(tx.dup(a), tx.number(16)), tx.binary(tx.dup(i), ast.KindAsteriskToken, tx.number(size))), tx.oob(i))
		return tx.sequence(temps, address)
	case printer.OwnCellArrayOfNumber:
		var temps []*ast.Node
		a := tx.copiable(tx.objectOf(node, objectContext), &temps)
		i := tx.copiable(tx.Visitor().VisitNode(node.AsElementAccessExpression().ArgumentExpression), &temps)
		base := tx.paren(tx.binary(tx.paren(tx.add(tx.dup(a), tx.number(16))), ast.KindGreaterThanGreaterThanToken, tx.number(3)))
		if isWrite {
			index := tx.paren(tx.cond(tx.inBounds(a, i), tx.dup(i), tx.oob(i)))
			return tx.f64(tx.sequence(temps, tx.add(base, index)))
		}
		read := tx.cond(tx.inBounds(a, i), tx.f64(tx.add(base, tx.dup(i))), tx.oob(i))
		return tx.sequence(temps, read)
	}
	return nil
}

// Reports whether evaluating an expression of the original tree may call a function (and so reach a
// quiescent point). Allocating a struct or a SharedArray does not.
func (tx *OwnLayoutTransformer) containsCall(node *ast.Node) bool {
	if node == nil {
		return false
	}
	original := tx.original(node)
	if original == nil {
		return true
	}
	found := false
	var visit ast.Visitor
	visit = func(n *ast.Node) bool {
		switch n.Kind {
		case ast.KindCallExpression, ast.KindTaggedTemplateExpression, ast.KindAwaitExpression, ast.KindYieldExpression:
			found = true
		case ast.KindNewExpression:
			found = tx.emitResolver.GetOwnCellType(n).Kind == printer.OwnCellNone
		case ast.KindArrowFunction, ast.KindFunctionExpression, ast.KindClassExpression:
			return false
		}
		return found || n.ForEachChild(visit)
	}
	visit(original)
	return found
}

// The object of a lowered access, visited (and so lowered and converted itself) in a context.
func (tx *OwnLayoutTransformer) objectOf(node *ast.Node, ctx ownContext) *ast.Node {
	return tx.visitIn(node.Expression(), ctx)
}

// Returns an expression that can be repeated: the expression itself if it is simple, otherwise a
// temporary that is assigned first (the assignment is appended to temps).
func (tx *OwnLayoutTransformer) copiable(expr *ast.Node, temps *[]*ast.Node) *ast.Node {
	if transformers.IsSimpleCopiableExpression(expr) {
		return expr
	}
	if assignment := tx.unparen(expr); assignment != expr && tx.EmitContext().HasAutoGenerateInfo(assignment.AsBinaryExpression().Left) {
		// Already assigned to a temporary (one that holds a count): reuse it.
		*temps = append(*temps, assignment)
		return assignment.AsBinaryExpression().Left
	}
	temp := tx.Factory().NewTempVariable()
	tx.EmitContext().AddVariableDeclaration(temp)
	*temps = append(*temps, tx.Factory().NewAssignmentExpression(temp, expr))
	return temp
}

// Another use of a repeated operand: temporaries are reused (they are generated names), other simple
// expressions are cloned.
func (tx *OwnLayoutTransformer) dup(expr *ast.Node) *ast.Node {
	if tx.EmitContext().HasAutoGenerateInfo(expr) {
		return expr
	}
	clone := expr.Clone(tx.Factory())
	// The comments around the operand belong to the access as a whole.
	tx.EmitContext().AddEmitFlags(clone, printer.EFNoComments)
	return clone
}

// A new reference to a variable of the original tree, by name.
func (tx *OwnLayoutTransformer) name(name *ast.Node) *ast.Node {
	return tx.dup(name)
}

func (tx *OwnLayoutTransformer) sequence(temps []*ast.Node, expr *ast.Node) *ast.Node {
	if len(temps) == 0 {
		return expr
	}
	return tx.Factory().InlineExpressions(append(slices.Clone(temps), expr))
}

// (I >>> 0) === I && I < __I32[(A + 4) >> 2]: an integer index in range (`>>> 0` maps negative and
// fractional indexes to other numbers).
func (tx *OwnLayoutTransformer) inBounds(a *ast.Node, i *ast.Node) *ast.Node {
	isIndex := tx.binary(tx.paren(tx.binary(tx.dup(i), ast.KindGreaterThanGreaterThanGreaterThanToken, tx.number(0))), ast.KindEqualsEqualsEqualsToken, tx.dup(i))
	return tx.binary(isIndex, ast.KindAmpersandAmpersandToken, tx.binary(tx.dup(i), ast.KindLessThanToken, tx.length(tx.dup(a))))
}

// __I32[(A + 4) >> 2]
func (tx *OwnLayoutTransformer) length(a *ast.Node) *ast.Node {
	return tx.i32(tx.binary(tx.paren(tx.add(a, tx.number(4))), ast.KindGreaterThanGreaterThanToken, tx.number(2)))
}

// __OOB[I]: throws a RangeError naming the index, without a call.
func (tx *OwnLayoutTransformer) oob(i *ast.Node) *ast.Node {
	return tx.Factory().NewElementAccessExpression(tx.helper(transformers.OwnRuntimeOutOfBoundsHelper), nil /*questionDotToken*/, tx.dup(i), ast.NodeFlagsNone)
}

func (tx *OwnLayoutTransformer) f64(index *ast.Node) *ast.Node {
	return tx.Factory().NewElementAccessExpression(tx.helper(transformers.OwnRuntimeF64Helper), nil /*questionDotToken*/, index, ast.NodeFlagsNone)
}

func (tx *OwnLayoutTransformer) i32(index *ast.Node) *ast.Node {
	return tx.Factory().NewElementAccessExpression(tx.helper(transformers.OwnRuntimeI32Helper), nil /*questionDotToken*/, index, ast.NodeFlagsNone)
}

func (tx *OwnLayoutTransformer) helper(name string) *ast.Node {
	return tx.Factory().NewUnscopedHelperName(name)
}

func (tx *OwnLayoutTransformer) call(helper string, args ...*ast.Node) *ast.Node {
	args = core.Map(args, tx.unparen)
	return tx.Factory().NewCallExpression(tx.helper(helper), nil /*questionDotToken*/, nil /*typeArguments*/, tx.Factory().NewNodeList(args), ast.NodeFlagsNone)
}

func (tx *OwnLayoutTransformer) number(n int) *ast.Node {
	return tx.Factory().NewNumericLiteral(strconv.Itoa(n), ast.TokenFlagsNone)
}

func (tx *OwnLayoutTransformer) binary(left *ast.Node, operator ast.Kind, right *ast.Node) *ast.Node {
	return tx.Factory().NewBinaryExpression(nil /*modifiers*/, left, nil /*typeNode*/, tx.Factory().NewToken(operator), right)
}

func (tx *OwnLayoutTransformer) add(left *ast.Node, right *ast.Node) *ast.Node {
	return tx.binary(left, ast.KindPlusToken, right)
}

// Parentheses that the printer would not need, to keep the emitted shapes readable.
func (tx *OwnLayoutTransformer) paren(expr *ast.Node) *ast.Node {
	return tx.Factory().NewParenthesizedExpression(expr)
}

func (tx *OwnLayoutTransformer) cond(condition *ast.Node, whenTrue *ast.Node, whenFalse *ast.Node) *ast.Node {
	return tx.Factory().NewConditionalExpression(condition, tx.Factory().NewToken(ast.KindQuestionToken), whenTrue, tx.Factory().NewToken(ast.KindColonToken), whenFalse)
}
