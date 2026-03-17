package danmuji

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// Package-level cached language to avoid regenerating on every TranspileDanmuji call.
var (
	danmujiLangOnce   sync.Once
	danmujiLangCached *gotreesitter.Language
	danmujiLangErr    error
)

func getDanmujiLanguage() (*gotreesitter.Language, error) {
	danmujiLangOnce.Do(func() {
		danmujiLangCached, danmujiLangErr = GenerateLanguage(DanmujiGrammar())
	})
	return danmujiLangCached, danmujiLangErr
}

// TranspileDanmuji parses a .dmj source file and emits valid Go test code.
func TranspileDanmuji(source []byte) (string, error) {
	lang, err := getDanmujiLanguage()
	if err != nil {
		return "", fmt.Errorf("generate danmuji language: %w", err)
	}

	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse(source)
	if err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}

	root := tree.RootNode()
	if root.HasError() {
		return "", fmt.Errorf("parse errors:\n%s", root.SExpr(lang))
	}

	tr := &dmjTranspiler{src: source, lang: lang, testVar: "t"}
	// First pass: collect package-level declarations (mocks)
	tr.collectTopLevel(root)
	// Second pass: emit the code
	output := tr.emit(root)

	// Inject all collected imports
	output = tr.injectImports(output)

	return output, nil
}

// ---------------------------------------------------------------------------
// Transpiler state
// ---------------------------------------------------------------------------

type dmjTranspiler struct {
	src     []byte
	lang    *gotreesitter.Language
	testVar string // "t" for tests, "b" for benchmarks
	// Package-level mock declarations collected during first pass.
	// These are emitted before the test function that contained them.
	mockDecls []string
	// Set of mock_declaration nodes (by start byte) that have been collected
	// so emit() can skip emitting them inline.
	collectedMockStarts map[uint32]bool
	// Collected imports (deduped by package path).
	neededImports map[string]bool
	// Whether we are inside an exec block (for special identifier translation).
	inExecBlock bool
	// Whether the Clock interface/fakeClock struct has been collected for package-level emission.
	fakeClockTypeCollected bool
	// Package-level type definitions for fake clock (emitted before test function).
	fakeClockTypeDecl string
}

// addImport records a package path that should be injected into the import block.
func (t *dmjTranspiler) addImport(pkg string) {
	if t.neededImports == nil {
		t.neededImports = make(map[string]bool)
	}
	t.neededImports[pkg] = true
}

func (t *dmjTranspiler) text(n *gotreesitter.Node) string {
	return string(t.src[n.StartByte():n.EndByte()])
}

func (t *dmjTranspiler) nodeType(n *gotreesitter.Node) string {
	return n.Type(t.lang)
}

func (t *dmjTranspiler) childByField(n *gotreesitter.Node, field string) *gotreesitter.Node {
	return n.ChildByFieldName(field, t.lang)
}

// ---------------------------------------------------------------------------
// First pass: collect mock declarations so they can be emitted at package level
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) collectTopLevel(n *gotreesitter.Node) {
	if t.collectedMockStarts == nil {
		t.collectedMockStarts = make(map[uint32]bool)
	}
	nt := t.nodeType(n)
	if nt == "mock_declaration" {
		t.mockDecls = append(t.mockDecls, t.buildMockDecl(n))
		t.collectedMockStarts[n.StartByte()] = true
		return
	}
	if nt == "fake_declaration" {
		t.mockDecls = append(t.mockDecls, t.buildFakeDecl(n))
		t.collectedMockStarts[n.StartByte()] = true
		return
	}
	if nt == "spy_declaration" {
		decl := t.buildSpyDecl(n)
		if decl != "" {
			t.mockDecls = append(t.mockDecls, decl)
			t.collectedMockStarts[n.StartByte()] = true
		}
		return
	}
	if nt == "fake_clock_directive" && !t.fakeClockTypeCollected {
		// Pre-collect the Clock interface and fakeClock struct for package-level emission.
		t.fakeClockTypeCollected = true
		t.addImport("time")
		t.addImport("sync")
		t.fakeClockTypeDecl = `// Clock interface for time abstraction
type Clock interface {
	Now() time.Time
	Since(t time.Time) time.Duration
	Until(t time.Time) time.Duration
	After(d time.Duration) <-chan time.Time
	NewTicker(d time.Duration) *time.Ticker
}

type fakeClock struct {
	mu      sync.Mutex
	current time.Time
	loc     *time.Location
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loc != nil {
		return c.current.In(c.loc)
	}
	return c.current
}

func (c *fakeClock) Since(t time.Time) time.Duration { return c.Now().Sub(t) }
func (c *fakeClock) Until(t time.Time) time.Duration { return t.Sub(c.Now()) }
func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- c.Now().Add(d)
	return ch
}
func (c *fakeClock) NewTicker(d time.Duration) *time.Ticker { return time.NewTicker(d) }

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.current.Add(d)
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = t
}

func (c *fakeClock) SetLocation(loc *time.Location) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loc = loc
}

`
		t.mockDecls = append(t.mockDecls, t.fakeClockTypeDecl)
		// Don't return — continue recursion to find nested mocks
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		t.collectTopLevel(n.Child(i))
	}
}

// ---------------------------------------------------------------------------
// Main emit dispatcher
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emit(n *gotreesitter.Node) string {
	switch t.nodeType(n) {
	case "test_block":
		return t.emitTestBlock(n)
	case "given_block":
		return t.emitBDDBlock(n, "given")
	case "when_block":
		return t.emitBDDBlock(n, "when")
	case "then_block":
		return t.emitBDDBlock(n, "then")
	case "expect_statement":
		return t.emitExpect(n)
	case "reject_statement":
		return t.emitReject(n)
	case "mock_declaration":
		// Already collected at package level — emit a blank (whitespace preserved
		// by emitDefault's gap logic on the parent).
		if t.collectedMockStarts[n.StartByte()] {
			return ""
		}
		return t.text(n)
	case "lifecycle_hook":
		return t.emitLifecycleHook(n)
	case "verify_statement":
		return t.emitVerify(n)
	case "needs_block":
		return t.emitNeedsBlock(n)
	case "load_block":
		return t.emitLoad(n)
	case "load_config":
		return "" // handled by emitLoad
	case "target_block":
		return "" // handled by emitLoad
	case "benchmark_block":
		return t.emitBenchmark(n)
	case "setup_block":
		return "" // handled by emitBenchmark
	case "measure_block":
		return "" // handled by emitBenchmark
	case "parallel_measure_block":
		return "" // handled by emitBenchmark
	case "report_directive":
		return "" // handled by emitBenchmark
	case "exec_block":
		return t.emitExec(n)
	case "run_command":
		return "" // handled by emitExec
	case "profile_block":
		return t.emitProfile(n)
	case "fake_declaration":
		// Already collected at package level — emit a blank.
		if t.collectedMockStarts[n.StartByte()] {
			return ""
		}
		return t.text(n)
	case "spy_declaration":
		// Already collected at package level — emit a blank.
		if t.collectedMockStarts[n.StartByte()] {
			return ""
		}
		return t.emitSpy(n)
	case "snapshot_block":
		return t.emitSnapshot(n)
	case "each_do_block":
		return t.emitEachDo(n)
	case "matrix_block":
		return t.emitMatrix(n)
	case "defaults_block":
		return "" // handled by emitEachDo
	case "scenario_entry":
		return "" // handled by emitEachDo
	case "scenario_field":
		return "" // handled by emitEachDo / emitMatrix
	case "matrix_field":
		return "" // handled by emitMatrix
	case "table_declaration":
		return t.emitTable(n)
	case "each_row_block":
		return t.emitEachRow(n)
	case "no_leaks_directive":
		return t.emitNoLeaks(n)
	case "fake_clock_directive":
		return t.emitFakeClock(n)
	default:
		return t.emitDefault(n)
	}
}

