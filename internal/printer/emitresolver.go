package printer

import (
	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/binder"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/evaluator"
	"github.com/microsoft/typescript-go/internal/nodebuilder"
)

type SymbolAccessibility int32

const (
	SymbolAccessibilityAccessible SymbolAccessibility = iota
	SymbolAccessibilityNotAccessible
	SymbolAccessibilityCannotBeNamed
	SymbolAccessibilityNotResolved
)

type SymbolAccessibilityResult struct {
	Accessibility        SymbolAccessibility
	AliasesToMakeVisible []*ast.Node // aliases that need to have this symbol visible
	ErrorSymbolName      string      // Optional - symbol name that results in error
	ErrorNode            *ast.Node   // Optional - node that results in error
	ErrorModuleName      string      // Optional - If the symbol is not visible from module, module's name
}

/**
 * Indicates how to serialize the name for a TypeReferenceNode when emitting decorator metadata
 *
 * @internal
 */
type TypeReferenceSerializationKind int32

const (
	// The TypeReferenceNode could not be resolved.
	// The type name should be emitted using a safe fallback.
	TypeReferenceSerializationKindUnknown = iota

	// The TypeReferenceNode resolves to a type with a constructor
	// function that can be reached at runtime (e.g. a `class`
	// declaration or a `var` declaration for the static side
	// of a type, such as the global `Promise` type in lib.d.ts).
	TypeReferenceSerializationKindTypeWithConstructSignatureAndValue

	// The TypeReferenceNode resolves to a Void-like, Nullable, or Never type.
	TypeReferenceSerializationKindVoidNullableOrNeverType

	// The TypeReferenceNode resolves to a Number-like type.
	TypeReferenceSerializationKindNumberLikeType

	// The TypeReferenceNode resolves to a BigInt-like type.
	TypeReferenceSerializationKindBigIntLikeType

	// The TypeReferenceNode resolves to a String-like type.
	TypeReferenceSerializationKindStringLikeType

	// The TypeReferenceNode resolves to a Boolean-like type.
	TypeReferenceSerializationKindBooleanType

	// The TypeReferenceNode resolves to an Array-like type.
	TypeReferenceSerializationKindArrayLikeType

	// The TypeReferenceNode resolves to the ESSymbol type.
	TypeReferenceSerializationKindESSymbolType

	// The TypeReferenceNode resolved to the global Promise constructor symbol.
	TypeReferenceSerializationKindPromise

	// The TypeReferenceNode resolves to a Function type or a type with call signatures.
	TypeReferenceSerializationKindTypeWithCallSignature

	// The TypeReferenceNode resolves to any other type.
	TypeReferenceSerializationKindObjectType
)

// The typed layout of a value of the own/shared threads extension (with `--ownRuntime`): a shared
// struct or a SharedArray, which is represented by the byte address of its block in the shared heap.
type OwnCellKind int32

const (
	OwnCellNone          OwnCellKind = iota
	OwnCellStruct                    // a shared struct: 8-byte slots, one per field
	OwnCellArrayOfNumber             // a SharedArray<number>: f64 elements after a 16-byte header
	OwnCellArrayOfStruct             // a SharedArray<S> for a shared struct S: S's slots inline after the header
)

type OwnCellType struct {
	Kind   OwnCellKind
	Fields []string // the struct's fields in slot order (for OwnCellArrayOfStruct, the element struct's)
}

// How the emit converts an expression between the raw address of a struct or SharedArray and the
// runtime's handle object for it.
type OwnConversion int32

const (
	OwnConversionNone  OwnConversion = iota
	OwnConversionBox                 // `__box(E)`: a raw address flows into a place typed otherwise
	OwnConversionUnbox               // `__unbox(E)`: a handle (or other value) flows into a struct-typed place
)

