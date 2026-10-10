package checker

import (
	"slices"
	"strconv"
	"strings"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/diagnostics"
	"github.com/microsoft/typescript-go/internal/scanner"
)

// Ownership (region) analysis for the "own and shared threads" extension.
//
// The analysis is intraprocedural and flow-sensitive, in the style of Swift's region-based isolation
// (SE-0414). Every value an expression evaluates to is either SHARED (it has no region: primitives,
// values declared `shared T`, shared struct instances, code) or lives in a REGION, a set of objects
// that may reference each other. Regions are the classes of a union-find. A region can be SENT (it
// was handed to another owner through a `sending` parameter, `share(...)` and the like); using a
// variable whose region was sent is an error. Two kinds of regions can never be sent: CALLER regions
// (ordinary parameters and `this`, which the caller may still reference) and OUTER regions (variables
// captured from an enclosing scope).
//
// Deliberate approximations, all on the conservative side unless noted:
//   - A call to an ordinary function merges the regions of all of its non-shared arguments, its
//     receiver and (for closures) the callee, since the callee may link them together. Its result is
//     in that merged region.
//   - Sharedness comes from `shared` type nodes only (the checker erases them), followed through type
//     aliases, `readonly`, parentheses and unions with null/undefined, plus `const` declarations whose
//     initializer is shared. A value read out of a shared value is shared ("deep" sharedness).
//   - There is no interprocedural inference: a function's parameters are CALLER regions unless declared
//     `sending`, and calls are summarized by their declared `sending`/`shared` parameter and return
//     types only. Library functions declared with those types (`take`, `partition`, ...) are trusted.
//   - Values declared in the default library (`Math`, `JSON`, `console`, ...) and code (functions,
//     classes, enums, namespaces) referenced by name are treated as shared. This is not sound for
//     mutable library state, but otherwise every call on a library global would pin its arguments.
//   - Storing a non-shared value into a field of a shared struct instance is not checked yet.

const ownMaxLoopIterations = 4

type ownRegion int32

type ownValueKind uint8

const (
	ownShared   ownValueKind = iota // no region
	ownInRegion                     // in region `region`
	ownConsumed                     // a variable whose value was moved out by `consume`
)

type ownValue struct {
	kind     ownValueKind
	region   ownRegion
	consumed *ownSend // for ownConsumed
	maybe    bool     // for ownConsumed: consumed on some paths only
}

var ownSharedValue = ownValue{kind: ownShared}

func ownRegionValue(r ownRegion) ownValue {
	return ownValue{kind: ownInRegion, region: r}
}

type ownRegionFlags uint8

const (
	ownRegionCaller ownRegionFlags = 1 << iota // may be referenced by the caller
	ownRegionOuter                             // captured from an enclosing scope
)

// A send (or consume) of a region.
type ownSend struct {
	site    *ast.Node // the sent expression
	callee  string    // what it was sent to
	via     string    // the variable (or expression) through which it was sent
	consume bool      // consumed rather than sent: `consume x`, or a share-like function
}

type ownRegionInfo struct {
	flags ownRegionFlags
	sent  *ownSend
	maybe bool // sent on some paths only
}

// The abstract state at a program point.
type ownState struct {
	unreachable bool
	vars        map[*ast.Symbol]ownValue
	parent      map[ownRegion]ownRegion     // union-find links (absent for roots)
	info        map[ownRegion]ownRegionInfo // by root
}

func newOwnState() *ownState {
	return &ownState{vars: map[*ast.Symbol]ownValue{}, parent: map[ownRegion]ownRegion{}, info: map[ownRegion]ownRegionInfo{}}
}

func newUnreachableOwnState() *ownState {
	s := newOwnState()
	s.unreachable = true
	return s
}

func (s *ownState) copy() *ownState {
	r := &ownState{
		unreachable: s.unreachable,
		vars:        make(map[*ast.Symbol]ownValue, len(s.vars)),
		parent:      make(map[ownRegion]ownRegion, len(s.parent)),
		info:        make(map[ownRegion]ownRegionInfo, len(s.info)),
	}
	for k, v := range s.vars {
		r.vars[k] = v
	}
	for k, v := range s.parent {
		r.parent[k] = v
	}
	for k, v := range s.info {
		r.info[k] = v
	}
	return r
}

func (s *ownState) find(r ownRegion) ownRegion {
	for {
		p, ok := s.parent[r]
		if !ok {
			return r
		}
		r = p
	}
}

func (s *ownState) root(v ownValue) (ownRegion, ownRegionInfo) {
	r := s.find(v.region)
	return r, s.info[r]
}

func (s *ownState) addFlags(r ownRegion, flags ownRegionFlags) {
	r = s.find(r)
	inf := s.info[r]
	inf.flags |= flags
	s.info[r] = inf
}

func (s *ownState) addSend(r ownRegion, send *ownSend, maybe bool) {
	r = s.find(r)
	inf := s.info[r]
	if inf.sent == nil || inf.maybe && !maybe {
		inf.sent = send
		inf.maybe = maybe
	}
	s.info[r] = inf
}

func (s *ownState) union(a, b ownRegion) ownRegion {
	ra, rb := s.find(a), s.find(b)
	if ra == rb {
		return ra
	}
	ia, ib := s.info[ra], s.info[rb]
	s.parent[rb] = ra
	delete(s.info, rb)
	merged := ia
	merged.flags |= ib.flags
	if ib.sent != nil && (merged.sent == nil || merged.maybe && !ib.maybe) {
		merged.sent = ib.sent
		merged.maybe = ib.maybe
	}
	s.info[ra] = merged
	return ra
}

func (s *ownState) isSentDefinitely(r ownRegion) bool {
	inf := s.info[s.find(r)]
	return inf.sent != nil && !inf.maybe
}

// Merges two values into one: the union of their regions.
func (s *ownState) mergeValues(a, b ownValue) ownValue {
	switch {
	case a.kind == ownInRegion && b.kind == ownInRegion:
		return ownRegionValue(s.union(a.region, b.region))
	case a.kind == ownInRegion:
		return a
	case b.kind == ownInRegion:
		return b
	}
	return ownSharedValue
}

// Joins the states of two paths. Equivalences, flags and sends of both are kept; a region sent on
// only one of the paths is "maybe sent".
func joinOwnStates(a, b *ownState) *ownState {
	if a.unreachable {
		return b.copy()
	}
	if b.unreachable {
		return a.copy()
	}
	r := newOwnState()
	for k, v := range a.parent {
		r.parent[k] = v
	}
	for k, v := range b.parent {
		r.union(k, v)
	}
	for k, inf := range a.info {
		r.addFlags(k, inf.flags)
	}
	for k, inf := range b.info {
		r.addFlags(k, inf.flags)
	}
	for k, inf := range a.info {
		if inf.sent != nil {
			r.addSend(k, inf.sent, inf.maybe || !b.isSentDefinitely(k))
		}
	}
	for k, inf := range b.info {
		if inf.sent != nil {
			r.addSend(k, inf.sent, inf.maybe || !a.isSentDefinitely(k))
		}
	}
	for sym, va := range a.vars {
		vb, ok := b.vars[sym]
		if !ok {
			r.vars[sym] = va
			continue
		}
		r.vars[sym] = r.joinValues(va, vb)
	}
	for sym, vb := range b.vars {
		if _, ok := a.vars[sym]; !ok {
			r.vars[sym] = vb
		}
	}
	return r
}

func (s *ownState) joinValues(a, b ownValue) ownValue {
	switch {
	case a.kind == ownConsumed && b.kind == ownConsumed:
		return ownValue{kind: ownConsumed, consumed: a.consumed, maybe: a.maybe || b.maybe}
	case a.kind == ownConsumed:
		return ownValue{kind: ownConsumed, consumed: a.consumed, maybe: true}
	case b.kind == ownConsumed:
		return ownValue{kind: ownConsumed, consumed: b.consumed, maybe: true}
	}
	return s.mergeValues(a, b)
}

func joinAllOwnStates(first *ownState, rest []*ownState) *ownState {
	result := first
	for _, s := range rest {
		result = joinOwnStates(result, s)
	}
	return result
}

// A canonical description of the state, used to detect that a loop has reached a fixpoint.
func (s *ownState) signature() string {
	if s.unreachable {
		return "unreachable"
	}
	syms := make([]*ast.Symbol, 0, len(s.vars))
	for sym := range s.vars {
		syms = append(syms, sym)
	}
	slices.SortFunc(syms, func(a, b *ast.Symbol) int {
		return int(ast.GetSymbolId(a)) - int(ast.GetSymbolId(b))
	})
	roots := map[ownRegion]int{}
	var b strings.Builder
	for _, sym := range syms {
		v := s.vars[sym]
		b.WriteString(strconv.Itoa(int(ast.GetSymbolId(sym))))
		switch v.kind {
		case ownShared:
			b.WriteString("=s;")
		case ownConsumed:
			b.WriteString("=c")
			if v.maybe {
				b.WriteString("?")
			}
			b.WriteString(";")
		case ownInRegion:
			r, inf := s.root(v)
			index, ok := roots[r]
			if !ok {
				index = len(roots)
				roots[r] = index
			}
			b.WriteString("=r")
			b.WriteString(strconv.Itoa(index))
			b.WriteString("f")
			b.WriteString(strconv.Itoa(int(inf.flags)))
			if inf.sent != nil {
				b.WriteString(core.IfElse(inf.maybe, "s?", "s"))
			}
			b.WriteString(";")
		}
	}
	return b.String()
}

