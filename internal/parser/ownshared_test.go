package parser_test

import (
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/parser"
	"gotest.tools/v3/assert"
)

// Tests for the "own and shared threads" syntax extension: `sending T`, `shared T`, `consume x`
// and `shared struct`.

var ownSharedKinds = []ast.Kind{ast.KindSendingType, ast.KindSharedType, ast.KindConsumeExpression, ast.KindSharedStructDeclaration}

func parseOwnShared(t *testing.T, sourceText string) *ast.SourceFile {
	t.Helper()
	return parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: "/index.ts",
		Path:     "/index.ts",
	}, sourceText, core.ScriptKindTS)
}

func countKinds(root *ast.Node) map[ast.Kind]int {
	counts := map[ast.Kind]int{}
	var visit func(node *ast.Node) bool
	visit = func(node *ast.Node) bool {
		counts[node.Kind]++
		node.ForEachChild(visit)
		return false
	}
	root.ForEachChild(visit)
	return counts
}

// Renders the type and expression structure of a node as a compact S-expression, e.g.
// `(SendingType (ArrayType (TypeReference Body)))`.
func shape(node *ast.Node) string {
	var b strings.Builder
	var write func(node *ast.Node)
	write = func(node *ast.Node) {
		if node.Kind == ast.KindIdentifier {
			b.WriteString(node.Text())
			return
		}
		b.WriteString("(")
		b.WriteString(strings.TrimPrefix(node.Kind.String(), "Kind"))
		node.ForEachChild(func(child *ast.Node) bool {
			b.WriteString(" ")
			write(child)
			return false
		})
		b.WriteString(")")
	}
	write(node)
	return b.String()
}

func assertNoParseErrors(t *testing.T, file *ast.SourceFile) {
	t.Helper()
	for _, d := range file.Diagnostics() {
		t.Errorf("unexpected parse error at %d: %s", d.Pos(), d.String())
	}
}

// The type annotation of the first variable declared by the statement at index i.
func varType(file *ast.SourceFile, i int) *ast.Node {
	return file.Statements.Nodes[i].AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes[0].Type()
}

// The initializer of the first variable declared by the statement at index i.
func varInit(file *ast.SourceFile, i int) *ast.Node {
	return file.Statements.Nodes[i].AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes[0].Initializer()
}

func TestOwnSharedTypeOperators(t *testing.T) {
	t.Parallel()
	tests := []struct {
		source string
		want   string
	}{
		{"let a: sending Body[];", "(SendingType (ArrayType (TypeReference Body)))"},
		{"let a: shared Pos[];", "(SharedType (ArrayType (TypeReference Pos)))"},
		{"let a: (sending Body[])[];", "(ArrayType (ParenthesizedType (SendingType (ArrayType (TypeReference Body)))))"},
		{"let a: readonly shared Pos[];", "(TypeOperator (SharedType (ArrayType (TypeReference Pos))))"},
		{"let a: Slots<shared Pos[]>;", "(TypeReference Slots (SharedType (ArrayType (TypeReference Pos))))"},
		{"let a: sending shared T;", "(SendingType (SharedType (TypeReference T)))"},
		{"let a: shared readonly number[];", "(SharedType (TypeOperator (ArrayType (NumberKeyword))))"},
		{"let a: shared keyof T;", "(SharedType (TypeOperator (TypeReference T)))"},
		{"let a: sending A | B;", "(UnionType (SendingType (TypeReference A)) (TypeReference B))"},
		{"let a: sending (A | B);", "(SendingType (ParenthesizedType (UnionType (TypeReference A) (TypeReference B))))"},
		{"let a: shared string;", "(SharedType (StringKeyword))"},
		{"let a: shared typeof x;", "(SharedType (TypeQuery x))"},
		{"let a: Map<string, sending Body>;", "(TypeReference Map (StringKeyword) (SendingType (TypeReference Body)))"},
	}
	for _, test := range tests {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			file := parseOwnShared(t, test.source)
			assertNoParseErrors(t, file)
			assert.Equal(t, shape(varType(file, 0)), test.want)
		})
	}
}

