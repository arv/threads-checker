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
	OwnCellNone           OwnCellKind = iota
	OwnCellStruct                     // a shared struct: 8-byte slots, one per field
	OwnCellArrayOfNumber              // a SharedArray<number>: f64 elements after a 16-byte header
	OwnCellArrayOfStruct              // a SharedArray<S> for a shared struct S: S's slots inline after the header
	OwnCellArrayOfBoolean             // a SharedArray<boolean>: one byte per element (0 or 1) after a 16-byte header
	OwnCellArrayOfString              // a SharedArray<string>: an i32 per element (an interned string's address, 0 for "") after a 16-byte header
	OwnCellArrayOfI32                 // a SharedArray<i32>: an i32 per element after a 16-byte header
	OwnCellArrayOfU8                  // a SharedArray<u8>: a byte per element after a 16-byte header
)

// Reports whether a layout is a SharedArray's.
func (k OwnCellKind) IsArray() bool {
	return k >= OwnCellArrayOfNumber
}

// How a shared struct's field holds its value in its 8-byte slot. The kinds are the letters of the
// string the emit gives the runtime (see OwnCellType.KindLetters).
type OwnFieldKind byte

const (
	OwnFieldNone OwnFieldKind = 0   // in a union of structs, a slot whose fields differ
	OwnFieldF64  OwnFieldKind = 'f' // a number: an f64
	OwnFieldRef  OwnFieldKind = 'r' // a shared struct or a SharedArray (or null): its address in an i32, 0 for null
	OwnFieldBool OwnFieldKind = 'b' // a boolean: a byte, 0 or 1, at the start of the slot
	OwnFieldStr  OwnFieldKind = 's' // a string: an interned string's address in an i32, 0 for ""
	OwnFieldI32  OwnFieldKind = 'i' // an i32 (the runtime's `i32`)
	OwnFieldU8   OwnFieldKind = 'u' // a u8 (the runtime's `u8`): a byte at the start of the slot
	OwnFieldChan OwnFieldKind = 'c' // a Channel (or null): its block's address in an i32, 0 for null (a channel lives as long as the program: no count)
)

type OwnCellType struct {
	Kind   OwnCellKind
	Fields []string       // the struct's fields in slot order (for OwnCellArrayOfStruct, the element struct's; for a union of structs, "" where they differ)
	Kinds  []OwnFieldKind // per field
}

// The kind of field k (OwnFieldNone if there is no such field).
func (t OwnCellType) FieldKind(k int) OwnFieldKind {
	if k < 0 || k >= len(t.Kinds) {
		return OwnFieldNone
	}
	return t.Kinds[k]
}

// The fields' kinds as a string of their letters, if any is other than a number or a reference
// (which the runtime tells apart by the reference fields' indexes alone), and "" otherwise.
func (t OwnCellType) KindLetters() string {
	for _, kind := range t.Kinds {
		if kind != OwnFieldF64 && kind != OwnFieldRef {
			return string(t.Kinds)
		}
	}
	return ""
}

// An atomic operation of the plain API (`atomic.add(s, "count", 1)` or `atomic.add(a, i, 1)`): the
// `Atomics` function it is, and what it acts on: i32 field Field of a shared struct, or (Field -1)
// an element of a SharedArray<i32>.
type OwnAtomicCall struct {
	Op    string
	Field int
}

// How the emit converts an expression between the raw address of a struct or SharedArray and the
// runtime's handle object for it.
type OwnConversion int32

const (
	OwnConversionNone  OwnConversion = iota
	OwnConversionBox                 // `__box(E)`: a raw address flows into a place typed otherwise
	OwnConversionUnbox               // `__unbox(E)`: a handle (or other value) flows into a struct-typed place
	OwnConversionZero                // `0`: null or undefined flows into a struct-typed place (or is compared with one)
)

// A shared struct that a `struct({...})` call of the plain API declares: its name (the variable it
// initializes) and layout.
type OwnStructCall struct {
	Name   string
	Layout OwnCellType
}

// How a struct or SharedArray value (a raw address) is counted (see REFCOUNT.md in porffor's
// threads/escape-proto and checker/ownrefcount.go).
type OwnCount int32

const (
	OwnCountNone       OwnCount = iota // not a raw address: not counted by the emit
	OwnCountNull                       // null or undefined: 0, which needs no count
	OwnCountStable                     // a stable borrow: a parameter, a local, an element of one, an unboxed handle
	OwnCountOwnedLocal                 // a stable borrow of an owned local of this function, whose count a return moves out
	OwnCountUnstable                   // an unstable borrow: a reference field read, valid until the next quiescent point
	OwnCountOwned                      // owned (+1): `new`, a call that returns a counted value
)

// How a variable or parameter of a struct or SharedArray type holds its value.
type OwnLocal int32

const (
	OwnLocalNone       OwnLocal = iota // not counted
	OwnLocalBorrowed                   // a borrow: no counting
	OwnLocalOwned                      // holds a count, released when its block (or function) ends
	OwnLocalOwnedMoved                 // owned, and a return moves its count out (so it is declared with `let`)
	OwnLocalModule                     // a module's variable: holds a count, never released
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
	IsOwnElementAccessInBounds(elementAccess *ast.Node) bool
	GetOwnCount(expression *ast.Node) OwnCount
	GetOwnLocal(declaration *ast.Node) OwnLocal
	GetOwnCaptures(function *ast.Node) []*ast.Node
	IsOwnQuiescentLoop(loop *ast.Node) bool
	IsOwnRefArgument(argument *ast.Node) bool
	GetOwnStructCall(call *ast.Node) *OwnStructCall
	GetOwnAtomicCall(call *ast.Node) *OwnAtomicCall
	IsOwnConsumeCall(call *ast.Node) bool

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
