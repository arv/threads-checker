package transformers

import (
	"slices"
	"strings"

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
// `__sharedStruct` builds the runtime value of a `shared struct` declaration (or of a `struct({...})`
// call of the plain API), and `__register` is called at the end of every module that uses the
// extension's syntax or imports the runtime, with all of its top-level functions, so that the runtime
// can find them by name in other threads:
//
//	__register(import.meta.url, { worker, simulate, main });
//
// The typed layouts of shared structs and SharedArrays use more helpers (`__F64`, `__I32`, `__U8`,
// `__newStruct`, `__newSharedArray`, `__OOB`, `__box`, `__unbox`, `__intern` and `__str` for strings,
// and `__cellAddress` for channels), and so does their reference
// counting (`__retain`, `__release`, `__store`, `__ref`, `__quiescent`, `__hold`; see
// tstransforms/ownlayout.go). The import lists exactly the helpers the emitted module references.
//
// The helpers are referenced through helper names, so a file that declares its own `__register`
// gets `import { __register as __register_1 }` instead of a conflict.

const (
	OwnRuntimeSharedStructHelper   = "__sharedStruct"
	OwnRuntimeRegisterHelper       = "__register"
	OwnRuntimeF64Helper            = "__F64"
	OwnRuntimeI32Helper            = "__I32"
	OwnRuntimeU8Helper             = "__U8"
	OwnRuntimeInternHelper         = "__intern"
	OwnRuntimeStrHelper            = "__str"
	OwnRuntimeCellAddressHelper    = "__cellAddress"
	OwnRuntimeNewStructHelper      = "__newStruct"
	OwnRuntimeNewSharedArrayHelper = "__newSharedArray"
	OwnRuntimeOutOfBoundsHelper    = "__OOB"
	OwnRuntimeBoxHelper            = "__box"
	OwnRuntimeUnboxHelper          = "__unbox"
	OwnRuntimeRetainHelper         = "__retain"
	OwnRuntimeReleaseHelper        = "__release"
	OwnRuntimeStoreHelper          = "__store"
	OwnRuntimeRefHelper            = "__ref"
	OwnRuntimeQuiescentHelper      = "__quiescent"
	OwnRuntimeHoldHelper           = "__hold"
)

var ownRuntimeHelpers = []string{ // sorted
	OwnRuntimeF64Helper,
	OwnRuntimeI32Helper,
	OwnRuntimeOutOfBoundsHelper,
	OwnRuntimeU8Helper,
	OwnRuntimeBoxHelper,
	OwnRuntimeCellAddressHelper,
	OwnRuntimeHoldHelper,
	OwnRuntimeInternHelper,
	OwnRuntimeNewSharedArrayHelper,
	OwnRuntimeNewStructHelper,
	OwnRuntimeQuiescentHelper,
	OwnRuntimeRefHelper,
	OwnRuntimeRegisterHelper,
	OwnRuntimeReleaseHelper,
	OwnRuntimeRetainHelper,
	OwnRuntimeSharedStructHelper,
	OwnRuntimeStoreHelper,
	OwnRuntimeStrHelper,
	OwnRuntimeUnboxHelper,
}

// Reports whether the emit of a file may use the `ownRuntime` helpers: the option is set and the
// file is an ES module that is emitted as one.
func UsesOwnRuntime(emitContext *printer.EmitContext, options *core.CompilerOptions, file *ast.SourceFile, getEmitModuleFormatOfFile func(ast.HasFileName) core.ModuleKind) bool {
	if options.OwnRuntime == "" || file == nil || options.GetEmitModuleKind() < core.ModuleKindES2015 {
		// Without ES module output the option is reported as an error and ignored.
		return false
	}
	original := emitContext.MostOriginal(file.AsNode()).AsSourceFile()
	if original.IsDeclarationFile || !ast.IsExternalModule(original) {
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

// Reports whether a file imports the runtime module (the plain API, which has no syntax to look for).
func importsOwnRuntime(file *ast.SourceFile, specifier string) bool {
	return core.Some(file.Statements.Nodes, func(statement *ast.Node) bool {
		if !ast.IsImportDeclaration(statement) && !ast.IsExportDeclaration(statement) {
			return false
		}
		module := statement.ModuleSpecifier()
		return module != nil && ast.IsStringLiteral(module) && module.Text() == specifier
	})
}

// The runtime helpers that a transformed file references, sorted.
func ownRuntimeHelpersUsedBy(emitContext *printer.EmitContext, file *ast.SourceFile) []string {
	used := map[string]bool{}
	var visit ast.Visitor
	visit = func(node *ast.Node) bool {
		if ast.IsIdentifier(node) {
			if emitContext.EmitFlags(node)&printer.EFHelperName != 0 {
				used[node.Text()] = true
			}
			return false
		}
		return node.ForEachChild(visit)
	}
	file.AsNode().ForEachChild(visit)
	return core.Filter(ownRuntimeHelpers, func(name string) bool { return used[name] })
}

// Creates the `import { ... } from "<ownRuntime>"` declaration and the `__register(...)` statement
// for a (transformed) file whose emit may use the `ownRuntime` helpers. Either may be nil if it is
// not needed. Files that use the extension's syntax register their top-level functions.
func CreateOwnRuntimeStatements(emitContext *printer.EmitContext, options *core.CompilerOptions, file *ast.SourceFile) (importDeclaration *ast.Statement, registration *ast.Statement) {
	original := emitContext.MostOriginal(file.AsNode()).AsSourceFile()
	f := emitContext.Factory
	var functions []string
	if original.Flags&ast.NodeFlagsPossiblyContainsOwnershipSyntax != 0 || importsOwnRuntime(original, options.OwnRuntime) {
		functions = OwnRuntimeRegisteredFunctions(original)
	}
	helpers := ownRuntimeHelpersUsedBy(emitContext, file)
	if len(functions) > 0 && !slices.Contains(helpers, OwnRuntimeRegisterHelper) {
		helpers = append(helpers, OwnRuntimeRegisterHelper)
		slices.SortFunc(helpers, strings.Compare)
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