// emitDefault preserves whitespace by walking children and copying inter-child gaps.
func (t *dmjTranspiler) emitDefault(n *gotreesitter.Node) string {
	cc := int(n.ChildCount())
	if cc == 0 {
		return t.text(n)
	}
	var b strings.Builder
	prev := n.StartByte()
	for i := 0; i < cc; i++ {
		c := n.Child(i)
		if c.StartByte() > prev {
			b.Write(t.src[prev:c.StartByte()])
		}
		b.WriteString(t.emit(c))
		prev = c.EndByte()
	}
	if n.EndByte() > prev {
		b.Write(t.src[prev:n.EndByte()])
	}
	return b.String()
}

// emitTestBody is the same as emitDefault — recurse into block children.
func (t *dmjTranspiler) emitTestBody(n *gotreesitter.Node) string {
	cc := int(n.ChildCount())
	if cc == 0 {
		return t.text(n)
	}
	var b strings.Builder
	prev := n.StartByte()
	for i := 0; i < cc; i++ {
		c := n.Child(i)
		if c.StartByte() > prev {
			b.Write(t.src[prev:c.StartByte()])
		}
		b.WriteString(t.emit(c))
		prev = c.EndByte()
	}
	if n.EndByte() > prev {
		b.Write(t.src[prev:n.EndByte()])
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Test block → func TestXxx(t *testing.T)
// ---------------------------------------------------------------------------

var nonAlphaNum = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func sanitizeTestName(name string) string {
	name = strings.Trim(name, "\"'`")
	parts := nonAlphaNum.Split(name, -1)
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

func (t *dmjTranspiler) emitTestBlock(n *gotreesitter.Node) string {
	var b strings.Builder

	// Extract category
	category := ""
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if t.nodeType(c) == "test_category" {
			category = t.text(c)
			break
		}
	}

	// Extract name
	nameNode := t.childByField(n, "name")
	name := "Test"
	if nameNode != nil {
		name = sanitizeTestName(t.text(nameNode))
	}

	// Extract tags
	var tags []string
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if t.nodeType(c) == "tag_list" {
			for j := 0; j < int(c.NamedChildCount()); j++ {
				tc := c.NamedChild(j)
				if t.nodeType(tc) == "tag" {
					tags = append(tags, t.text(tc))
				}
			}
		}
	}

	// Emit tags as comments
	for _, tag := range tags {
		fmt.Fprintf(&b, "// Tag: %s\n", tag)
	}

	// Build constraint for category
	if category == "integration" || category == "e2e" {
		fmt.Fprintf(&b, "//go:build %s\n\n", category)
	}

	// Emit any collected mock declarations before the function
	if len(t.mockDecls) > 0 {
		for _, md := range t.mockDecls {
			b.WriteString(md)
		}
		// Clear so we don't re-emit for a second test_block
		t.mockDecls = nil
	}

	// Function signature
	fmt.Fprintf(&b, "func Test%s(%s *testing.T) ", name, t.testVar)

	// Emit body block
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if t.nodeType(c) == "block" {
			b.WriteString(t.emitTestBody(c))
			break
		}
	}
	b.WriteString("\n")

	return b.String()
}

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
	var b strings.Builder
	// Find statement_list inside the block
	for i := 0; i < int(blockNode.NamedChildCount()); i++ {
		c := blockNode.NamedChild(i)
		if t.nodeType(c) == "statement_list" {
			for j := 0; j < int(c.NamedChildCount()); j++ {
				stmt := c.NamedChild(j)
				fmt.Fprintf(&b, "%s%s\n", indent, t.emit(stmt))
			}
			return b.String()
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// given/when/then → t.Run("description", func(t *testing.T) { ... })
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitBDDBlock(n *gotreesitter.Node, keyword string) string {
	desc := t.childByField(n, "description")
	descText := `"` + keyword + `"`
	if desc != nil {
		descText = t.text(desc)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s.Run(%s, func(%s *testing.T) ", t.testVar, descText, t.testVar)

	// Find and emit the block
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if t.nodeType(c) == "block" {
			b.WriteString(t.emitTestBody(c))
			break
		}
	}
	b.WriteString(")")

	return b.String()
}

// ---------------------------------------------------------------------------
// expect → assertion
//
// CRITICAL: Go's grammar absorbs == / != into binary_expression, so when
// expect's "actual" field is a binary_expression node we must extract
// left/op/right from its children (Child(0), Child(1), Child(2)) and emit
// the appropriate assertion. For bare expect (no binary op), emit truthiness.
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitExpect(n *gotreesitter.Node) string {
	actual := t.childByField(n, "actual")
	expected := t.childByField(n, "expected")

	if actual == nil {
		return t.text(n)
	}

	// Check for matchers in the raw text of the node
	nodeText := t.text(n)

	if strings.Contains(nodeText, "is_nil") {
		t.addImport("github.com/stretchr/testify/assert")
		actualText := t.emit(actual)
		return fmt.Sprintf("assert.Nil(%s, %s)", t.testVar, actualText)
	}
	if strings.Contains(nodeText, "not_nil") {
		t.addImport("github.com/stretchr/testify/assert")
		actualText := t.emit(actual)
		return fmt.Sprintf("assert.NotNil(%s, %s)", t.testVar, actualText)
	}
	if strings.Contains(nodeText, "contains") && expected != nil {
		t.addImport("github.com/stretchr/testify/assert")
		actualText := t.emit(actual)
		expectedText := t.emit(expected)
		return fmt.Sprintf("assert.Contains(%s, %s, %s)", t.testVar, actualText, expectedText)
	}

	// If the grammar's explicit expected field is populated, use it directly.
	if expected != nil {
		actualText := t.emit(actual)
		expectedText := t.emit(expected)
		if strings.Contains(nodeText, "!=") {
			// Special case: x != nil
			if expectedText == "nil" {
				t.addImport("github.com/stretchr/testify/assert")
				return fmt.Sprintf("assert.NotNil(%s, %s)", t.testVar, actualText)
			}
			t.addImport("github.com/stretchr/testify/assert")
			return fmt.Sprintf("assert.NotEqual(%s, %s, %s)", t.testVar, expectedText, actualText)
		}
		// Special case: err == nil → require.NoError
		if expectedText == "nil" && strings.HasSuffix(actualText, "err") {
			t.addImport("github.com/stretchr/testify/require")
			return fmt.Sprintf("require.NoError(%s, %s)", t.testVar, actualText)
		}
		// Special case: x == nil
		if expectedText == "nil" {
			t.addImport("github.com/stretchr/testify/assert")
			return fmt.Sprintf("assert.Nil(%s, %s)", t.testVar, actualText)
		}
		t.addImport("github.com/stretchr/testify/assert")
		return fmt.Sprintf("assert.Equal(%s, %s, %s)", t.testVar, expectedText, actualText)
	}

	// If actual is a binary_expression (e.g. Go absorbed "x == 5" into one node),
	// extract left/op/right from its children.
	if t.nodeType(actual) == "binary_expression" && actual.ChildCount() >= 3 {
		left := actual.Child(0)
		op := actual.Child(1)
		right := actual.Child(2)
		lT := t.emit(left)
		opT := t.text(op)
		rT := t.emit(right)
		switch opT {
		case "==":
			// Special case: err == nil → require.NoError
			if rT == "nil" && strings.HasSuffix(lT, "err") {
				t.addImport("github.com/stretchr/testify/require")
				return fmt.Sprintf("require.NoError(%s, %s)", t.testVar, lT)
			}
			// Special case: x == nil
			if rT == "nil" {
				t.addImport("github.com/stretchr/testify/assert")
				return fmt.Sprintf("assert.Nil(%s, %s)", t.testVar, lT)
			}
			t.addImport("github.com/stretchr/testify/assert")
			return fmt.Sprintf("assert.Equal(%s, %s, %s)", t.testVar, rT, lT)
		case "!=":
			if rT == "nil" {
				t.addImport("github.com/stretchr/testify/assert")
				return fmt.Sprintf("assert.NotNil(%s, %s)", t.testVar, lT)
			}
			t.addImport("github.com/stretchr/testify/assert")
			return fmt.Sprintf("assert.NotEqual(%s, %s, %s)", t.testVar, rT, lT)
		case "<":
			t.addImport("github.com/stretchr/testify/assert")
			return fmt.Sprintf("assert.Less(%s, %s, %s)", t.testVar, lT, rT)
		case ">":
			t.addImport("github.com/stretchr/testify/assert")
			return fmt.Sprintf("assert.Greater(%s, %s, %s)", t.testVar, lT, rT)
		case "<=":
			t.addImport("github.com/stretchr/testify/assert")
			return fmt.Sprintf("assert.LessOrEqual(%s, %s, %s)", t.testVar, lT, rT)
		case ">=":
			t.addImport("github.com/stretchr/testify/assert")
			return fmt.Sprintf("assert.GreaterOrEqual(%s, %s, %s)", t.testVar, lT, rT)
		}
	}

	// Bare expect (truthiness check)
	t.addImport("github.com/stretchr/testify/assert")
	actualText := t.emit(actual)
	return fmt.Sprintf("assert.True(%s, %s)", t.testVar, actualText)
}

// ---------------------------------------------------------------------------
// reject → inverse truthiness
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitReject(n *gotreesitter.Node) string {
	actual := t.childByField(n, "actual")
	if actual == nil {
		return t.text(n)
	}
	t.addImport("github.com/stretchr/testify/assert")
	actualText := t.emit(actual)
	return fmt.Sprintf("assert.False(%s, %s)", t.testVar, actualText)
}

// ---------------------------------------------------------------------------
// mock → struct with call counters + methods (package-level)
// ---------------------------------------------------------------------------

type mockMethodInfo struct {
	name       string
	params     string
	returnType string
	defaultVal string
}

func (t *dmjTranspiler) parseMockMethod(n *gotreesitter.Node) mockMethodInfo {
	info := mockMethodInfo{}
	if name := t.childByField(n, "name"); name != nil {
		info.name = t.text(name)
	}
	if params := t.childByField(n, "parameters"); params != nil {
		info.params = t.text(params)
	}
	if ret := t.childByField(n, "return_type"); ret != nil {
		info.returnType = t.text(ret)
	}
	if def := t.childByField(n, "default_value"); def != nil {
		info.defaultVal = t.text(def)
	}
	return info
}

// buildMockDecl generates the Go struct type + methods string for a mock_declaration
// node. The result is meant to be emitted at package level.
func (t *dmjTranspiler) buildMockDecl(n *gotreesitter.Node) string {
	nameNode := t.childByField(n, "name")
	if nameNode == nil {
		return t.text(n)
	}
	mockName := t.text(nameNode)
	structName := "mock" + mockName

	var methods []mockMethodInfo
	// Walk block children looking for mock_method nodes.
	// The block may contain them directly or inside a statement_list.
	t.findMockMethods(n, &methods)

	var b strings.Builder

	// Struct with call counters
	fmt.Fprintf(&b, "type %s struct {\n", structName)
	for _, m := range methods {
		fmt.Fprintf(&b, "\t%sCalls int\n", m.name)
		if m.returnType != "" {
			fmt.Fprintf(&b, "\t%sResult %s\n", m.name, m.returnType)
		}
	}
	fmt.Fprintf(&b, "}\n\n")

	// Methods
	for _, m := range methods {
		fmt.Fprintf(&b, "func (m *%s) %s%s", structName, m.name, m.params)
		if m.returnType != "" {
			fmt.Fprintf(&b, " %s", m.returnType)
		}
		fmt.Fprintf(&b, " {\n")
		fmt.Fprintf(&b, "\tm.%sCalls++\n", m.name)
		if m.defaultVal != "" {
			fmt.Fprintf(&b, "\treturn %s\n", m.defaultVal)
		} else if m.returnType != "" {
			fmt.Fprintf(&b, "\treturn m.%sResult\n", m.name)
		}
		fmt.Fprintf(&b, "}\n\n")
	}

	return b.String()
}

// findMockMethods recursively finds mock_method nodes under n.
func (t *dmjTranspiler) findMockMethods(n *gotreesitter.Node, out *[]mockMethodInfo) {
	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		if t.nodeType(c) == "mock_method" {
			*out = append(*out, t.parseMockMethod(c))
		} else {
			t.findMockMethods(c, out)
		}
	}
}

// ---------------------------------------------------------------------------
// lifecycle hooks
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitLifecycleHook(n *gotreesitter.Node) string {
	nodeText := t.text(n)
	isAfter := strings.HasPrefix(strings.TrimSpace(nodeText), "after")

	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if t.nodeType(c) == "block" {
			blockContent := t.emitTestBody(c)
			if isAfter {
				return fmt.Sprintf("%s.Cleanup(func() %s)", t.testVar, blockContent)
			}
			// before each: inline the block contents (strip outer braces)
			inner := strings.TrimSpace(blockContent)
			if strings.HasPrefix(inner, "{") && strings.HasSuffix(inner, "}") {
				inner = inner[1 : len(inner)-1]
			}
			return inner
		}
	}
	return t.text(n)
}

// ---------------------------------------------------------------------------
// verify → call count assertion
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitVerify(n *gotreesitter.Node) string {
	target := t.childByField(n, "target")
	assertion := t.childByField(n, "assertion")
	if target == nil || assertion == nil {
		return t.text(n)
	}
	targetText := t.emit(target)
	assertText := t.text(assertion)

	if strings.Contains(assertText, "not_called") {
		return fmt.Sprintf("if %sCalls != 0 { %s.Errorf(\"expected %%s not called, got %%d calls\", %q, %sCalls) }",
			targetText, t.testVar, targetText, targetText)
	}
	if strings.Contains(assertText, "called") && strings.Contains(assertText, "times") {
		parts := strings.Fields(assertText)
		count := "0"
		for i, p := range parts {
			if p == "called" && i+1 < len(parts) {
				count = parts[i+1]
				break
			}
		}
		return fmt.Sprintf("if %sCalls != %s { %s.Errorf(\"expected %%d calls to %%s, got %%d\", %s, %q, %sCalls) }",
			targetText, count, t.testVar, count, targetText, targetText)
	}
	return t.text(n)
}

// ---------------------------------------------------------------------------
// needs_block → testcontainers setup
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitNeedsBlock(n *gotreesitter.Node) string {
	t.addImport("github.com/stretchr/testify/require")

	serviceNode := t.childByField(n, "service")
	nameNode := t.childByField(n, "name")
	if serviceNode == nil || nameNode == nil {
		return t.text(n)
	}
	serviceType := t.text(serviceNode)
	varName := t.text(nameNode)

	tv := t.testVar
	var b strings.Builder
	switch serviceType {
	case "postgres":
		fmt.Fprintf(&b, "%sContainer, err := postgres.Run(ctx, \"postgres:15\",\n"+
			"\tpostgres.WithDatabase(\"test\"),\n"+
			"\ttestcontainers.WithWaitStrategy(wait.ForListeningPort(\"5432/tcp\")),\n"+
			")\n"+
			"require.NoError(%s, err)\n"+
			"%s.Cleanup(func() { %sContainer.Terminate(ctx) })\n"+
			"%sURL, err := %sContainer.ConnectionString(ctx)\n"+
			"require.NoError(%s, err)", varName, tv, tv, varName, varName, varName, tv)
	case "redis":
		fmt.Fprintf(&b, "%sContainer, err := redis.Run(ctx, \"redis:7\")\n"+
			"require.NoError(%s, err)\n"+
			"%s.Cleanup(func() { %sContainer.Terminate(ctx) })\n"+
			"%sURL, err := %sContainer.ConnectionString(ctx)\n"+
			"require.NoError(%s, err)", varName, tv, tv, varName, varName, varName, tv)
	case "mysql":
		fmt.Fprintf(&b, "%sContainer, err := mysql.Run(ctx, \"mysql:8\")\n"+
			"require.NoError(%s, err)\n"+
			"%s.Cleanup(func() { %sContainer.Terminate(ctx) })\n"+
			"%sURL, err := %sContainer.ConnectionString(ctx)\n"+
			"require.NoError(%s, err)", varName, tv, tv, varName, varName, varName, tv)
	case "kafka":
		fmt.Fprintf(&b, "%sContainer, err := kafka.Run(ctx, \"confluentinc/confluent-local:7.5.0\")\n"+
			"require.NoError(%s, err)\n"+
			"%s.Cleanup(func() { %sContainer.Terminate(ctx) })\n"+
			"%sBrokers, err := %sContainer.Brokers(ctx)\n"+
			"require.NoError(%s, err)", varName, tv, tv, varName, varName, varName, tv)
	case "mongo":
		fmt.Fprintf(&b, "%sContainer, err := mongodb.Run(ctx, \"mongo:7\")\n"+
			"require.NoError(%s, err)\n"+
			"%s.Cleanup(func() { %sContainer.Terminate(ctx) })\n"+
			"%sURI := %sContainer.GetConnectionString()", varName, tv, tv, varName, varName, varName)
	case "rabbitmq":
		fmt.Fprintf(&b, "%sContainer, err := rabbitmq.Run(ctx, \"rabbitmq:3-management\")\n"+
			"require.NoError(%s, err)\n"+
			"%s.Cleanup(func() { %sContainer.Terminate(ctx) })\n"+
			"%sURL, err := %sContainer.AmqpURL(ctx)\n"+
			"require.NoError(%s, err)", varName, tv, tv, varName, varName, varName, tv)
	case "nats":
		fmt.Fprintf(&b, "%sContainer, err := nats.Run(ctx, \"nats:2\")\n"+
			"require.NoError(%s, err)\n"+
			"%s.Cleanup(func() { %sContainer.Terminate(ctx) })\n"+
			"%sURL, err := %sContainer.ConnectionString(ctx)\n"+
			"require.NoError(%s, err)", varName, tv, tv, varName, varName, varName, tv)
	case "container":
		fmt.Fprintf(&b, "%sReq := testcontainers.ContainerRequest{\n"+
			"\tImage:        \"alpine:latest\",\n"+
			"\tExposedPorts: []string{},\n"+
			"}\n"+
			"%sContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{\n"+
			"\tContainerRequest: %sReq,\n"+
			"\tStarted:          true,\n"+
			"})\n"+
			"require.NoError(%s, err)\n"+
			"%s.Cleanup(func() { %sContainer.Terminate(ctx) })", varName, varName, varName, tv, tv, varName)
	default:
		return fmt.Sprintf("// unsupported needs service type: %s", serviceType)
	}

	return b.String()
}

// ---------------------------------------------------------------------------
// load_block → func TestLoadXxx(t *testing.T) with vegeta attack
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitLoad(n *gotreesitter.Node) string {
	t.addImport("time")
	t.addImport("github.com/tsenart/vegeta/v12/lib")

	// Extract name
	nameNode := t.childByField(n, "name")
	name := "Load"
	if nameNode != nil {
		name = sanitizeTestName(t.text(nameNode))
	}

	// Walk the body block's children for load_config, target_block, then_block
	bodyNode := t.childByField(n, "body")
	if bodyNode == nil {
		return t.text(n)
	}

	rate := "10"
	duration := "5"
	method := "GET"
	url := `"http://localhost"`
	var thenBlocks []*gotreesitter.Node

	t.walkChildren(bodyNode, func(child *gotreesitter.Node) {
		switch t.nodeType(child) {
		case "load_config":
			configText := t.text(child)
			// Extract the value (everything after the keyword)
			if strings.HasPrefix(configText, "rate") {
				val := strings.TrimSpace(strings.TrimPrefix(configText, "rate"))
				if val != "" {
					rate = val
				}
			} else if strings.HasPrefix(configText, "duration") {
				val := strings.TrimSpace(strings.TrimPrefix(configText, "duration"))
				if val != "" {
					duration = val
				}
			}
		case "target_block":
			if m := t.childByField(child, "method"); m != nil {
				method = strings.ToUpper(t.text(m))
			}
			if u := t.childByField(child, "url"); u != nil {
				url = t.text(u)
			}
		case "then_block":
			thenBlocks = append(thenBlocks, child)
		}
	})

	var b strings.Builder

	// Build constraint
	fmt.Fprintf(&b, "//go:build e2e\n\n")

	// Function signature
	fmt.Fprintf(&b, "func TestLoad%s(t *testing.T) {\n", name)

	// Vegeta setup
	fmt.Fprintf(&b, "\trate := vegeta.Rate{Freq: %s, Per: time.Second}\n", rate)
	fmt.Fprintf(&b, "\tduration := %s * time.Second\n", duration)
	fmt.Fprintf(&b, "\ttargeter := vegeta.NewStaticTargeter(vegeta.Target{\n")
	fmt.Fprintf(&b, "\t\tMethod: %q,\n", method)
	fmt.Fprintf(&b, "\t\tURL:    %s,\n", url)
	fmt.Fprintf(&b, "\t})\n")
	fmt.Fprintf(&b, "\tattacker := vegeta.NewAttacker()\n")
	fmt.Fprintf(&b, "\tvar metrics vegeta.Metrics\n")
	fmt.Fprintf(&b, "\tfor res := range attacker.Attack(targeter, rate, duration, %q) {\n", name)
	fmt.Fprintf(&b, "\t\tmetrics.Add(res)\n")
	fmt.Fprintf(&b, "\t}\n")
	fmt.Fprintf(&b, "\tmetrics.Close()\n")

	// Emit then blocks
	for _, tb := range thenBlocks {
		b.WriteString("\t")
		b.WriteString(t.emitBDDBlock(tb, "then"))
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "}\n")

	return b.String()
}

// ---------------------------------------------------------------------------
// injectImports adds all collected import paths into the existing import block
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) injectImports(code string) string {
	if len(t.neededImports) == 0 {
		return code
	}

	// Build sorted list of import paths for deterministic output,
	// filtering out packages that are already imported in the source.
	var imports []string
	for pkg := range t.neededImports {
		quoted := fmt.Sprintf("%q", pkg)
		if strings.Contains(code, quoted) {
			continue // already imported
		}
		imports = append(imports, quoted)
	}
	if len(imports) == 0 {
		return code
	}
	// Sort for deterministic output
	sortImports(imports)

	importBlock := "\n\t" + strings.Join(imports, "\n\t")

	// Try to find an existing import block and append inside it.
	// Look for the closing paren of an import(...) block.
	if idx := strings.Index(code, "import ("); idx >= 0 {
		// Find the matching closing paren
		closeIdx := strings.Index(code[idx:], ")")
		if closeIdx >= 0 {
			insertAt := idx + closeIdx
			return code[:insertAt] + importBlock + "\n" + code[insertAt:]
		}
	}

	// If there's a single import "..." line, convert to block form
	if idx := strings.Index(code, "import \""); idx >= 0 {
		endIdx := strings.Index(code[idx:], "\n")
		if endIdx >= 0 {
			origImport := code[idx : idx+endIdx]
			// Extract the import path from: import "path"
			path := strings.TrimPrefix(origImport, "import ")
			newBlock := "import (\n\t" + path + importBlock + "\n)"
			return code[:idx] + newBlock + code[idx+endIdx:]
		}
	}

	return code
}

