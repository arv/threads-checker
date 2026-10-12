package checker

import (
	"math"
	"slices"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/diagnostics"
	"github.com/microsoft/typescript-go/internal/jsnum"
	"github.com/microsoft/typescript-go/internal/printer"
)

// Integers (the runtime's `i32` and `u8`, with `--ownRuntime`)
//
// An i32 or a u8 holds an integer: a value stored into one is converted as an Int32Array or a
// Uint8Array converts it. That wraps an integer that is out of range, as integer arithmetic does in
// C or Java, but it truncates a number that is not an integer with no sign at all, and the types
// cannot tell the two apart: TypeScript has one number type, which `i32` and `u8` only brand. So a
// value that flows into an i32 or a u8 (a field or an element, a variable, a parameter, a return
// value, a cast) must be an integer expression, as asm.js required of a value stored into an integer
// view, and anything else must be converted explicitly, with an operation that says how. An integer
// expression is:
//
//   - a value of an integer type: an i32 or a u8, or an integer literal type (`const k = 3`);
//   - `x | y`, `x & y`, `x ^ y`, `~x`, and the shifts: integers, whatever their operands (asm.js's
//     idioms: `x | 0`, `~~x`, `x >>> 0`, `x & 255`);
//   - `x + y`, `x - y`, `x * y`, `x % y`, `-x`, `+x`, `x++`, and `c ? x : y`, of integers;
//   - Math.round, Math.floor, Math.ceil, Math.trunc, Math.sign, Math.clz32 and Math.imul of anything,
//     and Math.abs, Math.min and Math.max of integers;
//   - `.length`, and the results of charCodeAt, indexOf, lastIndexOf, findIndex and findLastIndex;
//   - a local variable whose every value is an integer expression: its initializer, and whatever is
//     assigned to it (not by destructuring, or as a for-in or for-of variable).

type ownIntegerState struct {
	reporting bool                     // conversions are being computed for diagnostics: report what flows into integers
	variables map[*ast.Symbol]bool     // local variables, by whether every value assigned to them is an integer
	files     map[*ast.SourceFile]bool // the files whose variables are computed (false while they are being computed)
}

// The kind of integer, i32 or u8, a value that flows into a place of type t becomes (looking through
// null and undefined), or OwnFieldNone.
func (c *Checker) getOwnIntegerTargetKind(t *Type) printer.OwnFieldKind {
	if t == nil {
		return printer.OwnFieldNone
	}
	members := c.ownNonNullishMembers(t)
	if len(members) != 1 {
		return printer.OwnFieldNone
	}
	return c.getOwnIntegerKind(members[0])
}

// Reports a value that flows into a place of type target, an i32 or a u8, but may not be an integer.
func (c *Checker) checkOwnIntegerTarget(expr *ast.Node, target *Type) {
	if !c.ownLayout.integers.reporting || expr == nil {
		return
	}
	kind := c.getOwnIntegerTargetKind(target)
	if kind == printer.OwnFieldNone || !c.maybeTypeOfKind(c.getTypeOfExpression(expr), TypeFlagsNumberLike|TypeFlagsAny) || c.isOwnIntegerExpression(expr) {
		return
	}
	c.reportOwnNonInteger(expr, kind)
}

// Reports a compound assignment to an i32 or a u8 (`x /= 2`) whose result may not be an integer.
func (c *Checker) checkOwnIntegerCompoundAssignment(node *ast.Node) {
	if !c.ownLayout.integers.reporting {
		return
	}
	kind := c.getOwnIntegerTargetKind(c.getOwnAssignmentTargetType(node.AsBinaryExpression().Left))
	if kind != printer.OwnFieldNone && !c.isOwnIntegerExpression(node) {
		c.reportOwnNonInteger(node, kind)
	}
}

func (c *Checker) reportOwnNonInteger(node *ast.Node, kind printer.OwnFieldKind) {
	name := "an i32"
	if kind == printer.OwnFieldU8 {
		name = "a u8"
	}
	c.error(node, diagnostics.This_value_may_not_be_an_integer_which_0_would_truncate_silently_Colon_convert_it_explicitly_as_with_Math_round_Math_trunc_or_0, name)
}

