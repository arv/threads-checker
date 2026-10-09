package parser

import (
	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/diagnostics"
	"github.com/microsoft/typescript-go/internal/scanner"
)

// Parsing for the "own and shared threads" extension:
//
//	sending T                      SendingType
//	shared T                       SharedType
//	consume x                      ConsumeExpression
//	shared struct Name { f: T }    SharedStructDeclaration
//
// `sending`, `shared`, `consume` and `struct` are contextual keywords. The scanner keeps producing
// plain identifiers for them, and they only take on their new meaning in positions where the old
// grammar had no valid parse, so existing code keeps parsing exactly as before.

const (
	sendingKeywordText = "sending"
	sharedKeywordText  = "shared"
	consumeKeywordText = "consume"
	structKeywordText  = "struct"
)

// Reports whether the current token is the unescaped identifier `text`. As with real keywords,
// spelling a contextual keyword with a unicode escape turns it back into an ordinary identifier.
func (p *Parser) isContextualIdentifier(text string) bool {
	return p.token == ast.KindIdentifier &&
		!p.scanner.HasUnicodeEscape() && !p.scanner.HasExtendedUnicodeEscape() &&
		p.scanner.TokenValue() == text
}

// Type operators

// `sending` and `shared` start a type operator when followed, on the same line, by a token that
// starts a type and that could not already continue a type reference named `sending` or `shared`.
func (p *Parser) isStartOfOwnershipTypeOperator() bool {
	return (p.isContextualIdentifier(sendingKeywordText) || p.isContextualIdentifier(sharedKeywordText)) &&
		p.lookAhead((*Parser).nextTokenStartsOperandOfOwnershipTypeOperator)
}

func (p *Parser) nextTokenStartsOperandOfOwnershipTypeOperator() bool {
	p.nextToken()
	if p.hasPrecedingLineBreak() {
		return false
	}
	switch p.token {
	// After a type reference these continue the type (`shared[]`, `shared<T>`, `shared | T`,
	// `T extends shared ? A : B`, JSDoc `shared?`, `shared!`, `shared=`), or start the body of a
	// function whose return type is `shared` (`function f(): shared {`). Keep their old meaning.
	case ast.KindOpenBracketToken, ast.KindOpenBraceToken, ast.KindLessThanToken, ast.KindBarToken,
		ast.KindAmpersandToken, ast.KindQuestionToken, ast.KindExclamationToken, ast.KindAsteriskToken,
		ast.KindDotDotDotToken, ast.KindEqualsToken:
		return false
	// `x as shared satisfies T`, `{ [K in shared as N]: V }`, ...
	case ast.KindAsKeyword, ast.KindSatisfiesKeyword, ast.KindIsKeyword, ast.KindImplementsKeyword:
		return false
	case ast.KindOpenParenToken, ast.KindMinusToken, ast.KindNoSubstitutionTemplateLiteral, ast.KindTemplateHead:
		// In `a < shared (1) > (2)` the type arguments of a would-be call are parsed speculatively.
		// There `shared (1)`, `shared -1` and `shared` followed by a template already have a meaning
		// as expressions, so only treat them as a type operator in real type positions.
		if p.inExpressionTypeArguments {
			return false
		}
	}
	return p.isStartOfType(false /*inStartOfParameter*/)
}

func (p *Parser) parseOwnershipTypeOperator() *ast.Node {
	pos := p.nodePos()
	isSending := p.scanner.TokenValue() == sendingKeywordText
	p.nextToken()
	operand := p.parseTypeOperatorOrHigher()
	var result *ast.Node
	if isSending {
		result = p.factory.NewSendingTypeNode(operand)
	} else {
		result = p.factory.NewSharedTypeNode(operand)
	}
	return p.finishNode(result, pos)
}

// Expressions

// `consume` starts a consume expression when followed, on the same line, by an identifier. The
// identifiers `as`, `satisfies` and `of` are excluded because `consume as T`, `consume satisfies T`
// and `for (consume of xs)` are already valid.
func (p *Parser) isStartOfConsumeExpression() bool {
	return p.isContextualIdentifier(consumeKeywordText) && p.lookAhead((*Parser).nextTokenIsOperandOfConsumeExpression)
}

func (p *Parser) nextTokenIsOperandOfConsumeExpression() bool {
	p.nextToken()
	if p.hasPrecedingLineBreak() || !p.isIdentifier() {
		return false
	}
	switch p.token {
	case ast.KindAsKeyword, ast.KindSatisfiesKeyword, ast.KindOfKeyword:
		return false
	}
	return true
}

func (p *Parser) parseConsumeExpression() *ast.Node {
	pos := p.nodePos()
	p.nextToken()
	return p.finishNode(p.factory.NewConsumeExpression(p.parseIdentifier()), pos)
}

// Declarations

// `shared struct Name` starts a shared struct declaration when all three tokens are on one line.
// This works both at the start of a statement and after modifiers (`export shared struct S {}`).
func (p *Parser) isStartOfSharedStructDeclaration() bool {
	return p.isContextualIdentifier(sharedKeywordText) && p.lookAhead((*Parser).nextTokensAreStructAndNameOnSameLine)
}

func (p *Parser) nextTokensAreStructAndNameOnSameLine() bool {
	p.nextToken()
	if p.hasPrecedingLineBreak() || !p.isContextualIdentifier(structKeywordText) {
		return false
	}
	p.nextToken()
	return !p.hasPrecedingLineBreak() && p.isIdentifier()
}

func (p *Parser) parseSharedStructDeclaration(pos int, jsdoc jsdocScannerInfo, modifiers *ast.ModifierList) *ast.Node {
	p.nextToken() // shared
	p.nextToken() // struct
	name := p.parseIdentifier()
	members := p.parseObjectTypeMembers()
	for _, member := range members.Nodes {
		// A shared struct only has fields: `name: Type`, optionally `readonly`.
		if member.Kind != ast.KindPropertySignature {
			p.parseErrorAt(scanner.SkipTrivia(p.sourceText, member.Pos()), member.End(), diagnostics.X_0_expected, "field")
		}
	}
	result := p.finishNode(p.factory.NewSharedStructDeclaration(modifiers, name, members), pos)
	p.withJSDoc(result, jsdoc)
	return result
}