// Entry point

// Reports whether any file of the program may use the own/shared threads syntax. The analysis does
// not run otherwise.
func (c *Checker) programUsesOwnershipSyntax() bool {
	if c.ownershipSyntaxState == 0 {
		c.ownershipSyntaxState = 2
		for _, file := range c.files {
			if file.Flags&ast.NodeFlagsPossiblyContainsOwnershipSyntax != 0 {
				c.ownershipSyntaxState = 1
				break
			}
		}
	}
	return c.ownershipSyntaxState == 1
}

// Runs the ownership analysis on the top-level statements and every function body of a file.
func (c *Checker) checkOwnership(file *ast.SourceFile) {
	a := &ownAnalyzer{
		c:        c,
		results:  map[*ast.Node]*ownFunctionResult{},
		reported: map[ownReportKey]struct{}{},
		shared:   map[*ast.Symbol]bool{},
		captures: map[*ast.Node]*ownCaptures{},
	}
	a.analyzeFunction(file.AsNode())
	var visit ast.Visitor
	visit = func(node *ast.Node) bool {
		if ast.IsTypeNode(node) {
			return false
		}
		if isOwnFunctionBody(node) {
			a.analyzeFunction(node)
		}
		node.ForEachChild(visit)
		return false
	}
	file.AsNode().ForEachChild(visit)
}

// Function-like declarations whose body the analysis visits.
func isOwnFunctionBody(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction, ast.KindMethodDeclaration,
		ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor:
		return node.Body() != nil
	case ast.KindClassStaticBlockDeclaration:
		return true
	}
	return false
}

type ownReportKind uint8

const (
	ownReportUse ownReportKind = iota
	ownReportSend
	ownReportReturn
	ownReportWrite
)

type ownReportKey struct {
	node *ast.Node
	kind ownReportKind
}

type ownFunctionResult struct {
	returnsFresh bool // every returned value is shared or in a region not linked to the caller or outer scopes
}

type ownCaptures struct {
	symbols  []*ast.Symbol
	nodes    []*ast.Node // first reference of each symbol
	usesThis bool        // an arrow function (or a closure nested in one) uses the enclosing `this`
}

type ownAnalyzer struct {
	c          *Checker
	nextRegion ownRegion
	results    map[*ast.Node]*ownFunctionResult
	reported   map[ownReportKey]struct{}
	shared     map[*ast.Symbol]bool
	captures   map[*ast.Node]*ownCaptures
}

func (a *ownAnalyzer) newRegion() ownRegion {
	a.nextRegion++
	return a.nextRegion
}

type ownTarget struct {
	labels    []string
	isLoop    bool
	isSwitch  bool
	breaks    []*ownState
	continues []*ownState
}

// The analysis of one function body (or of the top-level statements of a file).
type ownFunction struct {
	a              *ownAnalyzer
	node           *ast.Node
	result         *ownFunctionResult
	returnsSending bool
	thisRegion     ownRegion
	thisFlags      ownRegionFlags
	targets        []*ownTarget
	building       map[*ast.Node]bool // closures whose value is being computed (mutually recursive local functions)
}

func (a *ownAnalyzer) analyzeFunction(node *ast.Node) *ownFunctionResult {
	if r := a.results[node]; r != nil {
		return r
	}
	r := &ownFunctionResult{returnsFresh: true}
	a.results[node] = r
	f := &ownFunction{a: a, node: node, result: r}
	s := newOwnState()
	var body *ast.Node
	switch {
	case ast.IsSourceFile(node):
		f.statements(node.Statements(), s)
		return r
	case ast.IsClassStaticBlockDeclaration(node):
		body = node.AsClassStaticBlockDeclaration().Body
	default:
		body = node.Body()
		f.returnsSending = a.returnsSending(node)
		for _, p := range node.Parameters() {
			if ast.IsThisParameter(p) {
				continue
			}
			f.bindName(p.Name(), p, f.parameterValue(p, s), s)
		}
	}
	if !ast.IsArrowFunction(node) {
		f.thisRegion = a.newRegion()
		f.thisFlags = ownRegionCaller
		s.addFlags(f.thisRegion, f.thisFlags)
	}
	if body == nil {
		return r
	}
	if ast.IsBlock(body) {
		f.statements(body.Statements(), s)
	} else {
		v := f.eval(body, s)
		f.checkReturn(body, v, s)
	}
	return r
}

func (f *ownFunction) parameterValue(p *ast.Node, s *ownState) ownValue {
	a := f.a
	if t := a.parameterTypeNode(p); t != nil && a.isSendingTypeNode(t) {
		// `sending T`: the function owns the argument.
		return ownRegionValue(a.newRegion())
	}
	if sym := a.c.getSymbolOfDeclaration(p); sym != nil && a.isSharedSymbol(sym) {
		return ownSharedValue
	}
	r := a.newRegion()
	s.addFlags(r, ownRegionCaller)
	return ownRegionValue(r)
}

func (f *ownFunction) thisValue(s *ownState) ownValue {
	if f.thisRegion == 0 {
		if ast.IsSourceFile(f.node) {
			return ownSharedValue
		}
		// `this` in an arrow function is captured from the enclosing function.
		f.thisRegion = f.a.newRegion()
		f.thisFlags = ownRegionOuter
	}
	// Re-apply the flags: the region may have been created on another path.
	s.addFlags(f.thisRegion, f.thisFlags)
	return ownRegionValue(f.thisRegion)
}

// Statements

func (f *ownFunction) statements(nodes []*ast.Node, s *ownState) {
	for _, node := range nodes {
		if s.unreachable {
			return
		}
		f.statement(node, s)
	}
}

func (f *ownFunction) statement(node *ast.Node, s *ownState) {
	switch node.Kind {
	case ast.KindBlock:
		f.statements(node.Statements(), s)
	case ast.KindVariableStatement:
		f.variableDeclarationList(node.AsVariableStatement().DeclarationList, s)
	case ast.KindExpressionStatement:
		f.eval(node.Expression(), s)
	case ast.KindIfStatement:
		n := node.AsIfStatement()
		f.eval(n.Expression, s)
		thenState := s.copy()
		f.statement(n.ThenStatement, thenState)
		if n.ElseStatement != nil {
			f.statement(n.ElseStatement, s)
		}
		*s = *joinOwnStates(thenState, s)
	case ast.KindReturnStatement:
		if expr := node.Expression(); expr != nil {
			v := f.eval(expr, s)
			f.checkReturn(expr, v, s)
		}
		s.unreachable = true
	case ast.KindThrowStatement:
		f.eval(node.Expression(), s)
		s.unreachable = true
	case ast.KindWhileStatement, ast.KindDoStatement, ast.KindForStatement, ast.KindForInStatement, ast.KindForOfStatement:
		f.loop(node, nil, s)
	case ast.KindLabeledStatement:
		f.labeledStatement(node, s)
	case ast.KindBreakStatement, ast.KindContinueStatement:
		f.jump(node, s)
	case ast.KindSwitchStatement:
		f.switchStatement(node, s)
	case ast.KindTryStatement:
		f.tryStatement(node, s)
	case ast.KindExportAssignment:
		f.eval(node.Expression(), s)
	case ast.KindWithStatement:
		f.eval(node.Expression(), s)
		f.statement(node.Statement(), s)
	}
	// Function and class declarations are analyzed on their own; the closures they create are
	// accounted for where they are referenced. Type declarations, imports and the like have no effect.
}

func (f *ownFunction) variableDeclarationList(list *ast.Node, s *ownState) {
	for _, decl := range list.AsVariableDeclarationList().Declarations.Nodes {
		v := ownSharedValue
		if init := decl.Initializer(); init != nil {
			v = f.eval(init, s)
		}
		f.bindName(decl.Name(), decl, v, s)
	}
}

// Binds the names declared by a variable, parameter or binding element. Every name in a binding
// pattern is bound to the region of the destructured value.
func (f *ownFunction) bindName(name *ast.Node, decl *ast.Node, v ownValue, s *ownState) {
	if name == nil {
		return
	}
	if ast.IsIdentifier(name) {
		sym := f.a.c.getSymbolOfDeclaration(decl)
		if sym != nil {
			s.vars[sym] = f.a.normalize(sym, v)
		}
		return
	}
	if ast.IsBindingPattern(name) {
		for _, element := range name.Elements() {
			if !ast.IsBindingElement(element) {
				continue
			}
			ev := v
			if init := element.Initializer(); init != nil {
				ev = s.mergeValues(ev, f.eval(init, s))
			}
			f.bindName(element.Name(), element, ev, s)
		}
	}
}

