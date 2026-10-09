package checker

import (
	"testing"

	"github.com/microsoft/typescript-go/internal/ast"
	"gotest.tools/v3/assert"
)

// Unit tests for the abstract state of the ownership analysis. The analysis itself is tested by the
// ownShared* compiler tests in testdata/tests/cases/compiler.

func TestOwnStateUnionKeepsFlagsAndSends(t *testing.T) {
	t.Parallel()
	s := newOwnState()
	s.addFlags(1, ownRegionCaller)
	send := &ownSend{callee: "spawn", via: "x"}
	s.addSend(2, send, false)
	root := s.union(1, 2)
	assert.Equal(t, s.find(1), root)
	assert.Equal(t, s.find(2), root)
	inf := s.info[root]
	assert.Equal(t, inf.flags, ownRegionCaller)
	assert.Equal(t, inf.sent, send)
	assert.Assert(t, !inf.maybe)
}

func TestOwnStateJoinMarksOneSidedSendsAsMaybe(t *testing.T) {
	t.Parallel()
	x := &ast.Symbol{Name: "x"}
	y := &ast.Symbol{Name: "y"}
	entry := newOwnState()
	entry.vars[x] = ownRegionValue(1)
	entry.vars[y] = ownRegionValue(2)

	sentInBoth := &ownSend{callee: "send", via: "y"}
	thenState := entry.copy()
	thenState.addSend(1, &ownSend{callee: "send", via: "x"}, false)
	thenState.addSend(2, sentInBoth, false)
	elseState := entry.copy()
	elseState.addSend(2, sentInBoth, false)

	joined := joinOwnStates(thenState, elseState)
	_, xInfo := joined.root(joined.vars[x])
	assert.Assert(t, xInfo.sent != nil && xInfo.maybe, "x is sent on one path only")
	_, yInfo := joined.root(joined.vars[y])
	assert.Assert(t, yInfo.sent != nil && !yInfo.maybe, "y is sent on both paths")
}

func TestOwnStateJoinMergesDifferentBindings(t *testing.T) {
	t.Parallel()
	x := &ast.Symbol{Name: "x"}
	entry := newOwnState()
	a := entry.copy()
	a.vars[x] = ownRegionValue(1)
	b := entry.copy()
	b.vars[x] = ownRegionValue(2)
	b.addFlags(2, ownRegionOuter)
	joined := joinOwnStates(a, b)
	assert.Equal(t, joined.find(1), joined.find(2))
	_, inf := joined.root(joined.vars[x])
	assert.Equal(t, inf.flags, ownRegionOuter)
}

func TestOwnStateJoinWithUnreachable(t *testing.T) {
	t.Parallel()
	x := &ast.Symbol{Name: "x"}
	a := newOwnState()
	a.vars[x] = ownRegionValue(1)
	a.addSend(1, &ownSend{callee: "send", via: "x"}, false)
	joined := joinOwnStates(a, newUnreachableOwnState())
	_, inf := joined.root(joined.vars[x])
	assert.Assert(t, inf.sent != nil && !inf.maybe, "a returning path does not make the send a maybe")
}

func TestOwnStateJoinConsumed(t *testing.T) {
	t.Parallel()
	x := &ast.Symbol{Name: "x"}
	a := newOwnState()
	a.vars[x] = ownValue{kind: ownConsumed, consumed: &ownSend{callee: "consume", via: "x", consume: true}}
	b := newOwnState()
	b.vars[x] = ownRegionValue(1)
	joined := joinOwnStates(a, b)
	assert.Equal(t, joined.vars[x].kind, ownConsumed)
	assert.Assert(t, joined.vars[x].maybe)
}

func TestOwnStateSignatureIgnoresRegionNumbering(t *testing.T) {
	t.Parallel()
	x := &ast.Symbol{Name: "x"}
	y := &ast.Symbol{Name: "y"}
	a := newOwnState()
	a.vars[x] = ownRegionValue(1)
	a.vars[y] = ownRegionValue(1)
	b := newOwnState()
	b.vars[x] = ownRegionValue(7)
	b.vars[y] = ownRegionValue(8)
	assert.Assert(t, a.signature() != b.signature(), "x and y share a region in a only")
	b.union(7, 8)
	assert.Equal(t, a.signature(), b.signature())
	b.addSend(8, &ownSend{callee: "send", via: "y"}, true)
	assert.Assert(t, a.signature() != b.signature())
}