func TestOwnSharedTypeOperatorsInSignatures(t *testing.T) {
	t.Parallel()
	file := parseOwnShared(t, `
declare function makeBodies(): sending Body[];
declare function accelerate(mine: Body[], all: readonly shared Pos[]): void;
function worker(t: number, mine: sending Body[], board: Slots<shared Pos[]>): sending Body[] { return mine; }
type F = (x: sending T) => shared U;
interface I { m(x: sending T): shared U; p: shared Pos }
`)
	assertNoParseErrors(t, file)
	counts := countKinds(file.AsNode())
	assert.Equal(t, counts[ast.KindSendingType], 5)
	assert.Equal(t, counts[ast.KindSharedType], 5)
}

func TestOwnSharedTypeOperatorKeepsOldMeaning(t *testing.T) {
	t.Parallel()
	tests := []struct {
		source string
		want   string // shape of the type annotation of the last statement
	}{
		{"type sending = number; let x: sending;", "(TypeReference sending)"},
		{"type shared = number; let x: shared;", "(TypeReference shared)"},
		{"let x: shared<number>;", "(TypeReference shared (NumberKeyword))"},
		{"let x: shared[];", "(ArrayType (TypeReference shared))"},
		{"let x: sending[][];", "(ArrayType (ArrayType (TypeReference sending)))"},
		{"let x: shared | string;", "(UnionType (TypeReference shared) (StringKeyword))"},
		{"let x: shared & {};", "(IntersectionType (TypeReference shared) (TypeLiteral))"},
		{`let x: shared["k"];`, "(IndexedAccessType (TypeReference shared) (LiteralType (StringLiteral)))"},
		{"let x: shared.Inner;", "(TypeReference (QualifiedName shared Inner))"},
		{"let x: readonly shared[];", "(TypeOperator (ArrayType (TypeReference shared)))"},
		{"let x: keyof shared;", "(TypeOperator (TypeReference shared))"},
		{"let x: (shared);", "(ParenthesizedType (TypeReference shared))"},
		{"let x: [sending, ...shared];", "(TupleType (TypeReference sending) (RestType (TypeReference shared)))"},
		{"let x: () => shared;", "(FunctionType (TypeReference shared))"},
		{"let x: sending\n;", "(TypeReference sending)"},
	}
	for _, test := range tests {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			file := parseOwnShared(t, test.source)
			assertNoParseErrors(t, file)
			assert.Equal(t, shape(varType(file, len(file.Statements.Nodes)-1)), test.want)
		})
	}
}

func TestOwnSharedIdentifiersKeepOldMeaning(t *testing.T) {
	t.Parallel()
	// Each snippet is valid TypeScript today; none of it may turn into the new syntax.
	sources := []string{
		"type C<T> = T extends shared ? 1 : 2;",
		"type C<T> = T extends sending ? 1 : 2;",
		"function f(): shared { return null!; }",
		"const g = (): sending => { return null!; };",
		"let m: { a: shared\n b: sending };",
		"const r = x as shared satisfies object;",
		"const r = x satisfies shared as unknown;",
		"type M = { [K in shared as `k${K}`]: 1 };",
		"function isShared(x: unknown): x is shared { return true; }",
		"class C implements shared {}",
		"interface I extends shared {}",
		"interface I { shared: number; sending(): void; consume: string; struct: boolean }",
		"class K { shared = 1; sending() {} consume!: number; static struct = 2 }",
		"let shared = 1, sending = 2, consume = 3, struct = 4;",
		"shared; sending; consume; struct;",
		"shared\nstruct\nFoo\n{}",
		"consume = 1; consume(x); consume.x; consume[0]; consume++; consume!;",
		"consume\nx;",
		"for (consume of xs) {}",
		"for (consume in obj) {}",
		"const a = consume as any, b = consume satisfies any, c = consume instanceof Object, d = consume in obj;",
		"const o = { consume, shared, sending, struct };",
		"const fn = consume => consume;",
		"const fn2 = async consume => consume;",
		"function struct(shared: number, sending: string, consume: boolean) { return shared; }",
		"import { shared, sending as consume } from 'x';",
		"export { shared, consume as struct };",
		"namespace shared { export const x = 1; }",
		"label: consume;",
		"@consume class D {}",
		"const lt = a < shared (1) > (2);",
		"const lt2 = a < shared - 1 > (2);",
		"const lt3 = f<shared>(x);",
		"const lt4 = a < sending `t` > (2);",
		"let t: typeof shared;",
		"let tq: typeof consume;",
	}
	for _, source := range sources {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			file := parseOwnShared(t, source)
			assertNoParseErrors(t, file)
			counts := countKinds(file.AsNode())
			for _, kind := range ownSharedKinds {
				assert.Equal(t, counts[kind], 0, "unexpected %v", kind)
			}
		})
	}
}