func (f *ownFunction) labeledStatement(node *ast.Node, s *ownState) {
	var labels []string
	for ast.IsLabeledStatement(node) {
		labels = append(labels, node.Label().Text())
		node = node.Statement()
	}
	switch node.Kind {
	case ast.KindWhileStatement, ast.KindDoStatement, ast.KindForStatement, ast.KindForInStatement, ast.KindForOfStatement:
		f.loop(node, labels, s)
		return
	}
	t := &ownTarget{labels: labels}
	f.targets = append(f.targets, t)
	f.statement(node, s)
	f.targets = f.targets[:len(f.targets)-1]
	*s = *joinAllOwnStates(s, t.breaks)
}

func (f *ownFunction) jump(node *ast.Node, s *ownState) {
	isBreak := node.Kind == ast.KindBreakStatement
	label := ""
	if l := node.Label(); l != nil {
		label = l.Text()
	}
	for i := len(f.targets) - 1; i >= 0; i-- {
		t := f.targets[i]
		switch {
		case label != "":
			if !slices.Contains(t.labels, label) {
				continue
			}
		case isBreak:
			if !t.isLoop && !t.isSwitch {
				continue
			}
		default:
			if !t.isLoop {
				continue
			}
		}
		if isBreak {
			t.breaks = append(t.breaks, s.copy())
		} else {
			t.continues = append(t.continues, s.copy())
		}
		break
	}
	s.unreachable = true
}

// Loops are iterated until the state at the head stops changing, so that a send in one iteration is
// seen by the uses in the next.
func (f *ownFunction) loop(node *ast.Node, labels []string, s *ownState) {
	var iterated ownValue
	switch node.Kind {
	case ast.KindForStatement:
		if init := node.AsForStatement().Initializer; init != nil {
			if ast.IsVariableDeclarationList(init) {
				f.variableDeclarationList(init, s)
			} else {
				f.eval(init, s)
			}
		}
	case ast.KindForOfStatement:
		iterated = f.a.elementValue(node.Expression(), f.eval(node.Expression(), s))
	case ast.KindForInStatement:
		f.eval(node.Expression(), s)
		iterated = ownSharedValue // property names
	}
	head := s.copy()
	headSignature := head.signature()
	var exit *ownState
	var breaks []*ownState
	for range ownMaxLoopIterations {
		st := head.copy()
		t := &ownTarget{labels: labels, isLoop: true}
		f.targets = append(f.targets, t)
		switch node.Kind {
		case ast.KindWhileStatement:
			f.eval(node.Expression(), st)
			exit = st.copy()
			f.statement(node.Statement(), st)
			st = joinAllOwnStates(st, t.continues)
		case ast.KindDoStatement:
			f.statement(node.Statement(), st)
			st = joinAllOwnStates(st, t.continues)
			f.eval(node.Expression(), st)
			exit = st.copy()
		case ast.KindForStatement:
			n := node.AsForStatement()
			if n.Condition != nil {
				f.eval(n.Condition, st)
				exit = st.copy()
			} else {
				exit = newUnreachableOwnState()
			}
			f.statement(n.Statement, st)
			st = joinAllOwnStates(st, t.continues)
			if n.Incrementor != nil && !st.unreachable {
				f.eval(n.Incrementor, st)
			}
		case ast.KindForInStatement, ast.KindForOfStatement:
			exit = st.copy()
			n := node.AsForInOrOfStatement()
			if ast.IsVariableDeclarationList(n.Initializer) {
				for _, decl := range n.Initializer.AsVariableDeclarationList().Declarations.Nodes {
					f.bindName(decl.Name(), decl, iterated, st)
				}
			} else {
				f.assign(n.Initializer, iterated, st)
			}
			f.statement(n.Statement, st)
			st = joinAllOwnStates(st, t.continues)
		}
		f.targets = f.targets[:len(f.targets)-1]
		breaks = t.breaks
		next := joinOwnStates(head, st)
		nextSignature := next.signature()
		head = next
		if nextSignature == headSignature {
			break
		}
		headSignature = nextSignature
	}
	*s = *joinAllOwnStates(exit, breaks)
}

func (f *ownFunction) switchStatement(node *ast.Node, s *ownState) {
	n := node.AsSwitchStatement()
	f.eval(n.Expression, s)
	t := &ownTarget{isSwitch: true}
	f.targets = append(f.targets, t)
	afterExpression := s.copy()
	current := newUnreachableOwnState()
	hasDefault := false
	for _, clause := range n.CaseBlock.AsCaseBlock().Clauses.Nodes {
		entry := joinOwnStates(afterExpression, current) // falls through from the previous clause
		if clause.Kind == ast.KindCaseClause {
			f.eval(clause.Expression(), entry)
		} else {
			hasDefault = true
		}
		f.statements(clause.Statements(), entry)
		current = entry
	}
	f.targets = f.targets[:len(f.targets)-1]
	result := joinAllOwnStates(current, t.breaks)
	if !hasDefault {
		result = joinOwnStates(result, afterExpression)
	}
	*s = *result
}

// try/catch/finally is analyzed conservatively: the catch and finally blocks start from the join of
// the try block's entry and exit states.
func (f *ownFunction) tryStatement(node *ast.Node, s *ownState) {
	n := node.AsTryStatement()
	entry := s.copy()
	f.statement(n.TryBlock, s)
	after := s
	if n.CatchClause != nil {
		catchClause := n.CatchClause.AsCatchClause()
		cs := joinOwnStates(entry, s)
		if decl := catchClause.VariableDeclaration; decl != nil {
			// Whatever was thrown may be referenced from anywhere.
			r := f.a.newRegion()
			cs.addFlags(r, ownRegionOuter)
			f.bindName(decl.Name(), decl, ownRegionValue(r), cs)
		}
		f.statement(catchClause.Block, cs)
		after = joinOwnStates(s, cs)
	}
	if n.FinallyBlock != nil {
		fs := joinOwnStates(entry, after)
		f.statement(n.FinallyBlock, fs)
		if after.unreachable {
			fs.unreachable = true
		}
		after = fs
	}
	*s = *after
}

func (f *ownFunction) checkReturn(expr *ast.Node, v ownValue, s *ownState) {
	if v.kind != ownInRegion {
		return
	}
	_, inf := s.root(v)
	if inf.flags != 0 || inf.sent != nil {
		f.result.returnsFresh = false
	}
	if f.returnsSending && inf.flags != 0 {
		f.a.report(ownReportKey{expr, ownReportReturn}, expr, diagnostics.X_0_cannot_be_returned_as_sending_Colon_it_may_be_referenced_by_the_caller, ownExpressionName(expr))
	}
}

// Expressions

func (f *ownFunction) eval(node *ast.Node, s *ownState) ownValue {
	if node == nil {
		return ownSharedValue
	}
	a := f.a
	switch node.Kind {
	case ast.KindIdentifier:
		return f.readIdentifier(node, s)
	case ast.KindThisKeyword, ast.KindSuperKeyword:
		return f.thisValue(s)
	case ast.KindNumericLiteral, ast.KindBigIntLiteral, ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral,
		ast.KindTrueKeyword, ast.KindFalseKeyword, ast.KindNullKeyword, ast.KindMetaProperty, ast.KindOmittedExpression,
		ast.KindPrivateIdentifier:
		return ownSharedValue
	case ast.KindRegularExpressionLiteral:
		return ownRegionValue(a.newRegion())
	case ast.KindParenthesizedExpression, ast.KindNonNullExpression, ast.KindAsExpression, ast.KindSatisfiesExpression,
		ast.KindTypeAssertionExpression, ast.KindPartiallyEmittedExpression, ast.KindAwaitExpression:
		return f.eval(node.Expression(), s)
	case ast.KindConsumeExpression:
		return f.evalConsume(node, s)
	case ast.KindTemplateExpression:
		for _, span := range node.AsTemplateExpression().TemplateSpans.Nodes {
			f.eval(span.Expression(), s)
		}
		return ownSharedValue
	case ast.KindTypeOfExpression, ast.KindVoidExpression:
		f.eval(node.Expression(), s)
		return ownSharedValue
	case ast.KindDeleteExpression:
		f.evalWriteTarget(node.Expression(), s)
		return ownSharedValue
	case ast.KindPrefixUnaryExpression:
		n := node.AsPrefixUnaryExpression()
		if n.Operator == ast.KindPlusPlusToken || n.Operator == ast.KindMinusMinusToken {
			f.evalWriteTarget(n.Operand, s)
		} else {
			f.eval(n.Operand, s)
		}
		return ownSharedValue
	case ast.KindPostfixUnaryExpression:
		f.evalWriteTarget(node.AsPostfixUnaryExpression().Operand, s)
		return ownSharedValue
	case ast.KindYieldExpression:
		f.eval(node.Expression(), s)
		// The value sent into the generator comes from its caller.
		r := a.newRegion()
		s.addFlags(r, ownRegionCaller)
		return ownRegionValue(r)
	case ast.KindBinaryExpression:
		return f.evalBinary(node, s)
	case ast.KindConditionalExpression:
		n := node.AsConditionalExpression()
		f.eval(n.Condition, s)
		whenFalse := s.copy()
		vt := f.eval(n.WhenTrue, s)
		vf := f.eval(n.WhenFalse, whenFalse)
		*s = *joinOwnStates(s, whenFalse)
		return s.mergeValues(vt, vf)
	case ast.KindObjectLiteralExpression:
		return f.evalObjectLiteral(node, s)
	case ast.KindArrayLiteralExpression:
		r := a.newRegion()
		for _, element := range node.Elements() {
			if v := f.eval(element, s); v.kind == ownInRegion {
				s.union(r, v.region)
			}
		}
		return ownRegionValue(s.find(r))
	case ast.KindSpreadElement:
		return f.eval(node.Expression(), s)
	case ast.KindFunctionExpression, ast.KindArrowFunction, ast.KindClassExpression:
		return f.closureValue(node, s)
	case ast.KindCallExpression:
		return f.evalCall(node, s)
	case ast.KindNewExpression:
		return f.evalNew(node, s)
	case ast.KindTaggedTemplateExpression:
		n := node.AsTaggedTemplateExpression()
		v := f.eval(n.Tag, s)
		if ast.IsTemplateExpression(n.Template) {
			for _, span := range n.Template.AsTemplateExpression().TemplateSpans.Nodes {
				v = s.mergeValues(v, f.eval(span.Expression(), s))
			}
		}
		if a.isPrimitiveOrSharedStructType(a.c.getTypeOfExpression(node)) {
			return ownSharedValue
		}
		return v
	case ast.KindPropertyAccessExpression:
		obj := f.eval(node.Expression(), s)
		return f.readMember(node, node.Expression(), obj)
	case ast.KindElementAccessExpression:
		obj := f.eval(node.Expression(), s)
		f.eval(node.AsElementAccessExpression().ArgumentExpression, s)
		return f.readMember(node, node.Expression(), obj)
	}
	return f.evalChildren(node, s)
}