type EmitResolver interface {
	binder.ReferenceResolver
	IsReferencedAliasDeclaration(node *ast.Node) bool
	IsValueAliasDeclaration(node *ast.Node) bool
	IsTopLevelValueImportEqualsWithEntityName(node *ast.Node) bool
	MarkLinkedReferencesRecursively(file *ast.SourceFile)
	GetExternalModuleFileFromDeclaration(node *ast.Node) *ast.SourceFile
	GetEffectiveDeclarationFlags(node *ast.Node, flags ast.ModifierFlags) ast.ModifierFlags
	GetResolutionModeOverride(node *ast.Node) core.ResolutionMode

	// decorator metadata
	GetTypeReferenceSerializationKind(name *ast.EntityName, serialScope *ast.Node) TypeReferenceSerializationKind

	// const enum inlining
	GetConstantValue(node *ast.Node) any

	// own/shared threads typed layouts
	GetOwnCellType(expression *ast.Node) OwnCellType
	GetOwnConversion(expression *ast.Node) OwnConversion

	// JSX Emit
	GetJsxFactoryEntity(location *ast.Node) *ast.Node
	GetJsxFragmentFactoryEntity(location *ast.Node) *ast.Node
	SetReferencedImportDeclaration(node *ast.IdentifierNode, ref *ast.Declaration) // for overriding the reference resolver behavior for generated identifiers

	// declaration emit checker functionality projections
	PrecalculateDeclarationEmitVisibility(file *ast.SourceFile)
	IsSymbolAccessible(symbol *ast.Symbol, enclosingDeclaration *ast.Node, meaning ast.SymbolFlags, shouldComputeAliasToMarkVisible bool) SymbolAccessibilityResult
	IsEntityNameVisible(entityName *ast.Node, enclosingDeclaration *ast.Node) SymbolAccessibilityResult // previously SymbolVisibilityResult in strada - ErrorModuleName never set
	IsExpandoFunctionDeclaration(node *ast.Node) bool
	IsExpandoFunctionDeclarationUnsafe(node *ast.Node) bool
	IsLiteralConstDeclaration(node *ast.Node) bool
	RequiresAddingImplicitUndefined(node *ast.Node, symbol *ast.Symbol, enclosingDeclaration *ast.Node) bool
	IsDeclarationVisible(node *ast.Node) bool
	IsNameResolvable(location *ast.Node, name string) bool
	IsImportRequiredByAugmentation(decl *ast.ImportDeclaration) bool
	IsDefinitelyReferenceToGlobalSymbolObject(node *ast.Node) bool
	IsImplementationOfOverload(node *ast.SignatureDeclaration) bool
	GetEnumMemberValue(node *ast.Node) evaluator.Result
	IsLateBound(node *ast.Node) bool
	IsOptionalParameter(node *ast.Node) bool
	IsThisPropertyAssignmentDeclarationRedundant(node *ast.Node) bool

	// isolatedDeclarations-specific declaration emit
	GetPropertiesOfContainerFunction(node *ast.Node) []*ast.Symbol
	RequiresAddingImplicitUndefinedUnsafe(node *ast.Node, symbol *ast.Symbol, enclosingDeclaration *ast.Node) bool
	GetReferencedValueDeclarationUnsafe(node *ast.IdentifierNode) *ast.Declaration

	// Node construction for declaration emit
	CreateTypeOfDeclaration(emitContext *EmitContext, declaration *ast.Node, enclosingDeclaration *ast.Node, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) *ast.Node
	CreateReturnTypeOfSignatureDeclaration(emitContext *EmitContext, signatureDeclaration *ast.Node, enclosingDeclaration *ast.Node, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) *ast.Node
	CreateTypeParametersOfSignatureDeclaration(emitContext *EmitContext, signatureDeclaration *ast.Node, enclosingDeclaration *ast.Node, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) []*ast.Node
	CreateLiteralConstValue(emitContext *EmitContext, node *ast.Node, tracker nodebuilder.SymbolTracker) *ast.Node
	CreateTypeOfExpression(emitContext *EmitContext, expression *ast.Node, enclosingDeclaration *ast.Node, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) *ast.Node
	CreateLateBoundIndexSignatures(emitContext *EmitContext, container *ast.Node, enclosingDeclaration *ast.Node, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) []*ast.Node
	TryJSTypeNodeToTypeNode(emitContext *EmitContext, typeNode *ast.Node, enclosingDeclaration *ast.Node, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) *ast.Node
}
