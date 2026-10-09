package tstransforms_test

import (
	"testing"

	"github.com/microsoft/typescript-go/internal/binder"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/printer"
	"github.com/microsoft/typescript-go/internal/testutil/emittestutil"
	"github.com/microsoft/typescript-go/internal/testutil/parsetestutil"
	"github.com/microsoft/typescript-go/internal/transformers"
	"github.com/microsoft/typescript-go/internal/transformers/tstransforms"
)

// JS emit of the "own and shared threads" syntax: `sending T` and `shared T` are erased with the rest
// of the types, `consume x` becomes `x`, and `shared struct` becomes a placeholder runtime call.
func TestOwnSharedEmit(t *testing.T) {
	t.Parallel()
	data := []struct {
		title  string
		input  string
		output string
	}{
		{title: "SendingType", input: "function f(x: sending T[]): sending T[] { return x; }", output: "function f(x) { return x; }"},
		{title: "SharedType", input: "let b: Slots<shared Pos[]> = new Slots<shared Pos[]>(2);", output: "let b = new Slots(2);"},
		{title: "ReadonlySharedType", input: "declare function g(all: readonly shared Pos[]): void;", output: ""},
		{title: "TypeAlias", input: "type A = (sending Body[])[];", output: ""},
		{title: "ConsumeExpression#1", input: "f(t, consume part, b);", output: "f(t, part, b);"},
		{title: "ConsumeExpression#2", input: "const y = consume x;", output: "const y = x;"},
		{title: "ConsumeExpression#3", input: "(consume x).y;", output: "x.y;"},
		{title: "SharedStructDeclaration#1", input: "shared struct Counter { n: number; total: number }", output: "const Counter = __sharedStruct(\"Counter\", [\"n\", \"total\"]);"},
		{title: "SharedStructDeclaration#2", input: "export shared struct P { readonly x: f64; ps: shared Pos[] }", output: "export const P = __sharedStruct(\"P\", [\"x\", \"ps\"]);"},
		{title: "SharedStructDeclaration#3", input: "shared struct Q { \"a b\": i32; 0: f64 }", output: "const Q = __sharedStruct(\"Q\", [\"a b\", 0]);"},
		{title: "SharedStructDeclaration#4", input: "shared struct E {}", output: "const E = __sharedStruct(\"E\", []);"},
		{title: "SharedStructDeclaration#5", input: "declare shared struct A { x: number }", output: ""},
		{title: "SharedStructDeclaration#6", input: "namespace N { export shared struct S { x: number } }", output: "var N;\n(function (N) {\n    N.S = __sharedStruct(\"S\", [\"x\"]);\n})(N || (N = {}));"},
	}

	for _, rec := range data {
		t.Run(rec.title, func(t *testing.T) {
			t.Parallel()
			file := parsetestutil.ParseTypeScript(rec.input, false /*jsx*/)
			parsetestutil.CheckDiagnostics(t, file)
			binder.BindSourceFile(file)
			compilerOptions := &core.CompilerOptions{}
			emitContext := printer.NewEmitContext()
			opts := &transformers.TransformOptions{
				CompilerOptions: compilerOptions,
				Context:         emitContext,
				Resolver:        binder.NewReferenceResolver(compilerOptions, binder.ReferenceResolverHooks{}),
			}
			file = tstransforms.NewTypeEraserTransformer(opts).TransformSourceFile(file)
			file = tstransforms.NewRuntimeSyntaxTransformer(opts).TransformSourceFile(file)
			emittestutil.CheckEmit(t, emitContext, file, rec.output)
		})
	}
}