// sortImports sorts import strings lexicographically.
func sortImports(imports []string) {
	for i := 1; i < len(imports); i++ {
		for j := i; j > 0 && imports[j] < imports[j-1]; j-- {
			imports[j], imports[j-1] = imports[j-1], imports[j]
		}
	}
}

// ---------------------------------------------------------------------------
// exec_block → t.Run with os/exec command execution
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitExec(n *gotreesitter.Node) string {
	t.addImport("bytes")
	t.addImport("os/exec")

	nameNode := t.childByField(n, "name")
	name := "exec"
	if nameNode != nil {
		name = strings.Trim(t.text(nameNode), "\"`'")
	}

	// Find the body block
	bodyNode := t.childByField(n, "body")
	if bodyNode == nil {
		return t.text(n)
	}

	// Collect run_command nodes and other statements from the body
	type runCmd struct {
		command string
	}
	var runs []runCmd
	var otherStatements []*gotreesitter.Node

	t.walkChildren(bodyNode, func(child *gotreesitter.Node) {
		switch t.nodeType(child) {
		case "run_command":
			cmdNode := t.childByField(child, "command")
			if cmdNode != nil {
				runs = append(runs, runCmd{command: t.text(cmdNode)})
			}
		case "expect_statement":
			otherStatements = append(otherStatements, child)
		}
	})

	var b strings.Builder
	fmt.Fprintf(&b, "%s.Run(%q, func(%s *testing.T) {\n", t.testVar, name, t.testVar)

	// For each run command, emit the exec boilerplate
	for _, r := range runs {
		fmt.Fprintf(&b, "\tvar stdout, stderr bytes.Buffer\n")
		fmt.Fprintf(&b, "\tcmd := exec.Command(\"sh\", \"-c\", %s)\n", r.command)
		fmt.Fprintf(&b, "\tcmd.Stdout = &stdout\n")
		fmt.Fprintf(&b, "\tcmd.Stderr = &stderr\n")
		fmt.Fprintf(&b, "\terr := cmd.Run()\n")
		fmt.Fprintf(&b, "\texitCode := 0\n")
		fmt.Fprintf(&b, "\tif err != nil {\n")
		fmt.Fprintf(&b, "\t\tif exitErr, ok := err.(*exec.ExitError); ok {\n")
		fmt.Fprintf(&b, "\t\t\texitCode = exitErr.ExitCode()\n")
		fmt.Fprintf(&b, "\t\t} else {\n")
		fmt.Fprintf(&b, "\t\t\texitCode = -1\n")
		fmt.Fprintf(&b, "\t\t}\n")
		fmt.Fprintf(&b, "\t}\n")
		fmt.Fprintf(&b, "\t_ = exitCode // used by expect assertions\n")
	}

	// Emit expect statements with exec identifier translation
	oldInExec := t.inExecBlock
	t.inExecBlock = true
	for _, stmt := range otherStatements {
		b.WriteString("\t")
		b.WriteString(t.emitExecExpect(stmt))
		b.WriteString("\n")
	}
	t.inExecBlock = oldInExec

	fmt.Fprintf(&b, "})")
	return b.String()
}