func TestOwnSharedSpeculativeTypeArgumentsKeepOldMeaning(t *testing.T) {
	t.Parallel()
	// `a < shared (1) > (2)` is two comparisons, not a call `a<shared (1)>(2)`.
	file := parseOwnShared(t, "const lt = a < shared (1) > (2);")
	assertNoParseErrors(t, file)
	assert.Equal(t, shape(varInit(file, 0)), "(BinaryExpression (BinaryExpression a (LessThanToken) (CallExpression shared (NumericLiteral))) (GreaterThanToken) (ParenthesizedExpression (NumericLiteral)))")
	// A real type position inside the type arguments of a call still gets the type operator.
	file = parseOwnShared(t, "const c = f<shared Pos[]>(x);")
	assertNoParseErrors(t, file)
	assert.Equal(t, shape(varInit(file, 0)), "(CallExpression f (SharedType (ArrayType (TypeReference Pos))) x)")
}

func TestOwnSharedConsumeExpression(t *testing.T) {
	t.Parallel()
	tests := []struct {
		source string
		want   string // shape of the initializer of the last statement
	}{
		{"const y = consume x;", "(ConsumeExpression x)"},
		{"const y = f(consume part, other);", "(CallExpression f (ConsumeExpression part) other)"},
		{"const y = typeof consume x;", "(TypeOfExpression (ConsumeExpression x))"},
		{"const y = consume x + 1;", "(BinaryExpression (ConsumeExpression x) (PlusToken) (NumericLiteral))"},
		{"const y = !consume x;", "(PrefixUnaryExpression (ConsumeExpression x))"},
		{"const y = [consume a, consume b];", "(ArrayLiteralExpression (ConsumeExpression a) (ConsumeExpression b))"},
		{"const y = consume string;", "(ConsumeExpression string)"},
		{"const y = consume consume;", "(ConsumeExpression consume)"},
		{"const y = c ? consume a : consume b;", "(ConditionalExpression c (QuestionToken) (ConsumeExpression a) (ColonToken) (ConsumeExpression b))"},
		{"const y = (consume x);", "(ParenthesizedExpression (ConsumeExpression x))"},
	}
	for _, test := range tests {
		t.Run(test.source, func(t *testing.T) {
			t.Parallel()
			file := parseOwnShared(t, test.source)
			assertNoParseErrors(t, file)
			assert.Equal(t, shape(varInit(file, len(file.Statements.Nodes)-1)), test.want)
		})
	}

	file := parseOwnShared(t, "function f(x: T) { g(consume x); return consume x; }")
	assertNoParseErrors(t, file)
	assert.Equal(t, countKinds(file.AsNode())[ast.KindConsumeExpression], 2)
}

func TestOwnSharedConsumeExponentiationIsAnError(t *testing.T) {
	t.Parallel()
	file := parseOwnShared(t, "const y = consume x ** 2;")
	diags := file.Diagnostics()
	assert.Equal(t, len(diags), 1)
	assert.Assert(t, strings.Contains(diags[0].String(), "'consume'"), diags[0].String())
}