// Fallback for expressions without a specific rule (JSX and the like): a fresh region holding the
// regions of all subexpressions.
func (f *ownFunction) evalChildren(node *ast.Node, s *ownState) ownValue {
	r := f.a.newRegion()
	node.ForEachChild(func(child *ast.Node) bool {
		if ast.IsTypeNode(child) || ast.IsIdentifier(child) && !isOwnValueReference(child) {
			return false
		}
		if ast.IsExpression(child) || ast.IsJsxChild(child) || ast.IsJsxAttributeLike(child) || ast.IsJsxAttributes(child) {
			if v := f.eval(child, s); v.kind == ownInRegion {
				s.union(r, v.region)
			}
		} else {
			child.ForEachChild(func(grandchild *ast.Node) bool {
				if ast.IsExpression(grandchild) && !ast.IsIdentifier(grandchild) {
					if v := f.eval(grandchild, s); v.kind == ownInRegion {
						s.union(r, v.region)
					}
				}
				return false
			})
		}
		return false
	})
	return ownRegionValue(s.find(r))
}

func (f *ownFunction) readIdentifier(id *ast.Node, s *ownState) ownValue {
	sym := f.a.symbolOfIdentifier(id)
	if sym == nil {
		return ownSharedValue
	}
	v := f.lookup(sym, s)
	if f.checkUse(id, id.Text(), v, s) && v.kind == ownConsumed {
		return ownSharedValue
	}
	return v
}

// The value of a variable in this function: its binding, a closure for a local function
// declaration, or (for variables of enclosing scopes) shared or an OUTER region.
func (f *ownFunction) lookup(sym *ast.Symbol, s *ownState) ownValue {
	if v, ok := s.vars[sym]; ok {
		return v
	}
	a := f.a
	if a.isLocalTo(sym, f.node) {
		decl := sym.ValueDeclaration
		if decl != nil && !ast.IsSourceFile(f.node) && (ast.IsFunctionDeclaration(decl) || ast.IsClassDeclaration(decl)) {
			// A nested function or class: a closure over the variables it captures.
			return f.closureValue(decl, s)
		}
		if a.isSharedSymbol(sym) {
			return ownSharedValue
		}
		// Declared in this function but not initialized yet (or hoisted).
		return ownSharedValue
	}
	if a.isSharedSymbol(sym) {
		return ownSharedValue
	}
	r := a.newRegion()
	s.addFlags(r, ownRegionOuter)
	v := ownRegionValue(r)
	s.vars[sym] = v
	return v
}

// Reports a use of a variable whose region was sent (or whose value was consumed). Returns true if
// it reported an error.
func (f *ownFunction) checkUse(node *ast.Node, name string, v ownValue, s *ownState) bool {
	switch v.kind {
	case ownConsumed:
		f.a.reportUseAfterSend(node, name, v.consumed, v.maybe)
		return true
	case ownInRegion:
		if _, inf := s.root(v); inf.sent != nil {
			f.a.reportUseAfterSend(node, name, inf.sent, inf.maybe)
			return true
		}
	}
	return false
}

// `consume x` hands x's value over to the enclosing expression and ends x's lifetime: later uses of
// x are errors. The value itself is only sent if it is then passed to a `sending` parameter.
func (f *ownFunction) evalConsume(node *ast.Node, s *ownState) ownValue {
	operand := node.Expression()
	v := f.eval(operand, s)
	if ast.IsIdentifier(operand) {
		if sym := f.a.symbolOfIdentifier(operand); sym != nil && f.a.isLocalTo(sym, f.node) {
			callee := "consume"
			if call := ast.WalkUpParenthesizedExpressions(node.Parent); call != nil && ast.IsCallOrNewExpression(call) && call.Expression() != node {
				callee = ownCalleeName(call.Expression())
			}
			s.vars[sym] = ownValue{kind: ownConsumed, consumed: &ownSend{site: node, callee: callee, via: operand.Text(), consume: true}}
		}
	}
	return v
}

func (f *ownFunction) evalBinary(node *ast.Node, s *ownState) ownValue {
	n := node.AsBinaryExpression()
	op := n.OperatorToken.Kind
	switch {
	case op == ast.KindEqualsToken:
		v := f.eval(n.Right, s)
		f.assign(n.Left, v, s)
		return v
	case op == ast.KindAmpersandAmpersandEqualsToken || op == ast.KindBarBarEqualsToken || op == ast.KindQuestionQuestionEqualsToken:
		old := f.eval(n.Left, s)
		assigned := s.copy()
		v := assigned.mergeValues(old, f.eval(n.Right, assigned))
		f.assign(n.Left, v, assigned)
		*s = *joinOwnStates(s, assigned)
		return v
	case ast.IsAssignmentOperator(op):
		// Compound assignment: reads and writes the target.
		f.evalWriteTarget(n.Left, s)
		f.eval(n.Right, s)
		return ownSharedValue
	case op == ast.KindAmpersandAmpersandToken || op == ast.KindBarBarToken || op == ast.KindQuestionQuestionToken:
		left := f.eval(n.Left, s)
		right := s.copy()
		vr := f.eval(n.Right, right)
		*s = *joinOwnStates(s, right)
		return s.mergeValues(left, vr)
	case op == ast.KindCommaToken:
		left := f.eval(n.Left, s)
		return s.mergeValues(left, f.eval(n.Right, s))
	}
	f.eval(n.Left, s)
	f.eval(n.Right, s)
	if op == ast.KindPlusToken && !f.a.isPrimitiveOrSharedStructType(f.a.c.getTypeOfExpression(node)) {
		return ownRegionValue(f.a.newRegion())
	}
	return ownSharedValue
}

// Evaluates the target of `x++`, `delete a.b`, `a.b += 1` and the like, reporting writes through
// shared values.
func (f *ownFunction) evalWriteTarget(target *ast.Node, s *ownState) {
	target = ast.SkipParentheses(target)
	switch target.Kind {
	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		obj := f.eval(target.Expression(), s)
		if ast.IsElementAccessExpression(target) {
			f.eval(target.AsElementAccessExpression().ArgumentExpression, s)
		}
		f.checkWrite(target, target.Expression(), obj)
	default:
		f.eval(target, s)
	}
}