// Reports whether a type's values are integers: an i32 or a u8, an integer literal type, or a union
// of them.
func (c *Checker) isOwnIntegerType(t *Type) bool {
	if c.getOwnIntegerKind(t) != printer.OwnFieldNone {
		return true
	}
	if t.flags&TypeFlagsNumberLiteral != 0 {
		if value, ok := t.AsLiteralType().Value().(jsnum.Number); ok {
			f := float64(value)
			return !math.IsInf(f, 0) && f == math.Trunc(f)
		}
		return false
	}
	return t.flags&TypeFlagsUnion != 0 && core.Every(t.Types(), c.isOwnIntegerType)
}

// Reports whether an expression's value is an integer (see "Integers" above). A cast is one by its
// type: a cast to an integer type is reported by itself, where it flows into one.
func (c *Checker) isOwnIntegerExpression(expr *ast.Node) bool {
	expr = ast.SkipParentheses(expr)
	if c.isOwnIntegerType(c.getTypeOfExpression(expr)) {
		return true
	}
	switch expr.Kind {
	case ast.KindNonNullExpression, ast.KindSatisfiesExpression:
		return c.isOwnIntegerExpression(expr.Expression())
	case ast.KindPrefixUnaryExpression:
		n := expr.AsPrefixUnaryExpression()
		switch n.Operator {
		case ast.KindTildeToken:
			return true
		case ast.KindMinusToken, ast.KindPlusToken, ast.KindPlusPlusToken, ast.KindMinusMinusToken:
			return c.isOwnIntegerExpression(n.Operand)
		}
	case ast.KindPostfixUnaryExpression:
		return c.isOwnIntegerExpression(expr.AsPostfixUnaryExpression().Operand)
	case ast.KindBinaryExpression:
		n := expr.AsBinaryExpression()
		switch op := n.OperatorToken.Kind; op {
		case ast.KindBarToken, ast.KindAmpersandToken, ast.KindCaretToken, ast.KindLessThanLessThanToken, ast.KindGreaterThanGreaterThanToken,
			ast.KindGreaterThanGreaterThanGreaterThanToken, ast.KindBarEqualsToken, ast.KindAmpersandEqualsToken, ast.KindCaretEqualsToken,
			ast.KindLessThanLessThanEqualsToken, ast.KindGreaterThanGreaterThanEqualsToken, ast.KindGreaterThanGreaterThanGreaterThanEqualsToken:
			return true
		case ast.KindPlusToken, ast.KindPlusEqualsToken:
			return c.getTypeOfExpression(expr).flags&TypeFlagsNumberLike != 0 && c.isOwnIntegerExpression(n.Left) && c.isOwnIntegerExpression(n.Right)
		case ast.KindMinusToken, ast.KindAsteriskToken, ast.KindPercentToken, ast.KindMinusEqualsToken, ast.KindAsteriskEqualsToken,
			ast.KindPercentEqualsToken, ast.KindAmpersandAmpersandToken, ast.KindBarBarToken, ast.KindQuestionQuestionToken:
			return c.isOwnIntegerExpression(n.Left) && c.isOwnIntegerExpression(n.Right)
		case ast.KindCommaToken, ast.KindEqualsToken:
			return c.isOwnIntegerExpression(n.Right)
		}
	case ast.KindConditionalExpression:
		n := expr.AsConditionalExpression()
		return c.isOwnIntegerExpression(n.WhenTrue) && c.isOwnIntegerExpression(n.WhenFalse)
	case ast.KindCallExpression:
		return c.isOwnIntegerCall(expr)
	case ast.KindPropertyAccessExpression:
		return expr.Name().Text() == "length" && c.getTypeOfExpression(expr).flags&TypeFlagsNumber != 0
	case ast.KindIdentifier:
		return c.isOwnIntegerVariable(expr)
	}
	return false
}

var (
	ownIntegerMath        = []string{"round", "floor", "ceil", "trunc", "sign", "clz32", "imul"}
	ownIntegerMathOfInts  = []string{"abs", "min", "max"}
	ownIntegerMethodNames = []string{"charCodeAt", "indexOf", "lastIndexOf", "findIndex", "findLastIndex"}
)

// Reports whether a call's result is an integer: one of Math's that are (of anything, or of integers),
// or a method that returns an index or a char code.
func (c *Checker) isOwnIntegerCall(call *ast.Node) bool {
	callee := ast.SkipParentheses(call.Expression())
	if !ast.IsPropertyAccessExpression(callee) {
		return false
	}
	name := callee.Name().Text()
	if object := ast.SkipParentheses(callee.Expression()); ast.IsIdentifier(object) && object.Text() == "Math" && c.isOwnGlobalMath(object) {
		if slices.Contains(ownIntegerMath, name) {
			return true
		}
		return slices.Contains(ownIntegerMathOfInts, name) && len(call.Arguments()) > 0 && core.Every(call.Arguments(), c.isOwnIntegerExpression)
	}
	return slices.Contains(ownIntegerMethodNames, name) && c.getTypeOfExpression(call).flags&TypeFlagsNumber != 0
}