// walkChildren calls fn for each named child (recursing into statement_list/block).
func (t *dmjTranspiler) walkChildren(n *gotreesitter.Node, fn func(*gotreesitter.Node)) {
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		nt := t.nodeType(child)
		switch nt {
		case "block", "statement_list":
			t.walkChildren(child, fn)
		default:
			if child.IsNamed() {
				fn(child)
			}
		}
	}
}

// emitExecExpect translates expect statements inside exec blocks,
// replacing exec-specific identifiers with their Go equivalents.
func (t *dmjTranspiler) emitExecExpect(n *gotreesitter.Node) string {
	nodeText := t.text(n)

	// Handle "expect stdout contains X"
	if strings.Contains(nodeText, "stdout") && strings.Contains(nodeText, "contains") {
		// Extract the expected value after "contains"
		expected := t.childByField(n, "expected")
		if expected != nil {
			t.addImport("github.com/stretchr/testify/assert")
			return fmt.Sprintf("assert.Contains(%s, stdout.String(), %s)", t.testVar, t.emit(expected))
		}
	}

	// Handle "expect stderr contains X"
	if strings.Contains(nodeText, "stderr") && strings.Contains(nodeText, "contains") {
		expected := t.childByField(n, "expected")
		if expected != nil {
			t.addImport("github.com/stretchr/testify/assert")
			return fmt.Sprintf("assert.Contains(%s, stderr.String(), %s)", t.testVar, t.emit(expected))
		}
	}

	// Handle "expect exit_code == 0" — the grammar absorbs this as binary_expression
	actual := t.childByField(n, "actual")
	if actual != nil {
		actualText := t.text(actual)
		// Check if it's a binary expression with exit_code
		if t.nodeType(actual) == "binary_expression" && actual.ChildCount() >= 3 {
			left := actual.Child(0)
			op := actual.Child(1)
			right := actual.Child(2)
			lT := t.translateExecIdent(t.text(left))
			opT := t.text(op)
			rT := t.emit(right)
			t.addImport("github.com/stretchr/testify/assert")
			switch opT {
			case "==":
				return fmt.Sprintf("assert.Equal(%s, %s, %s)", t.testVar, rT, lT)
			case "!=":
				return fmt.Sprintf("assert.NotEqual(%s, %s, %s)", t.testVar, rT, lT)
			}
		}
		// Bare identifier like "expect exit_code"
		if strings.Contains(actualText, "exit_code") || strings.Contains(actualText, "stdout") || strings.Contains(actualText, "stderr") {
			translated := t.translateExecIdent(actualText)
			t.addImport("github.com/stretchr/testify/assert")
			return fmt.Sprintf("assert.True(%s, %s)", t.testVar, translated)
		}
	}

	// Fall through to normal expect emission
	return t.emitExpect(n)
}