// Assigns a value to an assignment target: rebinds a local variable, merges into the OUTER region of
// a captured one, or links the value to the object whose property is written.
func (f *ownFunction) assign(target *ast.Node, v ownValue, s *ownState) {
	target = ast.SkipOuterExpressions(target, ast.OEKAssertions|ast.OEKParentheses)
	a := f.a
	switch target.Kind {
	case ast.KindIdentifier:
		sym := a.symbolOfIdentifier(target)
		if sym == nil {
			return
		}
		if a.isLocalTo(sym, f.node) {
			s.vars[sym] = a.normalize(sym, v)
			return
		}
		s.mergeValues(f.lookup(sym, s), v)
	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		obj := f.eval(target.Expression(), s)
		if ast.IsElementAccessExpression(target) {
			f.eval(target.AsElementAccessExpression().ArgumentExpression, s)
		}
		f.checkWrite(target, target.Expression(), obj)
		if obj.kind == ownInRegion {
			s.mergeValues(obj, v)
		}
	case ast.KindArrayLiteralExpression:
		for _, element := range target.Elements() {
			switch {
			case ast.IsOmittedExpression(element):
			case ast.IsSpreadElement(element):
				f.assign(element.Expression(), v, s)
			case ast.IsBinaryExpression(element) && element.AsBinaryExpression().OperatorToken.Kind == ast.KindEqualsToken:
				f.assign(element.AsBinaryExpression().Left, s.mergeValues(v, f.eval(element.AsBinaryExpression().Right, s)), s)
			default:
				f.assign(element, v, s)
			}
		}
	case ast.KindObjectLiteralExpression:
		for _, property := range target.AsObjectLiteralExpression().Properties.Nodes {
			switch property.Kind {
			case ast.KindPropertyAssignment:
				if ast.IsComputedPropertyName(property.Name()) {
					f.eval(property.Name().Expression(), s)
				}
				init := property.Initializer()
				if ast.IsBinaryExpression(init) && init.AsBinaryExpression().OperatorToken.Kind == ast.KindEqualsToken {
					f.assign(init.AsBinaryExpression().Left, s.mergeValues(v, f.eval(init.AsBinaryExpression().Right, s)), s)
				} else {
					f.assign(init, v, s)
				}
			case ast.KindShorthandPropertyAssignment:
				pv := v
				if init := property.AsShorthandPropertyAssignment().ObjectAssignmentInitializer; init != nil {
					pv = s.mergeValues(pv, f.eval(init, s))
				}
				f.assign(property.Name(), pv, s)
			case ast.KindSpreadAssignment:
				f.assign(property.Expression(), v, s)
			}
		}
	default:
		f.eval(target, s)
	}
}

// Reports a write to a property or element of a shared value. Fields of shared struct instances
// are mutable cells, so writing them is allowed.
func (f *ownFunction) checkWrite(target *ast.Node, object *ast.Node, obj ownValue) {
	if obj.kind != ownShared || !f.a.isImmutableShared(object) {
		return
	}
	f.a.report(ownReportKey{target, ownReportWrite}, target, diagnostics.Cannot_change_0_Colon_it_is_a_shared_value_which_is_immutable, ownExpressionName(object))
}

// The value of `o.p` or `o[i]` given the value of `o`: shared if `o` is shared (sharedness is deep),
// if the member is declared `shared`, or if it is a primitive; otherwise in `o`'s region.
func (f *ownFunction) readMember(node *ast.Node, object *ast.Node, obj ownValue) ownValue {
	a := f.a
	if obj.kind != ownInRegion {
		return ownSharedValue
	}
	if ast.IsPropertyAccessExpression(node) {
		if prop := a.c.getSymbolAtLocation(node.Name(), true /*ignoreErrors*/); prop != nil &&
			(prop.Flags&ast.SymbolFlagsMethod != 0 || a.isDeclaredShared(prop)) {
			return ownSharedValue
		}
	} else if a.isElementDeclaredShared(object) {
		return ownSharedValue
	}
	if a.isPrimitiveOrSharedStructType(a.c.getTypeOfExpression(node)) {
		return ownSharedValue
	}
	return obj
}

func (f *ownFunction) evalObjectLiteral(node *ast.Node, s *ownState) ownValue {
	r := f.a.newRegion()
	add := func(v ownValue) {
		if v.kind == ownInRegion {
			s.union(r, v.region)
		}
	}
	for _, property := range node.AsObjectLiteralExpression().Properties.Nodes {
		if name := property.Name(); name != nil && ast.IsComputedPropertyName(name) {
			f.eval(name.Expression(), s)
		}
		switch property.Kind {
		case ast.KindPropertyAssignment:
			add(f.eval(property.Initializer(), s))
		case ast.KindShorthandPropertyAssignment:
			add(f.readIdentifier(property.Name(), s))
		case ast.KindSpreadAssignment:
			add(f.eval(property.Expression(), s))
		case ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor:
			add(f.closureValue(property, s))
		}
	}
	return ownRegionValue(s.find(r))
}

// The value of a closure (function, arrow function, class or object literal method): a fresh region
// merged with the regions of the variables it captures.
func (f *ownFunction) closureValue(node *ast.Node, s *ownState) ownValue {
	r := f.a.newRegion()
	if f.building[node] {
		return ownRegionValue(r)
	}
	if f.building == nil {
		f.building = map[*ast.Node]bool{}
	}
	f.building[node] = true
	defer delete(f.building, node)
	captures := f.a.capturesOf(node)
	for i, sym := range captures.symbols {
		v := f.lookup(sym, s)
		// A captured variable that was already sent is reported once, here; it is not linked to the
		// closure, so that uses of the closure do not report it again.
		if !f.checkUse(captures.nodes[i], sym.Name, v, s) && v.kind == ownInRegion {
			s.union(r, v.region)
		}
	}
	if captures.usesThis {
		if v := f.thisValue(s); v.kind == ownInRegion {
			s.union(r, v.region)
		}
	}
	return ownRegionValue(s.find(r))
}

type ownReturnKind uint8

const (
	ownReturnsOrdinary ownReturnKind = iota
	ownReturnsSending
	ownReturnsShared
)

func (f *ownFunction) evalCall(node *ast.Node, s *ownState) ownValue {
	a := f.a
	c := a.c
	call := node.AsCallExpression()
	callee := ast.SkipOuterExpressions(call.Expression, ast.OEKParentheses|ast.OEKNonNullAssertions)
	var regions []ownRegion
	add := func(v ownValue) {
		if v.kind == ownInRegion {
			regions = append(regions, v.region)
		}
	}
	if callee.Kind == ast.KindImportKeyword {
		// `import(...)`: the module namespace is a fresh object.
		for _, arg := range call.Arguments.Nodes {
			f.eval(arg, s)
		}
		return ownRegionValue(a.newRegion())
	}
	name := ownCalleeName(callee)
	var receiverExpr *ast.Node
	receiver := ownSharedValue
	switch {
	case ast.IsPropertyAccessExpression(callee) || ast.IsElementAccessExpression(callee):
		receiverExpr = callee.Expression()
		receiver = f.eval(receiverExpr, s)
		if ast.IsElementAccessExpression(callee) {
			f.eval(callee.AsElementAccessExpression().ArgumentExpression, s)
		}
		add(receiver)
	default:
		add(f.eval(callee, s))
	}
	sig := c.getResolvedSignature(node, nil, CheckModeNormal)
	var decl *ast.Node
	if sig != nil {
		decl = sig.declaration
	}
	if receiverExpr != nil && ownArrayMutators[name] && receiver.kind == ownShared && a.isArrayExpression(receiverExpr) && a.isImmutableShared(receiverExpr) {
		a.report(ownReportKey{node, ownReportWrite}, callee, diagnostics.Cannot_change_0_Colon_it_is_a_shared_value_which_is_immutable, ownExpressionName(receiverExpr))
	}
	params := ownSignatureParameters(decl)
	consuming := a.isConsumingSignature(decl)
	var callback *ast.Node
	if receiverExpr != nil && (name == "map" || name == "flatMap") && len(call.Arguments.Nodes) > 0 && a.isLibArrayMethod(decl) {
		if cb := ast.SkipParentheses(call.Arguments.Nodes[0]); ast.IsFunctionExpressionOrArrowFunction(cb) {
			callback = cb
		}
	}
	var otherArgs []ownRegion // arguments other than the receiver and a map callback
	for i, arg := range call.Arguments.Nodes {
		expr := arg
		if ast.IsSpreadElement(arg) {
			expr = arg.Expression()
		}
		v := f.eval(expr, s)
		if param := ownParameterAt(params, i); param != nil && a.isSendingTypeNode(param.Type()) {
			f.send(v, expr, name, consuming, s)
			continue
		}
		add(v)
		if v.kind == ownInRegion && !(callback != nil && i == 0) {
			otherArgs = append(otherArgs, v.region)
		}
	}
	switch a.returnKind(decl) {
	case ownReturnsSending:
		f.unionAll(regions, s)
		return ownRegionValue(a.newRegion())
	case ownReturnsShared:
		f.unionAll(regions, s)
		return ownSharedValue
	}
	if callback != nil && a.analyzeFunction(callback).returnsFresh {
		// map/flatMap with a callback that builds fresh values: the result is a fresh array.
		f.unionAll(otherArgs, s)
		return ownRegionValue(a.newRegion())
	}
	r := f.unionAll(regions, s)
	if a.isPrimitiveOrSharedStructType(c.getTypeOfExpression(node)) {
		return ownSharedValue
	}
	if r == 0 {
		r = a.newRegion()
	}
	return ownRegionValue(r)
}