// Reports whether an identifier `Math` is the global one, declared in a default library.
func (c *Checker) isOwnGlobalMath(id *ast.Node) bool {
	symbol := c.getResolvedSymbol(id)
	return symbol != nil && symbol != c.unknownSymbol && core.Some(symbol.Declarations, func(d *ast.Node) bool {
		return c.program.IsSourceFileDefaultLibrary(ast.GetSourceFileOfNode(d).Path())
	})
}

// Reports whether an identifier is a local variable (declared in a source file, not a parameter)
// whose every value is an integer expression.
func (c *Checker) isOwnIntegerVariable(id *ast.Node) bool {
	symbol := c.getResolvedSymbol(id)
	if symbol == nil || symbol.Flags&ast.SymbolFlagsVariable == 0 || symbol.ValueDeclaration == nil {
		return false
	}
	decl := symbol.ValueDeclaration
	if !ast.IsVariableDeclaration(decl) || !ast.IsIdentifier(decl.Name()) {
		return false
	}
	file := ast.GetSourceFileOfNode(decl)
	if file.IsDeclarationFile {
		return false
	}
	c.computeOwnIntegerVariables(file)
	return c.ownLayout.integers.variables[symbol]
}

// Decides which local variables of a file are integers: every one starts as an integer, and one with
// a value that is not (given what the others are) stops being one, until none changes.
func (c *Checker) computeOwnIntegerVariables(file *ast.SourceFile) {
	state := &c.ownLayout.integers
	if state.files == nil {
		state.files = map[*ast.SourceFile]bool{}
		state.variables = map[*ast.Symbol]bool{}
	}
	if _, ok := state.files[file]; ok {
		return // computed, or being computed: its variables' current values stand
	}
	state.files[file] = false
	values := map[*ast.Symbol][]*ast.Node{} // what is assigned to each variable (a compound assignment as a whole)
	var variables []*ast.Symbol
	notIntegers := map[*ast.Symbol]bool{}
	var visit ast.Visitor
	visit = func(node *ast.Node) bool {
		switch {
		case ast.IsVariableDeclaration(node) && ast.IsIdentifier(node.Name()):
			if symbol := c.getSymbolOfDeclaration(node); symbol != nil {
				variables = append(variables, symbol)
				if ast.IsForInOrOfStatement(node.Parent.Parent) {
					notIntegers[symbol] = true
				} else if init := node.Initializer(); init != nil {
					values[symbol] = append(values[symbol], init)
				}
			}
		case ast.IsBinaryExpression(node) && ast.IsAssignmentOperator(node.AsBinaryExpression().OperatorToken.Kind) &&
			ast.IsIdentifier(ast.SkipParentheses(node.AsBinaryExpression().Left)):
			n := node.AsBinaryExpression()
			if symbol := c.getResolvedSymbol(ast.SkipParentheses(n.Left)); symbol != nil {
				if n.OperatorToken.Kind == ast.KindEqualsToken {
					values[symbol] = append(values[symbol], n.Right)
				} else {
					values[symbol] = append(values[symbol], node)
				}
			}
		case ast.IsIdentifier(node) && ast.IsAssignmentTarget(node):
			// Assigned by destructuring, or as a for-in or for-of variable (an assignment's own left side
			// and the operand of ++ or -- are handled above, or keep an integer one).
			switch target := ast.GetAssignmentTarget(node); {
			case ast.IsBinaryExpression(target) && ast.SkipParentheses(target.AsBinaryExpression().Left) == node,
				ast.IsPrefixUnaryExpression(target), ast.IsPostfixUnaryExpression(target):
			default:
				if symbol := c.getResolvedSymbol(node); symbol != nil {
					notIntegers[symbol] = true
				}
			}
		}
		return node.ForEachChild(visit)
	}
	file.AsNode().ForEachChild(visit)
	for _, symbol := range variables {
		state.variables[symbol] = !notIntegers[symbol]
	}
	for changed := true; changed; {
		changed = false
		for _, symbol := range variables {
			if state.variables[symbol] && !core.Every(values[symbol], c.isOwnIntegerExpression) {
				state.variables[symbol] = false
				changed = true
			}
		}
	}
	state.files[file] = true
}
