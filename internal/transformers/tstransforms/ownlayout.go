package tstransforms

import (
	"slices"
	"strconv"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/printer"
	"github.com/microsoft/typescript-go/internal/transformers"
)

// Lowers the typed layouts of the own/shared threads extension (with `--ownRuntime`). A value whose
// type is a shared struct or a SharedArray is the byte address of its block in the runtime's shared
// heap, which `__F64` (a Float64Array) and `__I32` (an Int32Array) view:
//
//	shared struct S { f0; ...; f(n-1) }   [0: i32 kind][4: unused][8: f0][16: f1]...    size 8 + 8n
//	SharedArray<E>                        [0: i32 kind][4: i32 length][8: i32 element size][12: unused]
//	                                      then the elements from byte 16: f64s, or whole struct blocks
//
//	new S()                    __newStruct(S)
//	new SharedArray(S, n)      __newSharedArray(S, n)
//	new SharedArray(Number, n) __newSharedArray(8, n)
//	e.fk (read or write)       __F64[(E >> 3) + 1 + k]                  (1 + k folded)
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
// `__box(E)` or `__unbox(E)` (see checker/ownlayout.go). The helpers are imported from the
// `ownRuntime` module by the ES module transform.
type OwnLayoutTransformer struct {
	transformers.Transformer
	compilerOptions           *core.CompilerOptions
	emitResolver              printer.EmitResolver
	getEmitModuleFormatOfFile func(file ast.HasFileName) core.ModuleKind
}

func NewOwnLayoutTransformer(opt *transformers.TransformOptions) *transformers.Transformer {
	tx := &OwnLayoutTransformer{compilerOptions: opt.CompilerOptions, emitResolver: opt.EmitResolver, getEmitModuleFormatOfFile: opt.GetEmitModuleFormatOfFile}
	return tx.NewTransformer(tx.visit, opt.Context)
}

func (tx *OwnLayoutTransformer) visit(node *ast.Node) *ast.Node {
	if node.Kind == ast.KindSourceFile {
		if tx.emitResolver == nil || !transformers.UsesOwnRuntime(tx.EmitContext(), tx.compilerOptions, node.AsSourceFile(), tx.getEmitModuleFormatOfFile) {
			return node
		}
		return tx.Visitor().VisitEachChild(node)
	}
	result := tx.lower(node)
	if ast.IsExpression(node) {
		result = tx.convert(node, result)
	}
	return result
}

// The parse tree node a (possibly transformed) node came from.
func (tx *OwnLayoutTransformer) original(node *ast.Node) *ast.Node {
	original := tx.EmitContext().MostOriginal(node)
	if original == nil || original.Flags&ast.NodeFlagsSynthesized != 0 {
		return nil
	}
	return original
}

// Wraps an expression in `__box` or `__unbox` if the checker says it crosses representations.
func (tx *OwnLayoutTransformer) convert(node *ast.Node, result *ast.Node) *ast.Node {
	original := tx.original(node)
	if original == nil || result == nil {
		return result
	}
	var helper string
	switch tx.emitResolver.GetOwnConversion(original) {
	case printer.OwnConversionBox:
		helper = transformers.OwnRuntimeBoxHelper
	case printer.OwnConversionUnbox:
		helper = transformers.OwnRuntimeUnboxHelper
	default:
		return result
	}
	converted := tx.call(helper, result)
	tx.EmitContext().SetOriginal(converted, node)
	tx.EmitContext().AssignCommentAndSourceMapRanges(converted, node)
	return converted
}