func (f *ownFunction) evalNew(node *ast.Node, s *ownState) ownValue {
	a := f.a
	n := node.AsNewExpression()
	if a.isSharedStructReference(n.Expression) {
		// `new S(...)` of a shared struct: the instance is shared.
		if n.Arguments != nil {
			for _, arg := range n.Arguments.Nodes {
				f.eval(arg, s)
			}
		}
		return ownSharedValue
	}
	var regions []ownRegion
	if v := f.eval(n.Expression, s); v.kind == ownInRegion {
		regions = append(regions, v.region)
	}
	sig := a.c.getResolvedSignature(node, nil, CheckModeNormal)
	var decl *ast.Node
	if sig != nil {
		decl = sig.declaration
	}
	params := ownSignatureParameters(decl)
	name := ownCalleeName(n.Expression)
	consuming := a.isConsumingSignature(decl)
	if n.Arguments != nil {
		for i, arg := range n.Arguments.Nodes {
			expr := arg
			if ast.IsSpreadElement(arg) {
				expr = arg.Expression()
			}
			v := f.eval(expr, s)
			if param := ownParameterAt(params, i); param != nil && a.isSendingTypeNode(param.Type()) {
				f.send(v, expr, name, consuming, s)
				continue
			}
			if v.kind == ownInRegion {
				regions = append(regions, v.region)
			}
		}
	}
	switch a.returnKind(decl) {
	case ownReturnsSending:
		f.unionAll(regions, s)
		return ownRegionValue(a.newRegion())
	case ownReturnsShared:
		f.unionAll(regions, s)
		return ownSharedValue
	}
	if a.isPrimitiveOrSharedStructType(a.c.getTypeOfExpression(node)) {
		f.unionAll(regions, s)
		return ownSharedValue
	}
	// A new object: a fresh region holding the (non-shared) constructor arguments.
	r := a.newRegion()
	for _, region := range regions {
		s.union(r, region)
	}
	return ownRegionValue(s.find(r))
}

// Merges regions and returns the root, or 0 if there are none.
func (f *ownFunction) unionAll(regions []ownRegion, s *ownState) ownRegion {
	var r ownRegion
	for _, region := range regions {
		if r == 0 {
			r = s.find(region)
		} else {
			r = s.union(r, region)
		}
	}
	return r
}

// Sends a value: passes it to a `sending` parameter (or a share-like function, which consumes it).
func (f *ownFunction) send(v ownValue, expr *ast.Node, callee string, consuming bool, s *ownState) {
	if v.kind != ownInRegion {
		return
	}
	root, inf := s.root(v)
	name := ownExpressionName(expr)
	switch {
	case inf.flags&ownRegionCaller != 0:
		f.a.report(ownReportKey{expr, ownReportSend}, expr, diagnostics.X_0_cannot_be_sent_Colon_it_may_be_referenced_by_the_caller_Declare_the_parameter_sending_to_take_ownership, name)
	case inf.flags&ownRegionOuter != 0:
		f.a.report(ownReportKey{expr, ownReportSend}, expr, diagnostics.X_0_cannot_be_sent_Colon_it_is_captured_from_an_outer_scope, name)
	case inf.sent != nil && !inf.maybe:
		// Already sent: the use of the sent variable has been reported.
	default:
		s.addSend(root, &ownSend{site: expr, callee: callee, via: name, consume: consuming}, false)
	}
}

// Reporting

func (a *ownAnalyzer) report(key ownReportKey, node *ast.Node, message *diagnostics.Message, args ...any) *ast.Diagnostic {
	if _, ok := a.reported[key]; ok {
		return nil
	}
	a.reported[key] = struct{}{}
	return a.c.error(node, message, args...)
}

func (a *ownAnalyzer) reportUseAfterSend(node *ast.Node, name string, send *ownSend, maybe bool) {
	line := strconv.Itoa(ownLineOf(send.site))
	key := ownReportKey{node, ownReportUse}
	var d *ast.Diagnostic
	switch {
	case send.consume && send.via == name:
		d = a.report(key, node, core.IfElse(maybe, diagnostics.X_0_may_have_been_consumed_by_1_on_line_2, diagnostics.X_0_was_consumed_by_1_on_line_2), name, send.callee, line)
	case send.via == name:
		d = a.report(key, node, core.IfElse(maybe, diagnostics.X_0_cannot_be_used_here_Colon_it_may_have_been_sent_to_1_on_line_2, diagnostics.X_0_cannot_be_used_here_Colon_it_was_sent_to_1_on_line_2), name, send.callee, line)
	default:
		d = a.report(key, node, core.IfElse(maybe, diagnostics.X_0_cannot_be_used_here_Colon_it_shares_objects_with_1_which_may_have_been_sent_to_2_on_line_3, diagnostics.X_0_cannot_be_used_here_Colon_it_shares_objects_with_1_which_was_sent_to_2_on_line_3), name, send.via, send.callee, line)
	}
	if d != nil {
		d.AddRelatedInfo(createDiagnosticForNode(send.site, core.IfElse(send.consume, diagnostics.X_0_is_consumed_here, diagnostics.X_0_is_sent_here), send.via))
	}
}

func ownLineOf(node *ast.Node) int {
	file := ast.GetSourceFileOfNode(node)
	return scanner.GetECMALineOfPosition(file, scanner.SkipTrivia(file.Text(), node.Pos())) + 1
}

// A short name for an expression in a message: the variable for `x` and `consume x`, otherwise the
// (abbreviated) source text.
func ownExpressionName(expr *ast.Node) string {
	expr = ast.SkipParentheses(expr)
	if ast.IsConsumeExpression(expr) {
		expr = expr.Expression()
	}
	if ast.IsIdentifier(expr) {
		return expr.Text()
	}
	text := strings.Join(strings.Fields(scanner.GetTextOfNode(expr)), " ")
	if len(text) > 40 {
		text = text[:37] + "..."
	}
	return text
}

func ownCalleeName(callee *ast.Node) string {
	callee = ast.SkipParentheses(callee)
	switch {
	case ast.IsIdentifier(callee):
		return callee.Text()
	case ast.IsPropertyAccessExpression(callee):
		return callee.Name().Text()
	}
	return ownExpressionName(callee)
}

var ownArrayMutators = map[string]bool{
	"push": true, "pop": true, "shift": true, "unshift": true, "splice": true,
	"sort": true, "reverse": true, "fill": true, "copyWithin": true,
}

// Symbols, declarations and type nodes

// Resolves an identifier in an expression without reporting errors.
func (a *ownAnalyzer) symbolOfIdentifier(id *ast.Node) *ast.Symbol {
	sym := a.c.getResolvedSymbolOrNil(id)
	if sym == nil {
		sym = a.c.resolveName(id, id.Text(), ast.SymbolFlagsValue|ast.SymbolFlagsExportValue, nil, false /*isUse*/, false /*excludeGlobals*/)
	}
	if sym == nil || sym == a.c.unknownSymbol {
		return nil
	}
	return sym
}

// Reports whether a symbol is declared in the function (or file, for top-level code) `owner`.
func (a *ownAnalyzer) isLocalTo(sym *ast.Symbol, owner *ast.Node) bool {
	if sym.Flags&ast.SymbolFlagsAlias != 0 {
		return false
	}
	decl := sym.ValueDeclaration
	if decl == nil && len(sym.Declarations) > 0 {
		decl = sym.Declarations[0]
	}
	if decl == nil {
		return false
	}
	return ownOwnerOf(decl) == owner
}

// The function body or file in which a declaration lives.
func ownOwnerOf(decl *ast.Node) *ast.Node {
	for n := decl.Parent; n != nil; n = n.Parent {
		if isOwnFunctionBody(n) || ast.IsSourceFile(n) {
			return n
		}
	}
	return nil
}

// Binds a value to a variable, making it shared if the variable is declared shared (or is a
// primitive).
func (a *ownAnalyzer) normalize(sym *ast.Symbol, v ownValue) ownValue {
	if v.kind == ownInRegion && a.isSharedSymbol(sym) {
		return ownSharedValue
	}
	return v
}

// Reports whether a symbol's value is shared regardless of the program point: code, library values,
// shared structs, variables declared `shared` and variables of primitive types.
func (a *ownAnalyzer) isSharedSymbol(sym *ast.Symbol) bool {
	if result, ok := a.shared[sym]; ok {
		return result
	}
	a.shared[sym] = false // breaks cycles
	result := a.computeIsSharedSymbol(sym)
	a.shared[sym] = result
	return result
}

func (a *ownAnalyzer) computeIsSharedSymbol(sym *ast.Symbol) bool {
	c := a.c
	target := sym
	if sym.Flags&ast.SymbolFlagsAlias != 0 {
		target = c.resolveAlias(sym)
		if target == nil || target == c.unknownSymbol {
			return true
		}
	}
	if target.Flags&(ast.SymbolFlagsFunction|ast.SymbolFlagsClass|ast.SymbolFlagsEnum|ast.SymbolFlagsValueModule|ast.SymbolFlagsMethod) != 0 &&
		target.Flags&ast.SymbolFlagsVariable == 0 {
		return true
	}
	if len(target.Declarations) == 0 {
		return false
	}
	inLibrary := true
	for _, decl := range target.Declarations {
		if ast.IsSharedStructDeclaration(decl) {
			return true
		}
		if file := ast.GetSourceFileOfNode(decl); file == nil || !c.program.IsSourceFileDefaultLibrary(file.Path()) {
			inLibrary = false
		}
	}
	if inLibrary {
		return true
	}
	if a.isDeclaredShared(target) {
		return true
	}
	return a.isPrimitiveOrSharedStructType(c.getTypeOfSymbol(target))
}

