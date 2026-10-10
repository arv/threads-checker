package transformers

import (
	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/printer"
)

// Emit support for the runtime of the "own and shared threads" extension (`--ownRuntime`).
//
// With `ownRuntime` set to a module specifier, every ES module that uses the extension's syntax
// imports the runtime helpers it needs from that module:
//
//	import { __register, __sharedStruct } from "./own.mjs";
//
// `__sharedStruct` builds the runtime value of a `shared struct` declaration, and `__register` is
// called at the end of the module with all of its top-level functions, so that the runtime can find
// them by name in other threads:
//
//	__register(import.meta.url, { worker, simulate, main });
//
// The helpers are referenced through helper names, so a file that declares its own `__register`
// gets `import { __register as __register_1 }` instead of a conflict.

const (
	OwnRuntimeSharedStructHelper = "__sharedStruct"
	OwnRuntimeRegisterHelper     = "__register"
)

// Reports whether the emit of a file uses the `ownRuntime` helpers: the option is set, the file
// uses the extension's syntax, it is an ES module, and it is emitted as one.
func UsesOwnRuntime(emitContext *printer.EmitContext, options *core.CompilerOptions, file *ast.SourceFile, getEmitModuleFormatOfFile func(ast.HasFileName) core.ModuleKind) bool {
	if options.OwnRuntime == "" || file == nil || options.GetEmitModuleKind() < core.ModuleKindES2015 {
		// Without ES module output the option is reported as an error and ignored.
		return false
	}
	original := emitContext.MostOriginal(file.AsNode()).AsSourceFile()
	if original.IsDeclarationFile || original.Flags&ast.NodeFlagsPossiblyContainsOwnershipSyntax == 0 || !ast.IsExternalModule(original) {
		return false
	}
	format := options.GetEmitModuleKind()
	if getEmitModuleFormatOfFile != nil {
		format = getEmitModuleFormatOfFile(original)
	}
	return format >= core.ModuleKindES2015
}

// The names of the top-level functions of a file that `__register` registers: function declarations
// with a name and a body, exported or not, in source order.
func OwnRuntimeRegisteredFunctions(file *ast.SourceFile) []string {
	var names []string
	for _, statement := range file.Statements.Nodes {
		if ast.IsFunctionDeclaration(statement) && statement.Body() != nil && statement.Name() != nil &&
			!ast.HasSyntacticModifier(statement, ast.ModifierFlagsAmbient) {
			names = append(names, statement.Name().Text())
		}
	}
	return names
}

// Reports whether a file has a (non-ambient) shared struct declaration anywhere.
func OwnRuntimeNeedsSharedStruct(file *ast.SourceFile) bool {
	found := false
	var visit ast.Visitor
	visit = func(node *ast.Node) bool {
		switch {
		case ast.IsTypeNode(node) || node.Flags&ast.NodeFlagsAmbient != 0:
			return false
		case ast.IsSharedStructDeclaration(node):
			found = true
			return true
		}
		return node.ForEachChild(visit)
	}
	file.AsNode().ForEachChild(visit)
	return found
}

// Creates the `import { ... } from "<ownRuntime>"` declaration and the `__register(...)` statement
// for a file whose emit uses the `ownRuntime` helpers. Either may be nil if it is not needed.
func CreateOwnRuntimeStatements(emitContext *printer.EmitContext, options *core.CompilerOptions, file *ast.SourceFile) (importDeclaration *ast.Statement, registration *ast.Statement) {
	original := emitContext.MostOriginal(file.AsNode()).AsSourceFile()
	f := emitContext.Factory
	functions := OwnRuntimeRegisteredFunctions(original)
	var helpers []string // in sorted order
	if len(functions) > 0 {
		helpers = append(helpers, OwnRuntimeRegisterHelper)
	}
	if OwnRuntimeNeedsSharedStruct(original) {
		helpers = append(helpers, OwnRuntimeSharedStructHelper)
	}
	if len(helpers) == 0 {
		return nil, nil
	}
	// Helper references print as unique names (`__register_1`) if the file uses the name itself.
	emitContext.AddEmitFlags(original.AsNode(), printer.EFExternalHelpers)
	specifiers := core.Map(helpers, func(name string) *ast.Node {
		if printer.IsFileLevelUniqueName(original, name, nil /*hasGlobalName*/) {
			return f.NewImportSpecifier(false /*isTypeOnly*/, nil /*propertyName*/, f.NewIdentifier(name))
		}
		return f.NewImportSpecifier(false /*isTypeOnly*/, f.NewIdentifier(name), f.NewUnscopedHelperName(name))
	})
	importDeclaration = f.NewImportDeclaration(
		nil, /*modifiers*/
		f.NewImportClause(ast.KindUnknown /*phaseModifier*/, nil /*name*/, f.NewNamedImports(f.NewNodeList(specifiers))),
		f.NewStringLiteral(options.OwnRuntime, ast.TokenFlagsNone),
		nil, /*attributes*/
	)
	if len(functions) > 0 {
		properties := core.Map(functions, func(name string) *ast.Node {
			return f.NewShorthandPropertyAssignment(nil /*modifiers*/, f.NewIdentifier(name), nil /*postfixToken*/, nil /*typeNode*/, nil /*equalsToken*/, nil /*objectAssignmentInitializer*/)
		})
		registration = f.NewExpressionStatement(f.NewCallExpression(
			f.NewUnscopedHelperName(OwnRuntimeRegisterHelper),
			nil, /*questionDotToken*/
			nil, /*typeArguments*/
			f.NewNodeList([]*ast.Node{
				f.NewPropertyAccessExpression(f.NewMetaProperty(ast.KindImportKeyword, f.NewIdentifier("meta")), nil /*questionDotToken*/, f.NewIdentifier("url"), ast.NodeFlagsNone),
				f.NewObjectLiteralExpression(f.NewNodeList(properties), false /*multiLine*/),
			}),
			ast.NodeFlagsNone,
		))
	}
	return importDeclaration, registration
}