// translateExecIdent maps exec-specific identifiers to Go variable names.
func (t *dmjTranspiler) translateExecIdent(ident string) string {
	switch strings.TrimSpace(ident) {
	case "exit_code":
		return "exitCode"
	case "stdout":
		return "stdout.String()"
	case "stderr":
		return "stderr.String()"
	default:
		return ident
	}
}

// ---------------------------------------------------------------------------
// profile_block → runtime profiling instrumentation
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitProfile(n *gotreesitter.Node) string {
	// Extract profile_type from children
	profileType := ""
	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		if t.nodeType(c) == "profile_type" {
			profileType = t.text(c)
			break
		}
	}

	var b strings.Builder
	switch profileType {
	case "routines":
		t.addImport("runtime")
		t.addImport("time")
		b.WriteString("_goroutinesBefore := runtime.NumGoroutine()\n")
		b.WriteString("defer func() {\n")
		b.WriteString("\truntime.GC()\n")
		b.WriteString("\ttime.Sleep(100 * time.Millisecond)\n")
		b.WriteString("\t_goroutinesAfter := runtime.NumGoroutine()\n")
		b.WriteString("\t_goroutineDelta := _goroutinesAfter - _goroutinesBefore\n")
		b.WriteString("\t_ = _goroutineDelta // available for assertions\n")
		b.WriteString("}()\n")
	case "cpu":
		t.addImport("runtime/pprof")
		t.addImport("os")
		b.WriteString("_cpuProfFile, _cpuProfErr := os.CreateTemp(\"\", \"cpu_profile_*.prof\")\n")
		b.WriteString("if _cpuProfErr == nil {\n")
		b.WriteString("\tpprof.StartCPUProfile(_cpuProfFile)\n")
		b.WriteString("\tdefer func() {\n")
		b.WriteString("\t\tpprof.StopCPUProfile()\n")
		b.WriteString("\t\t_cpuProfFile.Close()\n")
		b.WriteString("\t}()\n")
		b.WriteString("}\n")
	case "mem":
		t.addImport("runtime")
		t.addImport("runtime/pprof")
		t.addImport("os")
		b.WriteString("defer func() {\n")
		b.WriteString("\truntime.GC()\n")
		b.WriteString("\t_memProfFile, _memProfErr := os.CreateTemp(\"\", \"mem_profile_*.prof\")\n")
		b.WriteString("\tif _memProfErr == nil {\n")
		b.WriteString("\t\tpprof.WriteHeapProfile(_memProfFile)\n")
		b.WriteString("\t\t_memProfFile.Close()\n")
		b.WriteString("\t}\n")
		b.WriteString("}()\n")
	case "allocs":
		t.addImport("runtime")
		b.WriteString("var _memStatsBefore runtime.MemStats\n")
		b.WriteString("runtime.ReadMemStats(&_memStatsBefore)\n")
		b.WriteString("defer func() {\n")
		b.WriteString("\tvar _memStatsAfter runtime.MemStats\n")
		b.WriteString("\truntime.ReadMemStats(&_memStatsAfter)\n")
		b.WriteString("\t_allocsDelta := _memStatsAfter.TotalAlloc - _memStatsBefore.TotalAlloc\n")
		b.WriteString("\t_ = _allocsDelta // available for assertions\n")
		b.WriteString("}()\n")
	case "blockprofile":
		t.addImport("runtime")
		b.WriteString("runtime.SetBlockProfileRate(1)\n")
		b.WriteString("defer runtime.SetBlockProfileRate(0)\n")
	case "mutexprofile":
		t.addImport("runtime")
		b.WriteString("runtime.SetMutexProfileFraction(1)\n")
		b.WriteString("defer runtime.SetMutexProfileFraction(0)\n")
	default:
		b.WriteString(fmt.Sprintf("// unsupported profile type: %s\n", profileType))
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// fake_declaration → struct with real method bodies (package-level)
// ---------------------------------------------------------------------------

type fakeMethodInfo struct {
	name       string
	params     string
	returnType string
	bodyText   string
}

func (t *dmjTranspiler) parseFakeMethod(n *gotreesitter.Node) fakeMethodInfo {
	info := fakeMethodInfo{}
	if name := t.childByField(n, "name"); name != nil {
		info.name = t.text(name)
	}
	if params := t.childByField(n, "parameters"); params != nil {
		info.params = t.text(params)
	}
	if ret := t.childByField(n, "return_type"); ret != nil {
		info.returnType = t.text(ret)
	}
	if body := t.childByField(n, "body"); body != nil {
		info.bodyText = t.emitTestBody(body)
	}
	return info
}

func (t *dmjTranspiler) buildFakeDecl(n *gotreesitter.Node) string {
	nameNode := t.childByField(n, "name")
	if nameNode == nil {
		return t.text(n)
	}
	fakeName := t.text(nameNode)
	structName := "fake" + fakeName

	var methods []fakeMethodInfo
	t.findFakeMethods(n, &methods)

	var b strings.Builder

	// Struct definition
	fmt.Fprintf(&b, "type %s struct{}\n\n", structName)

	// Methods with real bodies
	for _, m := range methods {
		fmt.Fprintf(&b, "func (f *%s) %s%s", structName, m.name, m.params)
		if m.returnType != "" {
			fmt.Fprintf(&b, " %s", m.returnType)
		}
		fmt.Fprintf(&b, " %s\n\n", m.bodyText)
	}

	return b.String()
}

func (t *dmjTranspiler) findFakeMethods(n *gotreesitter.Node, out *[]fakeMethodInfo) {
	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		if t.nodeType(c) == "fake_method" {
			*out = append(*out, t.parseFakeMethod(c))
		} else {
			t.findFakeMethods(c, out)
		}
	}
}

