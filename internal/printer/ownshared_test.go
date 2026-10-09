package printer_test

import (
	"testing"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/testutil/emittestutil"
	"github.com/microsoft/typescript-go/internal/testutil/parsetestutil"
)

// Printing the "own and shared threads" syntax back as TypeScript (no transforms).
func TestEmitOwnShared(t *testing.T) {
	t.Parallel()
	data := []struct {
		title  string
		input  string
		output string
	}{
		{title: "SendingType#1", input: "let a: sending Body[];", output: "let a: sending Body[];"},
		{title: "SendingType#2", input: "let a: (sending Body[])[];", output: "let a: (sending Body[])[];"},
		{title: "SendingType#3", input: "let a: sending (A | B);", output: "let a: sending (A | B);"},
		{title: "SendingType#4", input: "declare function f(x: sending T): sending U;", output: "declare function f(x: sending T): sending U;"},
		{title: "SharedType#1", input: "let a: readonly shared Pos[];", output: "let a: readonly shared Pos[];"},
		{title: "SharedType#2", input: "let a: Slots<shared Pos[]>;", output: "let a: Slots<shared Pos[]>;"},
		{title: "SharedType#3", input: "let a: sending shared T;", output: "let a: sending shared T;"},
		{title: "ConsumeExpression#1", input: "f(consume part);", output: "f(consume part);"},
		{title: "ConsumeExpression#2", input: "x = consume y;", output: "x = consume y;"},
		{title: "ConsumeExpression#3", input: "(consume y).z;", output: "(consume y).z;"},
		{title: "SharedStructDeclaration#1", input: "shared struct Counter { n: number; total: number }", output: "shared struct Counter {\n    n: number;\n    total: number;\n}"},
		{title: "SharedStructDeclaration#2", input: "export shared struct P {\n    readonly x: f64;\n    ps: shared Pos[];\n}", output: "export shared struct P {\n    readonly x: f64;\n    ps: shared Pos[];\n}"},
		{title: "SharedStructDeclaration#3", input: "declare shared struct S { }", output: "declare shared struct S {\n}"},
	}

	for _, rec := range data {
		t.Run(rec.title, func(t *testing.T) {
			t.Parallel()
			file := parsetestutil.ParseTypeScript(rec.input, false /*jsx*/)
			parsetestutil.CheckDiagnostics(t, file)
			emittestutil.CheckEmit(t, nil, file, rec.output)
		})
	}
}

func typeRef(factory *ast.NodeFactory, name string) *ast.Node {
	return factory.NewTypeReferenceNode(factory.NewIdentifier(name), nil /*typeArguments*/)
}

func TestParenthesizeOwnSharedTypes(t *testing.T) {
	t.Parallel()

	var factory ast.NodeFactory
	union := func() *ast.Node {
		return factory.NewUnionTypeNode(factory.NewNodeList([]*ast.Node{typeRef(&factory, "a"), typeRef(&factory, "b")}))
	}
	alias := func(name string, typeNode *ast.Node) *ast.Node {
		return factory.NewTypeAliasDeclaration(nil /*modifiers*/, factory.NewIdentifier(name), nil /*typeParameters*/, typeNode)
	}
	file := factory.NewSourceFile(ast.SourceFileParseOptions{FileName: "/file.ts", Path: "/file.ts"}, "", factory.NewNodeList(
		[]*ast.Node{
			// operand is parenthesized:
			alias("A", factory.NewSendingTypeNode(union())),
			// operator is parenthesized when used as an array element type:
			alias("B", factory.NewArrayTypeNode(factory.NewSharedTypeNode(typeRef(&factory, "T")))),
			// no parentheses needed:
			alias("C", factory.NewSharedTypeNode(factory.NewArrayTypeNode(typeRef(&factory, "T")))),
			alias("D", factory.NewUnionTypeNode(factory.NewNodeList([]*ast.Node{factory.NewSendingTypeNode(typeRef(&factory, "a")), typeRef(&factory, "b")}))),
		},
	), factory.NewToken(ast.KindEndOfFile))

	parsetestutil.MarkSyntheticRecursive(file)
	emittestutil.CheckEmit(t, nil, file.AsSourceFile(), "type A = sending (a | b);\ntype B = (shared T)[];\ntype C = shared T[];\ntype D = sending a | b;")
}

func TestParenthesizeConsumeExpression(t *testing.T) {
	t.Parallel()

	var factory ast.NodeFactory
	file := factory.NewSourceFile(ast.SourceFileParseOptions{FileName: "/file.ts", Path: "/file.ts"}, "", factory.NewNodeList(
		[]*ast.Node{
			factory.NewExpressionStatement(
				factory.NewPropertyAccessExpression(
					// will be parenthesized on emit:
					factory.NewConsumeExpression(factory.NewIdentifier("x")),
					nil, /*questionDotToken*/
					factory.NewIdentifier("y"),
					ast.NodeFlagsNone,
				),
			),
		},
	), factory.NewToken(ast.KindEndOfFile))

	parsetestutil.MarkSyntheticRecursive(file)
	emittestutil.CheckEmit(t, nil, file.AsSourceFile(), "(consume x).y;")
}