// Reports whether a variable, parameter or property is declared with a `shared` type, or is a
// `const` initialized with a shared value.
func (a *ownAnalyzer) isDeclaredShared(sym *ast.Symbol) bool {
	decl := sym.ValueDeclaration
	if decl == nil {
		return false
	}
	switch decl.Kind {
	case ast.KindParameter:
		return a.isSharedTypeNode(a.parameterTypeNode(decl))
	case ast.KindPropertySignature, ast.KindPropertyDeclaration:
		return a.isSharedTypeNode(decl.Type())
	case ast.KindVariableDeclaration:
		if t := decl.Type(); t != nil {
			return a.isSharedTypeNode(t)
		}
		if init := decl.Initializer(); init != nil && ast.GetCombinedNodeFlags(decl)&ast.NodeFlagsConst != 0 {
			return a.isStaticallyShared(init)
		}
	}
	return false
}

// Reports whether an expression's value is shared on every path, using declarations only.
func (a *ownAnalyzer) isStaticallyShared(expr *ast.Node) bool {
	expr = ast.SkipOuterExpressions(expr, ast.OEKAll)
	switch expr.Kind {
	case ast.KindNumericLiteral, ast.KindBigIntLiteral, ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral,
		ast.KindTemplateExpression, ast.KindTrueKeyword, ast.KindFalseKeyword, ast.KindNullKeyword:
		return true
	case ast.KindIdentifier:
		sym := a.symbolOfIdentifier(expr)
		return sym != nil && a.isSharedSymbol(sym)
	case ast.KindPropertyAccessExpression:
		if a.isStaticallyShared(expr.Expression()) {
			return true
		}
		prop := a.c.getSymbolAtLocation(expr.Name(), true /*ignoreErrors*/)
		return prop != nil && a.isDeclaredShared(prop)
	case ast.KindCallExpression, ast.KindNewExpression:
		if ast.IsNewExpression(expr) && a.isSharedStructReference(expr.Expression()) {
			return true
		}
		sig := a.c.getResolvedSignature(expr, nil, CheckModeNormal)
		return sig != nil && a.returnKind(sig.declaration) == ownReturnsShared
	}
	return false
}

// Reports whether an expression names a shared struct (whose value constructs shared instances).
func (a *ownAnalyzer) isSharedStructReference(expr *ast.Node) bool {
	expr = ast.SkipOuterExpressions(expr, ast.OEKAll)
	var sym *ast.Symbol
	switch {
	case ast.IsIdentifier(expr):
		sym = a.symbolOfIdentifier(expr)
	case ast.IsPropertyAccessExpression(expr):
		sym = a.c.getSymbolAtLocation(expr.Name(), true /*ignoreErrors*/)
	}
	if sym != nil && sym.Flags&ast.SymbolFlagsAlias != 0 {
		sym = a.c.resolveAlias(sym)
	}
	return sym != nil && core.Some(sym.Declarations, ast.IsSharedStructDeclaration)
}

// Reports whether the elements of an array-typed expression are declared shared, as in
// `xs: (shared T)[]`.
func (a *ownAnalyzer) isElementDeclaredShared(expr *ast.Node) bool {
	t := a.resolveTypeNode(a.declaredTypeNodeOf(expr))
	if t == nil {
		return false
	}
	switch t.Kind {
	case ast.KindArrayType:
		return a.isSharedTypeNode(t.AsArrayTypeNode().ElementType)
	case ast.KindTypeReference:
		if args := t.TypeArguments(); len(args) == 1 {
			if name := t.AsTypeReferenceNode().TypeName; ast.IsIdentifier(name) && (name.Text() == "Array" || name.Text() == "ReadonlyArray") {
				return a.isSharedTypeNode(args[0])
			}
		}
	}
	return false
}

// The type node an expression was declared with, if it has one.
func (a *ownAnalyzer) declaredTypeNodeOf(expr *ast.Node) *ast.Node {
	expr = ast.SkipOuterExpressions(expr, ast.OEKAll)
	switch expr.Kind {
	case ast.KindIdentifier:
		if sym := a.symbolOfIdentifier(expr); sym != nil && sym.ValueDeclaration != nil {
			decl := sym.ValueDeclaration
			switch decl.Kind {
			case ast.KindParameter:
				return a.parameterTypeNode(decl)
			case ast.KindVariableDeclaration, ast.KindPropertyDeclaration, ast.KindPropertySignature:
				return decl.Type()
			}
		}
	case ast.KindPropertyAccessExpression:
		if prop := a.c.getSymbolAtLocation(expr.Name(), true /*ignoreErrors*/); prop != nil && prop.ValueDeclaration != nil {
			switch prop.ValueDeclaration.Kind {
			case ast.KindPropertyDeclaration, ast.KindPropertySignature:
				return prop.ValueDeclaration.Type()
			}
		}
	case ast.KindCallExpression, ast.KindNewExpression:
		if sig := a.c.getResolvedSignature(expr, nil, CheckModeNormal); sig != nil && sig.declaration != nil && sig.declaration.FunctionLikeData() != nil {
			return sig.declaration.Type()
		}
	case ast.KindElementAccessExpression:
		if t := a.resolveTypeNode(a.declaredTypeNodeOf(expr.Expression())); t != nil && ast.IsArrayTypeNode(t) {
			return t.AsArrayTypeNode().ElementType
		}
	}
	return nil
}

// The element value of iterating over a value.
func (a *ownAnalyzer) elementValue(iterable *ast.Node, v ownValue) ownValue {
	if v.kind != ownInRegion || a.isElementDeclaredShared(iterable) {
		return ownSharedValue
	}
	return v
}

// Skips parentheses and `readonly`, and follows type aliases (a few levels deep).
func (a *ownAnalyzer) resolveTypeNode(t *ast.Node) *ast.Node {
	for range 5 {
		if t == nil {
			return nil
		}
		for ast.IsParenthesizedTypeNode(t) || ast.IsTypeOperatorNode(t) && t.AsTypeOperatorNode().Operator == ast.KindReadonlyKeyword {
			t = t.Type()
		}
		if !ast.IsTypeReferenceNode(t) {
			return t
		}
		sym := a.c.getSymbolAtLocation(t.AsTypeReferenceNode().TypeName, true /*ignoreErrors*/)
		if sym == nil || sym.Flags&ast.SymbolFlagsTypeAlias == 0 {
			return t
		}
		decl := core.Find(sym.Declarations, ast.IsTypeAliasDeclaration)
		if decl == nil {
			return t
		}
		t = decl.Type()
	}
	return t
}

// `shared T`, possibly behind parentheses, `readonly` or an alias, or a union of such types with
// null and undefined.
func (a *ownAnalyzer) isSharedTypeNode(t *ast.Node) bool {
	t = a.resolveTypeNode(t)
	if t == nil {
		return false
	}
	switch t.Kind {
	case ast.KindSharedType:
		return true
	case ast.KindUnionType:
		sawShared := false
		for _, member := range t.AsUnionTypeNode().Types.Nodes {
			m := a.resolveTypeNode(member)
			switch {
			case m.Kind == ast.KindUndefinedKeyword || m.Kind == ast.KindNullKeyword ||
				ast.IsLiteralTypeNode(m) && m.AsLiteralTypeNode().Literal.Kind == ast.KindNullKeyword:
			case a.isSharedTypeNode(m):
				sawShared = true
			default:
				return false
			}
		}
		return sawShared
	}
	return false
}

func (a *ownAnalyzer) isSendingTypeNode(t *ast.Node) bool {
	t = a.resolveTypeNode(t)
	return t != nil && t.Kind == ast.KindSendingType
}

// The declared type of a parameter, or the type of the corresponding parameter of the contextual
// signature for an unannotated parameter of a function expression.
func (a *ownAnalyzer) parameterTypeNode(p *ast.Node) *ast.Node {
	if t := p.Type(); t != nil {
		return t
	}
	fn := p.Parent
	if fn == nil || !(ast.IsFunctionExpressionOrArrowFunction(fn) || ast.IsObjectLiteralMethod(fn)) {
		return nil
	}
	sig := a.c.getContextualSignatureForFunctionLikeDeclaration(fn)
	if sig == nil {
		return nil
	}
	index := -1
	for i, param := range ownSignatureParameters(fn) {
		if param == p {
			index = i
		}
	}
	if param := ownParameterAt(ownSignatureParameters(sig.declaration), index); param != nil {
		return param.Type()
	}
	return nil
}

// The parameters of a signature declaration, without a `this` parameter.
func ownSignatureParameters(decl *ast.Node) []*ast.Node {
	if decl == nil || decl.FunctionLikeData() == nil {
		return nil
	}
	params := decl.Parameters()
	if len(params) > 0 && ast.IsThisParameter(params[0]) {
		params = params[1:]
	}
	return params
}

// The parameter an argument at the given index is passed to.
func ownParameterAt(params []*ast.Node, index int) *ast.Node {
	if index < 0 || len(params) == 0 {
		return nil
	}
	if index < len(params) {
		return params[index]
	}
	if last := params[len(params)-1]; last.AsParameterDeclaration().DotDotDotToken != nil {
		return last
	}
	return nil
}

func (a *ownAnalyzer) returnKind(decl *ast.Node) ownReturnKind {
	if decl == nil || decl.FunctionLikeData() == nil {
		return ownReturnsOrdinary
	}
	t := decl.Type()
	switch {
	case t == nil:
		return ownReturnsOrdinary
	case a.isSendingTypeNode(t):
		return ownReturnsSending
	case a.isSharedTypeNode(t):
		return ownReturnsShared
	}
	return ownReturnsOrdinary
}