func TestOwnSharedStructDeclaration(t *testing.T) {
	t.Parallel()
	file := parseOwnShared(t, `
shared struct Counter { n: number; total: number }
export shared struct Particle {
    readonly id: i32,
    pos: shared Pos[]
    mass: f64;
}
declare shared struct Ambient { a: number }
namespace N { export shared struct Inner { x: number } }
`)
	assertNoParseErrors(t, file)
	statements := file.Statements.Nodes

	counter := statements[0]
	assert.Equal(t, counter.Kind, ast.KindSharedStructDeclaration)
	assert.Equal(t, counter.Name().Text(), "Counter")
	assert.Equal(t, shape(counter), "(SharedStructDeclaration Counter (PropertySignature n (NumberKeyword)) (PropertySignature total (NumberKeyword)))")

	particle := statements[1]
	assert.Equal(t, particle.Kind, ast.KindSharedStructDeclaration)
	assert.Assert(t, ast.HasSyntacticModifier(particle, ast.ModifierFlagsExport))
	members := particle.Members()
	assert.Equal(t, len(members), 3)
	assert.Assert(t, ast.HasSyntacticModifier(members[0], ast.ModifierFlagsReadonly))
	assert.Equal(t, shape(members[0].Type()), "(TypeReference i32)")
	assert.Equal(t, shape(members[1].Type()), "(SharedType (ArrayType (TypeReference Pos)))")
	assert.Equal(t, shape(members[2].Type()), "(TypeReference f64)")
	assert.Assert(t, file.ExternalModuleIndicator != nil)

	ambient := statements[2]
	assert.Equal(t, ambient.Kind, ast.KindSharedStructDeclaration)
	assert.Assert(t, ast.HasSyntacticModifier(ambient, ast.ModifierFlagsAmbient))

	inner := statements[3].AsModuleDeclaration().Body.Statements()[0]
	assert.Equal(t, inner.Kind, ast.KindSharedStructDeclaration)
}

func TestOwnSharedStructOnlyHasFields(t *testing.T) {
	t.Parallel()
	file := parseOwnShared(t, "shared struct S { x: number; m(): void; [k: string]: number }")
	assert.Equal(t, file.Statements.Nodes[0].Kind, ast.KindSharedStructDeclaration)
	diags := file.Diagnostics()
	assert.Equal(t, len(diags), 2)
	for _, d := range diags {
		assert.Assert(t, strings.Contains(d.String(), "'field' expected"), d.String())
	}
}

func TestOwnSharedNBodyExample(t *testing.T) {
	t.Parallel()
	file := parseOwnShared(t, `
type Body = { x: number; y: number; z: number; vx: number; vy: number; vz: number; m: number };
type Pos = { x: number; y: number; z: number; m: number };
declare const STEPS: number;
declare function makeBodies(): sending Body[];
declare function accelerate(mine: Body[], all: readonly shared Pos[]): void;
declare function move(mine: Body[]): void;
declare function energy(bodies: Body[]): number;

function worker(t: number, mine: sending Body[], board: Slots<shared Pos[]>, barrier: Barrier): sending Body[] {
  for (let s = 0; s < STEPS; s++) {
    board.set(t, share(mine.map(b => ({ x: b.x, y: b.y, z: b.z, m: b.m }))));
    barrier.wait();
    accelerate(mine, board.flat());
    barrier.wait();
    move(mine);
  }
  return mine;
}

function simulate(threads: number): number {
  const board = new Slots<shared Pos[]>(threads), barrier = new Barrier(threads);
  return scope(s => {
    const parts = partition(makeBodies(), threads);
    const ts = parts.map((part, t) => s.spawn(worker, t, consume part, board, barrier));
    return energy(ts.flatMap(th => th.join()));
  });
}

shared struct Counter { n: number; total: number }
`)
	assertNoParseErrors(t, file)
	counts := countKinds(file.AsNode())
	assert.Equal(t, counts[ast.KindSendingType], 3)
	assert.Equal(t, counts[ast.KindSharedType], 3)
	assert.Equal(t, counts[ast.KindConsumeExpression], 1)
	assert.Equal(t, counts[ast.KindSharedStructDeclaration], 1)
}

func TestOwnSharedSourceFlag(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"let a: sending T;",
		"let a: shared T;",
		"f(consume x);",
		"shared struct S { x: number }",
	} {
		file := parseOwnShared(t, source)
		assert.Assert(t, file.Flags&ast.NodeFlagsPossiblyContainsOwnershipSyntax != 0, source)
	}
	file := parseOwnShared(t, "type shared<T> = T; let sending: shared<number> = 1; consume(sending); let struct = 0;")
	assert.Assert(t, file.Flags&ast.NodeFlagsPossiblyContainsOwnershipSyntax == 0)
}