// ---------------------------------------------------------------------------
// spy_declaration → struct wrapping real implementation with call recording
//
// A spy has an `inner` field that delegates to the real implementation.
// Each method records calls and args, then passes through to inner.
// Uses mock_method syntax inside the body (same as mock_declaration).
//
// spy EventBus {
//     Publish(topic string, data interface{})
//     Subscribe(topic string) -> chan interface{}
// }
//
// Generates:
//   type spyEventBus struct {
//       inner          EventBus
//       PublishCalls   int
//       PublishArgs    [][]interface{}
//       SubscribeCalls int
//       SubscribeArgs  [][]interface{}
//   }
//   func (s *spyEventBus) Publish(topic string, data interface{}) { ... }
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitSpy(n *gotreesitter.Node) string {
	nameNode := t.childByField(n, "name")
	name := "Unknown"
	if nameNode != nil {
		name = t.text(nameNode)
	}

	// If no body, emit a placeholder comment (backwards compatible).
	bodyNode := t.childByField(n, "body")
	if bodyNode == nil {
		return fmt.Sprintf("// TODO: spy for %s — wrap real implementation with call recording", name)
	}

	// With a body, the struct is emitted at package level via buildSpyDecl.
	// This inline call should not happen if collectTopLevel did its job.
	return ""
}

// buildSpyDecl generates the Go struct type + methods string for a spy_declaration
// with a body. The result is emitted at package level.
func (t *dmjTranspiler) buildSpyDecl(n *gotreesitter.Node) string {
	nameNode := t.childByField(n, "name")
	if nameNode == nil {
		return ""
	}
	spyName := t.text(nameNode)
	structName := "spy" + spyName

	bodyNode := t.childByField(n, "body")
	if bodyNode == nil {
		// No body — emit a TODO placeholder. Not collected at package level.
		return ""
	}

	// Reuse findMockMethods to parse method signatures from the body.
	var methods []mockMethodInfo
	t.findMockMethods(n, &methods)

	var b strings.Builder

	// Struct with inner field + call counters + args slices
	fmt.Fprintf(&b, "type %s struct {\n", structName)
	fmt.Fprintf(&b, "\tinner %s\n", spyName)
	for _, m := range methods {
		fmt.Fprintf(&b, "\t%sCalls int\n", m.name)
		fmt.Fprintf(&b, "\t%sArgs [][]interface{}\n", m.name)
	}
	fmt.Fprintf(&b, "}\n\n")

	// Methods: record call, record args, delegate to inner
	for _, m := range methods {
		// Parse parameter names from the params string, e.g. "(topic string, data interface{})"
		paramNames := extractParamNames(m.params)

		fmt.Fprintf(&b, "func (s *%s) %s%s", structName, m.name, m.params)
		if m.returnType != "" {
			fmt.Fprintf(&b, " %s", m.returnType)
		}
		fmt.Fprintf(&b, " {\n")

		// Record call count
		fmt.Fprintf(&b, "\ts.%sCalls++\n", m.name)

		// Record args
		argsSlice := "[]interface{}{"
		for i, pn := range paramNames {
			if i > 0 {
				argsSlice += ", "
			}
			argsSlice += pn
		}
		argsSlice += "}"
		fmt.Fprintf(&b, "\ts.%sArgs = append(s.%sArgs, %s)\n", m.name, m.name, argsSlice)

		// Delegate to inner
		callArgs := strings.Join(paramNames, ", ")
		if m.returnType != "" {
			fmt.Fprintf(&b, "\treturn s.inner.%s(%s)\n", m.name, callArgs)
		} else {
			fmt.Fprintf(&b, "\ts.inner.%s(%s)\n", m.name, callArgs)
		}

		fmt.Fprintf(&b, "}\n\n")
	}

	return b.String()
}

// extractParamNames parses a Go parameter list string like "(topic string, data interface{})"
// and returns the parameter names: ["topic", "data"].
func extractParamNames(params string) []string {
	// Strip outer parens
	inner := strings.TrimSpace(params)
	if strings.HasPrefix(inner, "(") {
		inner = inner[1:]
	}
	if strings.HasSuffix(inner, ")") {
		inner = inner[:len(inner)-1]
	}
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return nil
	}

	// Split on commas, but respect nested braces/parens for interface{} etc.
	var names []string
	depth := 0
	start := 0
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '(', '{':
			depth++
		case ')', '}':
			depth--
		case ',':
			if depth == 0 {
				names = append(names, extractSingleParamName(inner[start:i]))
				start = i + 1
			}
		}
	}
	names = append(names, extractSingleParamName(inner[start:]))

	return names
}

// extractSingleParamName extracts the parameter name from a single param like "topic string".
func extractSingleParamName(param string) string {
	param = strings.TrimSpace(param)
	// Variadic: "args ...interface{}" → "args..."
	if idx := strings.Index(param, "..."); idx > 0 {
		name := strings.TrimSpace(param[:idx])
		return name + "..."
	}
	// Normal: "topic string" → "topic"
	parts := strings.Fields(param)
	if len(parts) >= 1 {
		return parts[0]
	}
	return param
}