func (tx *OwnLayoutTransformer) lower(node *ast.Node) *ast.Node {
	switch node.Kind {
	case ast.KindNewExpression:
		if lowered := tx.lowerNew(node); lowered != nil {
			return tx.finish(lowered, node)
		}
	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		if lowered := tx.lowerAccess(node, false /*isWrite*/); lowered != nil {
			return tx.finish(lowered, node)
		}
	case ast.KindBinaryExpression:
		n := node.AsBinaryExpression()
		if ast.IsAssignmentOperator(n.OperatorToken.Kind) {
			if left := tx.lowerAccess(n.Left, true /*isWrite*/); left != nil {
				left = tx.finish(left, n.Left)
				right := tx.Visitor().VisitNode(n.Right)
				return tx.Factory().UpdateBinaryExpression(n, nil /*modifiers*/, left, nil /*typeNode*/, n.OperatorToken, right)
			}
		}
	case ast.KindPrefixUnaryExpression:
		n := node.AsPrefixUnaryExpression()
		if n.Operator == ast.KindPlusPlusToken || n.Operator == ast.KindMinusMinusToken {
			if operand := tx.lowerAccess(n.Operand, true /*isWrite*/); operand != nil {
				return tx.Factory().UpdatePrefixUnaryExpression(n, n.Operator, tx.finish(operand, n.Operand))
			}
		}
	case ast.KindPostfixUnaryExpression:
		n := node.AsPostfixUnaryExpression()
		if operand := tx.lowerAccess(n.Operand, true /*isWrite*/); operand != nil {
			return tx.Factory().UpdatePostfixUnaryExpression(n, tx.finish(operand, n.Operand), n.Operator)
		}
	case ast.KindShorthandPropertyAssignment:
		// `{ b }` where b is boxed or unboxed: `{ b: __box(b) }`.
		n := node.AsShorthandPropertyAssignment()
		if original := tx.original(n.Name()); original != nil && tx.emitResolver.GetOwnConversion(original) != printer.OwnConversionNone {
			value := tx.convert(n.Name(), n.Name().Clone(tx.Factory()))
			assignment := tx.Factory().NewPropertyAssignment(nil /*modifiers*/, n.Name().Clone(tx.Factory()), nil /*postfixToken*/, nil /*typeNode*/, value)
			return tx.finish(assignment, node)
		}
	}
	return tx.Visitor().VisitEachChild(node)
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

// Lowers `e.f`, `a.length` and `a[i]` on structs and SharedArrays; nil if the access is not one.
func (tx *OwnLayoutTransformer) lowerAccess(node *ast.Node, isWrite bool) *ast.Node {
	node = ast.SkipParentheses(node)
	if !ast.IsPropertyAccessExpression(node) && !ast.IsElementAccessExpression(node) {
		return nil
	}
	original := tx.original(node)
	if original == nil || original.Kind != node.Kind || ast.IsOptionalChain(original) {
		return nil
	}
	cell := tx.emitResolver.GetOwnCellType(original.Expression())
	if ast.IsPropertyAccessExpression(node) {
		name := original.Name().Text()
		switch cell.Kind {
		case printer.OwnCellStruct:
			k := slices.Index(cell.Fields, name)
			if k < 0 {
				return nil
			}
			object := tx.objectOf(node)
			if ast.IsBinaryExpression(object) {
				object = tx.paren(object) // an unchecked element address: ((A + 16 + I * size) >> 3)
			}
			return tx.f64(tx.add(tx.paren(tx.binary(object, ast.KindGreaterThanGreaterThanToken, tx.number(3))), tx.number(1+k)))
		case printer.OwnCellArrayOfNumber, printer.OwnCellArrayOfStruct:
			if name != "length" || isWrite {
				return nil
			}
			return tx.length(tx.objectOf(node))
		}
		return nil
	}
	if (cell.Kind == printer.OwnCellArrayOfStruct || cell.Kind == printer.OwnCellArrayOfNumber) && tx.emitResolver.IsOwnElementAccessInBounds(original) {
		// Proven in bounds (A and I are identifiers): no check, no temporaries.
		if cell.Kind == printer.OwnCellArrayOfStruct && isWrite {
			return nil // reported by the checker
		}
		a := tx.objectOf(node)
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
		a := tx.copiable(tx.objectOf(node), &temps)
		i := tx.copiable(tx.Visitor().VisitNode(node.AsElementAccessExpression().ArgumentExpression), &temps)
		size := 8 + 8*len(cell.Fields)
		address := tx.cond(tx.inBounds(a, i), tx.add(tx.add(tx.dup(a), tx.number(16)), tx.binary(tx.dup(i), ast.KindAsteriskToken, tx.number(size))), tx.oob(i))
		return tx.sequence(temps, address)
	case printer.OwnCellArrayOfNumber:
		var temps []*ast.Node
		a := tx.copiable(tx.objectOf(node), &temps)
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

// The object of a lowered access, visited (and so lowered and converted itself).
func (tx *OwnLayoutTransformer) objectOf(node *ast.Node) *ast.Node {
	return tx.Visitor().VisitNode(node.Expression())
}

// Returns an expression that can be repeated: the expression itself if it is simple, otherwise a
// temporary that is assigned first (the assignment is appended to temps).
func (tx *OwnLayoutTransformer) copiable(expr *ast.Node, temps *[]*ast.Node) *ast.Node {
	if transformers.IsSimpleCopiableExpression(expr) {
		return expr
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
	return tx.Factory().NewElementAccessExpression(tx.helper(transformers.OwnRuntimeI32Helper), nil /*questionDotToken*/, tx.binary(tx.paren(tx.add(a, tx.number(4))), ast.KindGreaterThanGreaterThanToken, tx.number(2)), ast.NodeFlagsNone)
}

// __OOB[I]: throws a RangeError naming the index, without a call.
func (tx *OwnLayoutTransformer) oob(i *ast.Node) *ast.Node {
	return tx.Factory().NewElementAccessExpression(tx.helper(transformers.OwnRuntimeOutOfBoundsHelper), nil /*questionDotToken*/, tx.dup(i), ast.NodeFlagsNone)
}

func (tx *OwnLayoutTransformer) f64(index *ast.Node) *ast.Node {
	return tx.Factory().NewElementAccessExpression(tx.helper(transformers.OwnRuntimeF64Helper), nil /*questionDotToken*/, index, ast.NodeFlagsNone)
}

func (tx *OwnLayoutTransformer) helper(name string) *ast.Node {
	return tx.Factory().NewUnscopedHelperName(name)
}

func (tx *OwnLayoutTransformer) call(helper string, args ...*ast.Node) *ast.Node {
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
