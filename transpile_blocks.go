package danmuji

import (
	"fmt"
	gotreesitter "github.com/odvcencio/gotreesitter"
	"strings"
)

// ---------------------------------------------------------------------------
// benchmark_block → func BenchmarkXxx(b *testing.B)
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitBenchmark(n *gotreesitter.Node) string {
	// Save and set testVar for benchmark context
	oldTestVar := t.testVar
	t.testVar = "b"
	defer func() { t.testVar = oldTestVar }()

	// Extract name
	nameNode := t.childByField(n, "name")
	name := "Benchmark"
	if nameNode != nil {
		name = sanitizeTestName(t.text(nameNode))
	}

	// Walk the body block's children for setup_block, measure_block,
	// parallel_measure_block, report_directive, then_block
	bodyNode := t.childByField(n, "body")
	if bodyNode == nil {
		return t.text(n)
	}

	var setupBlocks []*gotreesitter.Node
	var measureBlocks []*gotreesitter.Node
	var parallelMeasureBlocks []*gotreesitter.Node
	hasReportAllocs := false
	var thenBlocks []*gotreesitter.Node

	t.walkChildren(bodyNode, func(child *gotreesitter.Node) {
		switch t.nodeType(child) {
		case "setup_block":
			setupBlocks = append(setupBlocks, child)
		case "measure_block":
			measureBlocks = append(measureBlocks, child)
		case "parallel_measure_block":
			parallelMeasureBlocks = append(parallelMeasureBlocks, child)
		case "report_directive":
			hasReportAllocs = true
		case "then_block":
			thenBlocks = append(thenBlocks, child)
		}
	})

	var b strings.Builder

	// Emit any collected mock declarations before the function
	if len(t.mockDecls) > 0 {
		for _, md := range t.mockDecls {
			b.WriteString(md)
		}
		t.mockDecls = nil
	}

	// Function signature
	fmt.Fprintf(&b, "func Benchmark%s(b *testing.B) {\n", name)
	b.WriteString(t.lineDirective(n))

	// Emit setup code
	for _, sb := range setupBlocks {
		b.WriteString(t.emitBlockContents(sb))
	}

	// Report allocs
	if hasReportAllocs {
		fmt.Fprintf(&b, "\tb.ReportAllocs()\n")
	}

	// Reset timer after setup
	if len(setupBlocks) > 0 || hasReportAllocs {
		fmt.Fprintf(&b, "\tb.ResetTimer()\n")
	}

	// Emit parallel measure blocks
	if len(parallelMeasureBlocks) > 0 {
		for _, pmb := range parallelMeasureBlocks {
			fmt.Fprintf(&b, "\tb.RunParallel(func(pb *testing.PB) {\n")
			fmt.Fprintf(&b, "\t\tfor pb.Next() {\n")
			b.WriteString(t.emitBlockContentsIndented(pmb, "\t\t\t"))
			fmt.Fprintf(&b, "\t\t}\n")
			fmt.Fprintf(&b, "\t})\n")
		}
	}

	// Emit measure blocks
	if len(measureBlocks) > 0 {
		for _, mb := range measureBlocks {
			fmt.Fprintf(&b, "\tfor i := 0; i < b.N; i++ {\n")
			b.WriteString(t.emitBlockContentsIndented(mb, "\t\t"))
			fmt.Fprintf(&b, "\t}\n")
		}
	}

	// Emit then blocks
	for _, tb := range thenBlocks {
		b.WriteString("\t")
		b.WriteString(t.emitBDDBlock(tb, "then"))
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "}\n")

	return b.String()
}

// emitBlockContents extracts the inner content of a *_block node (which has
// a Sym("block") child) and emits it with one tab indent.
func (t *dmjTranspiler) emitBlockContents(n *gotreesitter.Node) string {
	// Find the block child
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if t.nodeType(c) == "block" {
			return t.emitBlockInner(c, "\t")
		}
	}
	return ""
}

// emitBlockContentsIndented extracts and emits block inner content with a given indent.
func (t *dmjTranspiler) emitBlockContentsIndented(n *gotreesitter.Node, indent string) string {
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if t.nodeType(c) == "block" {
			return t.emitBlockInner(c, indent)
		}
	}
	return ""
}

// emitBlockInner emits the inner contents of a block node (statement_list children),
// with each statement at the given indent level.
func (t *dmjTranspiler) emitBlockInner(blockNode *gotreesitter.Node, indent string) string {
	return t.emitBlockInnerWithHooks(blockNode, indent, nil, nil)
}