// ---------------------------------------------------------------------------
// each_do_block → scenario-driven table test with defaults
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitEachDo(n *gotreesitter.Node) string {
	t.addImport("fmt")

	// Extract name
	nameNode := t.childByField(n, "name")
	name := "scenarios"
	if nameNode != nil {
		name = strings.Trim(t.text(nameNode), "\"'`")
	}
	structName := sanitizeTestName(name) + "Scenario"

	// Collect defaults
	defaults := make(map[string]string) // field_name → default_value_source
	var defaultsOrder []string

	// Collect scenario entries
	type scenarioEntry struct {
		fields map[string]string // field_name → value_source
	}
	var scenarios []scenarioEntry

	// All field names (for struct generation), preserving order
	allFieldsMap := make(map[string]bool)
	var allFields []string
	addField := func(f string) {
		if !allFieldsMap[f] {
			allFieldsMap[f] = true
			allFields = append(allFields, f)
		}
	}

	// Walk the scenarios block to find defaults_block and scenario_entry nodes.
	scenariosBlock := t.childByField(n, "scenarios")
	if scenariosBlock != nil {
		t.walkChildren(scenariosBlock, func(child *gotreesitter.Node) {
			switch t.nodeType(child) {
			case "defaults_block":
				t.extractScenarioFields(child, func(key, val string) {
					defaults[key] = val
					defaultsOrder = append(defaultsOrder, key)
					addField(key)
				})
			case "scenario_entry":
				entry := scenarioEntry{fields: make(map[string]string)}
				t.extractScenarioFields(child, func(key, val string) {
					entry.fields[key] = val
					addField(key)
				})
				scenarios = append(scenarios, entry)
			}
		})
	}

	// Build body
	bodyNode := t.childByField(n, "body")

	var b strings.Builder

	// Emit struct type
	fmt.Fprintf(&b, "type %s struct {\n", structName)
	for _, f := range allFields {
		fmt.Fprintf(&b, "\t%s interface{}\n", f)
	}
	fmt.Fprintf(&b, "}\n")

	// Emit scenario slice
	fmt.Fprintf(&b, "scenarios := []%s{\n", structName)
	for _, sc := range scenarios {
		b.WriteString("\t{")
		for i, f := range allFields {
			if i > 0 {
				b.WriteString(", ")
			}
			if val, ok := sc.fields[f]; ok {
				fmt.Fprintf(&b, "%s: %s", f, val)
			} else if defVal, ok := defaults[f]; ok {
				fmt.Fprintf(&b, "%s: %s", f, defVal)
			} else {
				fmt.Fprintf(&b, "%s: nil", f)
			}
		}
		b.WriteString("},\n")
	}
	b.WriteString("}\n")

	// Emit iteration loop
	tv := t.testVar
	fmt.Fprintf(&b, "for _, scenario := range scenarios {\n")
	fmt.Fprintf(&b, "\t_scenarioName := fmt.Sprintf(\"%%v\", scenario.name)\n")
	fmt.Fprintf(&b, "\t%s.Run(_scenarioName, func(%s *testing.T) {\n", tv, tv)
	fmt.Fprintf(&b, "\t\t%s.Parallel()\n", tv)

	// Emit the body
	if bodyNode != nil {
		b.WriteString(t.emitBlockInner(bodyNode, "\t\t"))
	}

	fmt.Fprintf(&b, "\t})\n")
	b.WriteString("}\n")

	return b.String()
}

// extractScenarioFields walks a node for scenario_field children and calls fn(key, val).
// Also handles the case where a single-field scenario_entry is parsed by Go's grammar
// as a labeled_statement (e.g., { name: "ok" } → label_name: expression_statement).
func (t *dmjTranspiler) extractScenarioFields(n *gotreesitter.Node, fn func(key, val string)) {
	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		switch t.nodeType(c) {
		case "scenario_field":
			keyNode := t.childByField(c, "key")
			valNode := t.childByField(c, "value")
			if keyNode != nil && valNode != nil {
				fn(t.text(keyNode), t.emit(valNode))
			}
		case "labeled_statement":
			// Fallback: { name: "ok" } parsed as label: expression
			if c.ChildCount() >= 2 {
				label := c.Child(0) // label_name
				if label != nil && t.nodeType(label) == "label_name" {
					key := t.text(label)
					// Find the expression child (skip the ":")
					for j := 1; j < int(c.ChildCount()); j++ {
						expr := c.Child(j)
						if expr.IsNamed() {
							fn(key, t.emit(expr))
							break
						}
					}
				}
			}
		default:
			t.extractScenarioFields(c, fn)
		}
	}
}

// ---------------------------------------------------------------------------
// matrix_block → cartesian product scenario-driven test
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitMatrix(n *gotreesitter.Node) string {
	t.addImport("fmt")

	// Extract name
	nameNode := t.childByField(n, "name")
	name := "matrix"
	if nameNode != nil {
		name = strings.Trim(t.text(nameNode), "\"'`")
	}
	structName := sanitizeTestName(name) + "Scenario"

	// Collect matrix fields from the dimensions block
	type matrixDim struct {
		key    string
		values []string
	}
	var dims []matrixDim

	dimsBlock := t.childByField(n, "dimensions")
	if dimsBlock != nil {
		t.walkChildren(dimsBlock, func(child *gotreesitter.Node) {
			if t.nodeType(child) == "matrix_field" {
				keyNode := t.childByField(child, "key")
				if keyNode == nil {
					return
				}
				dim := matrixDim{key: t.text(keyNode)}
				// Walk children of matrix_field for expression values (skip braces and key)
				for j := 0; j < int(child.ChildCount()); j++ {
					gc := child.Child(j)
					if gc.IsNamed() && t.nodeType(gc) != "identifier" {
						dim.values = append(dim.values, t.emit(gc))
					}
				}
				dims = append(dims, dim)
			}
		})
	}

	// Build body
	bodyNode := t.childByField(n, "body")

	// Generate cartesian product
	type combo map[string]string
	combos := []combo{{}}
	for _, dim := range dims {
		var newCombos []combo
		for _, existing := range combos {
			for _, val := range dim.values {
				c := make(combo)
				for k, v := range existing {
					c[k] = v
				}
				c[dim.key] = val
				newCombos = append(newCombos, c)
			}
		}
		combos = newCombos
	}

	var b strings.Builder

	// Emit struct type
	fmt.Fprintf(&b, "type %s struct {\n", structName)
	for _, dim := range dims {
		fmt.Fprintf(&b, "\t%s interface{}\n", dim.key)
	}
	fmt.Fprintf(&b, "}\n")

	// Emit scenario slice
	fmt.Fprintf(&b, "scenarios := []%s{\n", structName)
	for _, c := range combos {
		b.WriteString("\t{")
		for i, dim := range dims {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%s", c[dim.key])
		}
		b.WriteString("},\n")
	}
	b.WriteString("}\n")

	// Emit iteration loop with auto-generated names
	tv := t.testVar
	fmt.Fprintf(&b, "for _, scenario := range scenarios {\n")

	// Build name from all dim values: fmt.Sprintf("%v_%v", scenario.method, scenario.auth)
	var nameFormatParts []string
	var nameArgParts []string
	for _, dim := range dims {
		nameFormatParts = append(nameFormatParts, "%v")
		nameArgParts = append(nameArgParts, "scenario."+dim.key)
	}
	nameFormat := strings.Join(nameFormatParts, "_")
	nameArgs := strings.Join(nameArgParts, ", ")
	fmt.Fprintf(&b, "\tname := fmt.Sprintf(%q, %s)\n", nameFormat, nameArgs)

	fmt.Fprintf(&b, "\t%s.Run(name, func(%s *testing.T) {\n", tv, tv)
	fmt.Fprintf(&b, "\t\t%s.Parallel()\n", tv)

	// Emit the body
	if bodyNode != nil {
		b.WriteString(t.emitBlockInner(bodyNode, "\t\t"))
	}

	fmt.Fprintf(&b, "\t})\n")
	b.WriteString("}\n")

	return b.String()
}

// ---------------------------------------------------------------------------
// table_declaration → Go slice literal of anonymous struct rows
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitTable(n *gotreesitter.Node) string {
	t.addImport("fmt")
	nameNode := t.childByField(n, "name")
	tableName := "cases"
	if nameNode != nil {
		tableName = t.text(nameNode)
	}

	// Collect table rows
	var rows [][]string
	maxCols := 0
	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		if t.nodeType(c) == "table_row" {
			var cells []string
			for j := 0; j < int(c.NamedChildCount()); j++ {
				cell := c.NamedChild(j)
				cells = append(cells, t.emit(cell))
			}
			if len(cells) > maxCols {
				maxCols = len(cells)
			}
			rows = append(rows, cells)
		}
	}

	var b strings.Builder

	// Build struct field names
	var fields []string
	for i := 0; i < maxCols; i++ {
		fields = append(fields, fmt.Sprintf("col%d", i))
	}

	// Emit type and slice
	fmt.Fprintf(&b, "type %sRow struct { ", tableName)
	for i, f := range fields {
		if i > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%s interface{}", f)
	}
	b.WriteString(" }\n")
	fmt.Fprintf(&b, "%s := []%sRow{\n", tableName, tableName)
	for _, row := range rows {
		b.WriteString("\t{")
		for i, cell := range row {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(cell)
		}
		b.WriteString("},\n")
	}
	b.WriteString("}\n")
	fmt.Fprintf(&b, "_ = %s\n", tableName)

	return b.String()
}