func (a *ownAnalyzer) returnsSending(fn *ast.Node) bool {
	if t := fn.Type(); t != nil {
		return a.isSendingTypeNode(t)
	}
	if ast.IsFunctionExpressionOrArrowFunction(fn) || ast.IsObjectLiteralMethod(fn) {
		if sig := a.c.getContextualSignatureForFunctionLikeDeclaration(fn); sig != nil {
			return a.returnKind(sig.declaration) == ownReturnsSending
		}
	}
	return false
}

// A share-like signature `<T>(x: sending T) => shared T` turns its argument into a shared value; the
// argument is then reported as consumed rather than sent.
func (a *ownAnalyzer) isConsumingSignature(decl *ast.Node) bool {
	if a.returnKind(decl) != ownReturnsShared {
		return false
	}
	result := ownTypeParameterName(decl, a.resolveTypeNode(decl.Type()).Type())
	if result == "" {
		return false
	}
	for _, p := range ownSignatureParameters(decl) {
		if t := p.Type(); t != nil && a.isSendingTypeNode(t) && ownTypeParameterName(decl, a.resolveTypeNode(t).Type()) == result {
			return true
		}
	}
	return false
}

// The name of a type parameter of decl that t refers to, if it is one.
func ownTypeParameterName(decl *ast.Node, t *ast.Node) string {
	for t != nil && ast.IsParenthesizedTypeNode(t) {
		t = t.Type()
	}
	if t == nil || !ast.IsTypeReferenceNode(t) || len(t.TypeArguments()) != 0 || !ast.IsIdentifier(t.AsTypeReferenceNode().TypeName) {
		return ""
	}
	name := t.AsTypeReferenceNode().TypeName.Text()
	for _, tp := range decl.TypeParameters() {
		if tp.Name().Text() == name {
			return name
		}
	}
	return ""
}

// Reports whether a signature is Array.prototype.map or flatMap (or the ReadonlyArray versions) from
// the default library.
func (a *ownAnalyzer) isLibArrayMethod(decl *ast.Node) bool {
	if decl == nil || decl.Parent == nil || !ast.IsInterfaceDeclaration(decl.Parent) {
		return false
	}
	switch decl.Parent.Name().Text() {
	case "Array", "ReadonlyArray":
	default:
		return false
	}
	file := ast.GetSourceFileOfNode(decl)
	return file != nil && a.c.program.IsSourceFileDefaultLibrary(file.Path())
}

func (a *ownAnalyzer) isArrayExpression(expr *ast.Node) bool {
	t := a.c.getTypeOfExpression(expr)
	return t != nil && (a.c.isArrayOrTupleType(t) || a.c.isArrayType(t))
}

// Reports whether an expression is a shared value in the sense of the extension, which is immutable:
// it must be declared shared (a variable, parameter or member declared `shared T`, a call returning
// `shared T`, or something read out of such a value). A shared value that is shared for another
// reason (a primitive, code, a library value, or an uninitialized variable) is not reported, and
// shared struct instances are mutable cells.
func (a *ownAnalyzer) isImmutableShared(expr *ast.Node) bool {
	return !a.isPrimitiveOrSharedStructType(a.c.getTypeOfExpression(expr)) && a.isDeclaredSharedExpression(expr)
}

func (a *ownAnalyzer) isDeclaredSharedExpression(expr *ast.Node) bool {
	expr = ast.SkipOuterExpressions(expr, ast.OEKAll)
	switch expr.Kind {
	case ast.KindIdentifier:
		sym := a.symbolOfIdentifier(expr)
		if sym == nil {
			return false
		}
		if sym.Flags&ast.SymbolFlagsAlias != 0 {
			sym = a.c.resolveAlias(sym)
		}
		if a.isDeclaredShared(sym) {
			return true
		}
		// `for (const x of xs)` where xs is shared or has shared elements.
		if decl := sym.ValueDeclaration; decl != nil && ast.IsVariableDeclaration(decl) && decl.Parent != nil &&
			ast.IsVariableDeclarationList(decl.Parent) && decl.Parent.Parent != nil && ast.IsForOfStatement(decl.Parent.Parent) {
			iterable := decl.Parent.Parent.Expression()
			return a.isDeclaredSharedExpression(iterable) || a.isElementDeclaredShared(iterable)
		}
		return false
	case ast.KindPropertyAccessExpression:
		if a.isDeclaredSharedExpression(expr.Expression()) {
			return true
		}
		prop := a.c.getSymbolAtLocation(expr.Name(), true /*ignoreErrors*/)
		return prop != nil && a.isDeclaredShared(prop)
	case ast.KindElementAccessExpression:
		return a.isDeclaredSharedExpression(expr.Expression()) || a.isElementDeclaredShared(expr.Expression())
	case ast.KindCallExpression, ast.KindNewExpression:
		sig := a.c.getResolvedSignature(expr, nil, CheckModeNormal)
		return sig != nil && a.returnKind(sig.declaration) == ownReturnsShared
	}
	return false
}

func (a *ownAnalyzer) isPrimitiveOrSharedStructType(t *Type) bool {
	if t == nil {
		return false
	}
	if t.flags&TypeFlagsUnion != 0 {
		for _, member := range t.Types() {
			if !a.isPrimitiveOrSharedStructType(member) {
				return false
			}
		}
		return true
	}
	if t.flags&(TypeFlagsPrimitive|TypeFlagsNever) != 0 || a.c.getOwnSharedArrayElementType(t) != nil {
		// SharedArrays (with `--ownRuntime`) are cells in the shared heap, like shared structs.
		return true
	}
	if t.symbol != nil {
		for _, decl := range t.symbol.Declarations {
			if ast.IsSharedStructDeclaration(decl) {
				return true
			}
		}
	}
	return false
}

// Closure captures

func (a *ownAnalyzer) capturesOf(fn *ast.Node) *ownCaptures {
	if captures := a.captures[fn]; captures != nil {
		return captures
	}
	captures := &ownCaptures{}
	a.captures[fn] = captures
	seen := map[*ast.Symbol]bool{}
	var visit func(node *ast.Node, inArrow bool) bool
	visit = func(node *ast.Node, inArrow bool) bool {
		if ast.IsTypeNode(node) {
			return false
		}
		switch {
		case ast.IsIdentifier(node):
			if !isOwnValueReference(node) {
				return false
			}
			sym := a.symbolOfIdentifier(node)
			if sym == nil || seen[sym] || a.isSharedSymbol(sym) {
				return false
			}
			decl := sym.ValueDeclaration
			if decl == nil && len(sym.Declarations) > 0 {
				decl = sym.Declarations[0]
			}
			if decl != nil && !ownIsWithin(decl, fn) {
				seen[sym] = true
				captures.symbols = append(captures.symbols, sym)
				captures.nodes = append(captures.nodes, node)
			}
			return false
		case node.Kind == ast.KindThisKeyword || node.Kind == ast.KindSuperKeyword:
			if inArrow {
				captures.usesThis = true
			}
			return false
		}
		childInArrow := inArrow
		if node != fn && isOwnFunctionBody(node) {
			childInArrow = inArrow && ast.IsArrowFunction(node)
		}
		node.ForEachChild(func(child *ast.Node) bool { return visit(child, childInArrow) })
		return false
	}
	fn.ForEachChild(func(child *ast.Node) bool { return visit(child, ast.IsArrowFunction(fn)) })
	return captures
}

func ownIsWithin(node *ast.Node, ancestor *ast.Node) bool {
	for n := node; n != nil; n = n.Parent {
		if n == ancestor {
			return true
		}
	}
	return false
}

// Reports whether an identifier refers to a variable (rather than naming a property, a declaration
// or a label).
func isOwnValueReference(id *ast.Node) bool {
	parent := id.Parent
	if parent == nil {
		return false
	}
	switch parent.Kind {
	case ast.KindPropertyAccessExpression, ast.KindPropertyAssignment, ast.KindPropertyDeclaration, ast.KindPropertySignature,
		ast.KindMethodDeclaration, ast.KindMethodSignature, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindEnumMember,
		ast.KindVariableDeclaration, ast.KindParameter, ast.KindFunctionDeclaration, ast.KindFunctionExpression,
		ast.KindClassDeclaration, ast.KindClassExpression, ast.KindTypeParameter, ast.KindImportSpecifier,
		ast.KindExportSpecifier, ast.KindImportClause, ast.KindNamespaceImport, ast.KindImportEqualsDeclaration,
		ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration, ast.KindEnumDeclaration, ast.KindModuleDeclaration,
		ast.KindSharedStructDeclaration, ast.KindJsxAttribute, ast.KindNamespaceExport:
		return parent.Name() != id
	case ast.KindBindingElement:
		return parent.Name() != id && parent.PropertyName() != id
	case ast.KindLabeledStatement, ast.KindBreakStatement, ast.KindContinueStatement, ast.KindMetaProperty, ast.KindQualifiedName:
		return false
	}
	return true
}