// blockHasNestedBDD reports whether a block node's statement_list directly
// contains at least one given/when/then block. It is used to decide whether
// a hook inherited from an ancestor must keep being threaded down (the
// block is a container for further BDD structure) or must be materialized
// here (the block is a leaf: hooks stop propagating once there is nowhere
// further to distribute them).
func (t *dmjTranspiler) blockHasNestedBDD(blockNode *gotreesitter.Node) bool {
	for i := 0; i < int(blockNode.NamedChildCount()); i++ {
		c := blockNode.NamedChild(i)
		if t.nodeType(c) != "statement_list" {
			continue
		}
		for j := 0; j < int(c.NamedChildCount()); j++ {
			switch t.nodeType(c.NamedChild(j)) {
			case "given_block", "when_block", "then_block":
				return true
			}
		}
	}
	return false
}

// emitBlockInnerWithHooks is emitBlockInner generalized to accept hooks
// inherited from an enclosing given/when/then. Inherited hooks are merged
// with any hooks declared locally in this block's own statement_list (outer
// hooks first, so they run before inner ones) before being handed to any
// nested given/when/then statement — this is what lets a `before each`
// declared above a `given` keep reaching every leaf `then` underneath it,
// instead of firing once for the whole group.
func (t *dmjTranspiler) emitBlockInnerWithHooks(blockNode *gotreesitter.Node, indent string, inheritedBefore, inheritedAfter []*gotreesitter.Node) string {
	var b strings.Builder
	// Find statement_list inside the block
	for i := 0; i < int(blockNode.NamedChildCount()); i++ {
		c := blockNode.NamedChild(i)
		if t.nodeType(c) == "statement_list" {
			beforeEachHooks := append([]*gotreesitter.Node{}, inheritedBefore...)
			afterEachHooks := append([]*gotreesitter.Node{}, inheritedAfter...)
			for j := 0; j < int(c.NamedChildCount()); j++ {
				stmt := c.NamedChild(j)
				if t.nodeType(stmt) == "lifecycle_hook" {
					hookText := strings.TrimSpace(t.text(stmt))
					switch {
					case strings.HasPrefix(hookText, "before each"):
						beforeEachHooks = append(beforeEachHooks, stmt)
						continue
					case strings.HasPrefix(hookText, "after each"):
						afterEachHooks = append(afterEachHooks, stmt)
						continue
					}
				}

				if len(beforeEachHooks) > 0 || len(afterEachHooks) > 0 {
					if hooked, ok := t.emitStatementWithHooks(stmt, beforeEachHooks, afterEachHooks); ok {
						fmt.Fprintf(&b, "%s%s\n", indent, hooked)
						continue
					}
				}

				fmt.Fprintf(&b, "%s%s\n", indent, t.emit(stmt))
			}
			return b.String()
		}
	}
	return ""
}

func (t *dmjTranspiler) emitStatementWithHooks(stmt *gotreesitter.Node, beforeEachHooks, afterEachHooks []*gotreesitter.Node) (string, bool) {
	switch t.nodeType(stmt) {
	case "given_block":
		return t.emitBDDBlockWithHooks(stmt, "given", beforeEachHooks, afterEachHooks), true
	case "when_block":
		return t.emitBDDBlockWithHooks(stmt, "when", beforeEachHooks, afterEachHooks), true
	case "then_block":
		return t.emitBDDBlockWithHooks(stmt, "then", beforeEachHooks, afterEachHooks), true
	case "each_do_block", "matrix_block", "each_row_block":
		oldBefore := t.beforeEachHookContext
		oldAfter := t.afterEachHookContext
		t.beforeEachHookContext = beforeEachHooks
		t.afterEachHookContext = afterEachHooks
		defer func() {
			t.beforeEachHookContext = oldBefore
			t.afterEachHookContext = oldAfter
		}()
		return t.emit(stmt), true
	default:
		return "", false
	}
}