// ---------------------------------------------------------------------------
// each_row_block → for range iteration over table
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitEachRow(n *gotreesitter.Node) string {
	t.addImport("fmt")
	tableNode := t.childByField(n, "table")
	tableName := "cases"
	if tableNode != nil {
		tableName = t.text(tableNode)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "for _, row := range %s {\n", tableName)
	fmt.Fprintf(&b, "\t%s.Run(fmt.Sprintf(\"row_%%v\", row), func(%s *testing.T) {\n", t.testVar, t.testVar)

	// Find and emit the block body
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if t.nodeType(c) == "block" {
			b.WriteString(t.emitBlockInner(c, "\t\t"))
			break
		}
	}

	fmt.Fprintf(&b, "\t})\n")
	b.WriteString("}\n")

	return b.String()
}

// ---------------------------------------------------------------------------
// no_leaks_directive → goroutine leak detection via t.Cleanup
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitNoLeaks(n *gotreesitter.Node) string {
	t.addImport("runtime")
	t.addImport("time")
	return fmt.Sprintf(`_goroutinesBefore := runtime.NumGoroutine()
%s.Cleanup(func() {
    time.Sleep(100 * time.Millisecond)
    runtime.GC()
    if _delta := runtime.NumGoroutine() - _goroutinesBefore; _delta > 0 {
        %s.Errorf("goroutine leak: %%d new goroutines still running", _delta)
    }
})`, t.testVar, t.testVar)
}

// ---------------------------------------------------------------------------
// fake_clock_directive → Clock interface + fakeClock struct + initialization
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitFakeClock(n *gotreesitter.Node) string {
	t.addImport("time")
	t.addImport("sync")

	startTime := t.childByField(n, "start_time")
	timezone := t.childByField(n, "timezone")

	// The Clock interface and fakeClock struct are collected during the first
	// pass (collectTopLevel) and emitted at package level. Here we only
	// generate the clock variable initialization (inline in the test function).
	var b strings.Builder
	if startTime != nil {
		timeStr := strings.Trim(t.text(startTime), `"`)
		if timezone != nil {
			tz := strings.Trim(t.text(timezone), `"`)
			b.WriteString(fmt.Sprintf("_loc, _ := time.LoadLocation(%s)\n", strconv.Quote(tz)))
			b.WriteString(fmt.Sprintf("_startTime, _ := time.ParseInLocation(time.RFC3339, %s, _loc)\n", strconv.Quote(timeStr)))
			b.WriteString("clock := &fakeClock{current: _startTime, loc: _loc}\n")
		} else {
			b.WriteString(fmt.Sprintf("_startTime, _ := time.Parse(time.RFC3339, %s)\n", strconv.Quote(timeStr)))
			b.WriteString("clock := &fakeClock{current: _startTime}\n")
		}
	} else {
		b.WriteString("clock := &fakeClock{current: time.Now()}\n")
	}

	return b.String()
}

// ---------------------------------------------------------------------------
// snapshot_block → t.Run with golden file comparison
//
// snapshot "valid_response" { body }
//
// The body is executed, and the last expression is captured as the snapshot value.
// The value is compared against a golden file at testdata/snapshots/<name>.golden.
// Set DANMUJI_UPDATE_SNAPSHOTS=1 to update golden files.
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitSnapshot(n *gotreesitter.Node) string {
	t.addImport("path/filepath")
	t.addImport("os")
	t.addImport("fmt")
	t.addImport("github.com/stretchr/testify/assert")

	nameNode := t.childByField(n, "name")
	snapshotName := "snapshot"
	if nameNode != nil {
		snapshotName = strings.Trim(t.text(nameNode), "\"'`")
	}

	// Walk the block children. Separate setup statements from the last expression
	// which becomes the snapshot value.
	var blockNode *gotreesitter.Node
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if t.nodeType(c) == "block" {
			blockNode = c
			break
		}
	}
	if blockNode == nil {
		return t.text(n)
	}

	// Collect all named children from the block's statement_list.
	var stmts []*gotreesitter.Node
	t.walkChildren(blockNode, func(child *gotreesitter.Node) {
		stmts = append(stmts, child)
	})

	// The last statement that looks like an expression is the snapshot value.
	// Everything before it is setup code.
	var setupStmts []*gotreesitter.Node
	snapshotExpr := ""

	if len(stmts) > 0 {
		last := stmts[len(stmts)-1]
		lastType := t.nodeType(last)
		// expression_statement wraps standalone expressions in Go grammar
		if lastType == "expression_statement" || isExpressionNode(lastType) {
			setupStmts = stmts[:len(stmts)-1]
			snapshotExpr = t.emit(last)
		} else {
			// All statements are setup; snapshot the empty string
			setupStmts = stmts
			snapshotExpr = `""`
		}
	}

	var b strings.Builder
	tv := t.testVar
	fmt.Fprintf(&b, "%s.Run(\"snapshot_%s\", func(%s *testing.T) {\n", tv, snapshotName, tv)

	// Emit setup statements
	for _, s := range setupStmts {
		fmt.Fprintf(&b, "\t%s\n", t.emit(s))
	}

	// Capture snapshot value
	fmt.Fprintf(&b, "\t_snapshotValue := fmt.Sprintf(\"%%v\", %s)\n", snapshotExpr)
	fmt.Fprintf(&b, "\n")

	// Golden file path
	fmt.Fprintf(&b, "\t_goldenPath := filepath.Join(\"testdata\", \"snapshots\", %q)\n", snapshotName+".golden")
	fmt.Fprintf(&b, "\n")

	// Update mode
	fmt.Fprintf(&b, "\tif os.Getenv(\"DANMUJI_UPDATE_SNAPSHOTS\") != \"\" {\n")
	fmt.Fprintf(&b, "\t\tos.MkdirAll(filepath.Dir(_goldenPath), 0755)\n")
	fmt.Fprintf(&b, "\t\tos.WriteFile(_goldenPath, []byte(_snapshotValue), 0644)\n")
	fmt.Fprintf(&b, "\t\t%s.Logf(\"snapshot updated: %%s\", _goldenPath)\n", tv)
	fmt.Fprintf(&b, "\t\treturn\n")
	fmt.Fprintf(&b, "\t}\n")
	fmt.Fprintf(&b, "\n")

	// Read and compare
	fmt.Fprintf(&b, "\t_expected, err := os.ReadFile(_goldenPath)\n")
	fmt.Fprintf(&b, "\tif err != nil {\n")
	fmt.Fprintf(&b, "\t\tos.MkdirAll(filepath.Dir(_goldenPath), 0755)\n")
	fmt.Fprintf(&b, "\t\tos.WriteFile(_goldenPath, []byte(_snapshotValue), 0644)\n")
	fmt.Fprintf(&b, "\t\t%s.Logf(\"snapshot created: %%s (run again to verify)\", _goldenPath)\n", tv)
	fmt.Fprintf(&b, "\t\treturn\n")
	fmt.Fprintf(&b, "\t}\n")
	fmt.Fprintf(&b, "\n")

	// Assert equality
	fmt.Fprintf(&b, "\tassert.Equal(%s, string(_expected), _snapshotValue, \"snapshot mismatch for %%s\\nRun with DANMUJI_UPDATE_SNAPSHOTS=1 to update\", %q)\n", tv, snapshotName)

	fmt.Fprintf(&b, "})")

	return b.String()
}

// isExpressionNode returns true if the node type is an expression-like node.
func isExpressionNode(nodeType string) bool {
	switch nodeType {
	case "call_expression", "selector_expression", "index_expression",
		"unary_expression", "binary_expression", "identifier",
		"int_literal", "float_literal", "interpreted_string_literal",
		"raw_string_literal", "rune_literal", "composite_literal",
		"func_literal", "parenthesized_expression", "true", "false", "nil":
		return true
	}
	return false
}