// ---------------------------------------------------------------------------
// given/when/then → t.Run("description", func(t *testing.T) { ... })
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitBDDBlock(n *gotreesitter.Node, keyword string) string {
	desc := t.childByField(n, "description")
	descText := `"` + keyword + `"`
	label := keyword
	if desc != nil {
		descText = t.text(desc)
		label = strings.Trim(descText, "\"'`")
	}
	t.pushContext(fmt.Sprintf("%s %s", keyword, label))

	defer t.popContext()

	var b strings.Builder
	b.WriteString(t.lineDirective(n))

	// Find and emit the block
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if t.nodeType(c) == "block" {
			if t.blockHasDirectLifecycleHook(c) {
				// This block declares its own before/after each with no
				// hooks inherited from an ancestor. Route through the
				// hook-aware body writer (with no inherited hooks) so the
				// locally declared hook still reaches every nested
				// given/when/then instead of running once via the
				// source-preserving, hook-blind emitTestBody/emitDefault
				// path used below.
				fmt.Fprintf(&b, "%s.Run(%s, func(%s *testing.T) {\n", t.testVar, descText, t.testVar)
				b.WriteString(t.emitSubtestBodyWithHooks(c, "\t", nil, nil))
				b.WriteString("})")
			} else {
				fmt.Fprintf(&b, "%s.Run(%s, func(%s *testing.T) ", t.testVar, descText, t.testVar)
				b.WriteString(t.emitTestBody(c))
				b.WriteString(")")
			}
			break
		}
	}

	return b.String()
}

// blockHasDirectLifecycleHook reports whether a block node's statement_list
// directly declares a `before each`/`after each` hook. emitBDDBlock uses
// this to decide whether it needs hook-aware body processing at all: the
// common case (no hook declared in this scope) keeps using the
// source-preserving emitTestBody/emitDefault path unchanged.
func (t *dmjTranspiler) blockHasDirectLifecycleHook(blockNode *gotreesitter.Node) bool {
	for i := 0; i < int(blockNode.NamedChildCount()); i++ {
		c := blockNode.NamedChild(i)
		if t.nodeType(c) != "statement_list" {
			continue
		}
		for j := 0; j < int(c.NamedChildCount()); j++ {
			stmt := c.NamedChild(j)
			if t.nodeType(stmt) != "lifecycle_hook" {
				continue
			}
			hookText := strings.TrimSpace(t.text(stmt))
			if strings.HasPrefix(hookText, "before each") || strings.HasPrefix(hookText, "after each") {
				return true
			}
		}
	}
	return false
}

func (t *dmjTranspiler) emitBDDBlockWithHooks(n *gotreesitter.Node, keyword string, beforeEachHooks, afterEachHooks []*gotreesitter.Node) string {
	desc := t.childByField(n, "description")
	descText := `"` + keyword + `"`
	label := keyword
	if desc != nil {
		descText = t.text(desc)
		label = strings.Trim(descText, "\"'`")
	}
	t.pushContext(fmt.Sprintf("%s %s", keyword, label))
	defer t.popContext()

	var b strings.Builder
	b.WriteString(t.lineDirective(n))
	fmt.Fprintf(&b, "%s.Run(%s, func(%s *testing.T) {\n", t.testVar, descText, t.testVar)

	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if t.nodeType(c) == "block" {
			b.WriteString(t.emitSubtestBodyWithHooks(c, "\t", beforeEachHooks, afterEachHooks))
			break
		}
	}

	b.WriteString("})")
	return b.String()
}

func (t *dmjTranspiler) emitSubtestBodyWithHooks(bodyNode *gotreesitter.Node, indent string, beforeEachHooks, afterEachHooks []*gotreesitter.Node) string {
	var b strings.Builder

	// If this block directly contains further given/when/then blocks, it is
	// a container, not a leaf test case: materializing the hook body here
	// would run it once for the whole container instead of once per leaf
	// then. Keep threading the hooks down so each nested given/when/then
	// gets its own copy (see emitBlockInnerWithHooks).
	if t.blockHasNestedBDD(bodyNode) {
		b.WriteString(t.emitBlockInnerWithHooks(bodyNode, indent, beforeEachHooks, afterEachHooks))
		return b.String()
	}

	for _, hook := range beforeEachHooks {
		b.WriteString(t.emitBlockContentsIndented(hook, indent))
	}
	for _, hook := range afterEachHooks {
		fmt.Fprintf(&b, "%s%s.Cleanup(func() {\n", indent, t.testVar)
		b.WriteString(t.emitBlockContentsIndented(hook, indent+"\t"))
		fmt.Fprintf(&b, "%s})\n", indent)
	}
	b.WriteString(t.emitBlockInner(bodyNode, indent))
	return b.String()
}
