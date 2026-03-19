package danmuji

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// Package-level cached language and expectation map to avoid regenerating on every call.
var (
	danmujiLangOnce            sync.Once
	danmujiLangCached          *gotreesitter.Language
	danmujiLangErr             error
	danmujiExpectationsCached  map[string]*ProductionExpectations
)

func getDanmujiLanguage() (*gotreesitter.Language, error) {
	danmujiLangOnce.Do(func() {
		g := DanmujiGrammar()
		danmujiExpectationsCached = buildExpectationMap(g)

		if blob, err := loadCachedDanmujiLanguageBlob(g); err == nil && len(blob) > 0 {
			if lang, err := LoadLanguageBlob(blob); err == nil {
				danmujiLangCached = lang
				return
			}
		}

		var blob []byte
		danmujiLangCached, blob, danmujiLangErr = GenerateLanguageAndBlob(g)
		if danmujiLangErr == nil {
			_ = storeCachedDanmujiLanguageBlob(g, blob)
		}
	})
	return danmujiLangCached, danmujiLangErr
}

// TranspileOptions controls optional transpiler behavior.
type TranspileOptions struct {
	SourceFile string
	Debug      bool
}

// TranspileDanmuji parses a .dmj source file and emits valid Go test code.
func TranspileDanmuji(source []byte, opts TranspileOptions) (string, error) {
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
		return "", fmt.Errorf("%s", FormatParseError(source, root, lang, opts.SourceFile, danmujiExpectationsCached))
	}

	tr := &dmjTranspiler{
		src:                source,
		lang:               lang,
		testVar:            "t",
		sourceFile:         opts.SourceFile,
		emitLineDirectives: opts.SourceFile != "" && !opts.Debug,
	}
	// First pass: collect package-level declarations (mocks)
	tr.collectTopLevel(root)
	if err := tr.semanticError(); err != nil {
		return "", err
	}
	// Second pass: emit the code
	output := tr.emit(root)

	// Inject all collected imports
	output = tr.injectImports(output)

	// Replace DMJLINE placeholders with real //line directives (must happen
	// after injectImports so go/format doesn't reposition them).
	if tr.emitLineDirectives {
		output = resolveLineDirectives(output)
	}

	return output, nil
}

// ---------------------------------------------------------------------------
// Transpiler state
// ---------------------------------------------------------------------------

type dmjTranspiler struct {
	src     []byte
	lang    *gotreesitter.Language
	testVar string // "t" for tests, "b" for benchmarks
	// Source file path for //line directives.
	sourceFile string
	// Whether to emit //line directives (true when sourceFile != "" && !Debug).
	emitLineDirectives bool
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
	// Whether we are emitting an eventually/consistently body where expect/reject
	// should be rendered as boolean conditions.
	inPollingBlock bool
	// Whether we are emitting a property block where expect/reject should be
	// rendered as boolean conditions.
	inPropertyBlock bool
	// Execution context for richer assertion messages.
	contextStack []string
	// Whether the Clock interface/fakeClock struct has been collected for package-level emission.
	fakeClockTypeCollected bool
	// Package-level type definitions for fake clock (emitted before test function).
	fakeClockTypeDecl string
	// One-time package-level helpers for polling assertions.
	pollingHelpersEmitted bool
	// Whether the syncBuffer type has been collected for package-level emission.
	syncBufferEmitted bool
	// Semantic diagnostics discovered during transpilation.
	semanticErrors []transpileDiagnostic
}

type transpileDiagnostic struct {
	line    int
	message string
	hint    string
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

func (t *dmjTranspiler) lineOf(n *gotreesitter.Node) int {
	return strings.Count(string(t.src[:n.StartByte()]), "\n") + 1
}

// lineDirective returns a placeholder marker for a //line directive. The actual
// //line directive is substituted after import injection (which uses go/format
// and would misplace //line comments). The placeholder is a valid Go comment so
// go/format preserves it verbatim.
func (t *dmjTranspiler) lineDirective(n *gotreesitter.Node) string {
	if !t.emitLineDirectives {
		return ""
	}
	return fmt.Sprintf("/*DMJLINE %s:%d*/\n", t.sourceFile, t.lineOf(n))
}

// resolveLineDirectives replaces all DMJLINE placeholder markers with real
// //line directives. Called after injectImports so go/format cannot reposition them.
func resolveLineDirectives(code string) string {
	re := regexp.MustCompile(`/\*DMJLINE ([^*]+)\*/`)
	return re.ReplaceAllString(code, "//line $1")
}

func (t *dmjTranspiler) assertContextString() string {
	if len(t.contextStack) == 0 {
		return ""
	}
	return strings.Join(t.contextStack, " > ")
}

func (t *dmjTranspiler) pushContext(context string) {
	t.contextStack = append(t.contextStack, context)
}

func (t *dmjTranspiler) popContext() {
	if len(t.contextStack) == 0 {
		return
	}
	t.contextStack = t.contextStack[:len(t.contextStack)-1]
}

func (t *dmjTranspiler) addSemanticError(n *gotreesitter.Node, message, hint string) {
	line := 1
	if n != nil {
		line = t.lineOf(n)
	}
	t.semanticErrors = append(t.semanticErrors, transpileDiagnostic{
		line:    line,
		message: message,
		hint:    hint,
	})
}

func (t *dmjTranspiler) semanticError() error {
	if len(t.semanticErrors) == 0 {
		return nil
	}

	var b strings.Builder
	for i, diag := range t.semanticErrors {
		if i > 0 {
			b.WriteString("\n")
		}
		location := fmt.Sprintf("danmuji:%d", diag.line)
		if t.sourceFile != "" {
			location = fmt.Sprintf("%s:%d", t.sourceFile, diag.line)
		}
		fmt.Fprintf(&b, "%s: %s", location, diag.message)
		if diag.hint != "" {
			fmt.Fprintf(&b, "\n   hint: %s", diag.hint)
		}
	}

	return fmt.Errorf("%s", b.String())
}

func (t *dmjTranspiler) expectFailureContext(prefix, rawText string, n *gotreesitter.Node) string {
	line := t.lineOf(n)
	context := t.assertContextString()
	trimmed := strings.TrimSpace(rawText)
	if context == "" {
		return strconv.Quote(fmt.Sprintf("danmuji:%d: %s (%s)", line, prefix, trimmed))
	}
	return strconv.Quote(fmt.Sprintf("danmuji:%d: %s | %s (%s)", line, context, prefix, trimmed))
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
		if t.childByField(n, "body") == nil {
			name := "spy"
			if nameNode := t.childByField(n, "name"); nameNode != nil {
				name = t.text(nameNode)
			}
			t.addSemanticError(n,
				fmt.Sprintf("spy %s must declare at least one method", name),
				fmt.Sprintf("spy %s { Publish(topic string) }", name))
			return
		}
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
	if (nt == "eventually_block" || nt == "consistently_block" || nt == "property_block") && !t.pollingHelpersEmitted {
		t.pollingHelpersEmitted = true
		t.addImport("reflect")
		t.addImport("strings")
		t.mockDecls = append(t.mockDecls, pollingAssertionHelpers)
	}
	if nt == "process_block" && !t.syncBufferEmitted {
		t.syncBufferEmitted = true
		t.addImport("sync")
		t.addImport("bytes")
		t.mockDecls = append(t.mockDecls, syncBufferHelper)
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
		if t.inExecBlock {
			return t.emitExecExpect(n)
		}
		return t.emitExpect(n)
	case "reject_statement":
		if t.inExecBlock {
			return t.emitExecReject(n)
		}
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
	case "eventually_block":
		return t.emitEventually(n)
	case "consistently_block":
		return t.emitConsistently(n)
	case "property_block":
		return t.emitProperty(n)
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
	case "process_block":
		return t.emitProcess(n)
	case "stop_block":
		return t.emitStop(n)
	case "process_args":
		return "" // handled by emitProcess
	case "process_env":
		return "" // handled by emitProcess
	case "ready_clause":
		return "" // handled by emitProcess
	case "signal_directive":
		return "" // handled by emitStop (Task 5)
	case "timeout_directive":
		return "" // handled by emitStop (Task 5)
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
	return t.emitDefault(n)
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
	if tagsNode := t.childByField(n, "tags"); tagsNode != nil {
		for i := 0; i < int(tagsNode.NamedChildCount()); i++ {
			tc := tagsNode.NamedChild(i)
			if t.nodeType(tc) == "tag" {
				tags = append(tags, strings.TrimSpace(t.text(tc)))
			}
		}
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

	// Emit body block with inline directives from tags.
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if t.nodeType(c) == "block" {
			typeTagLines := t.emitTagDirectives(tags, t.testVar)
			b.WriteString("{\n")
			b.WriteString(t.lineDirective(n))
			if len(typeTagLines) > 0 {
				b.WriteString(typeTagLines)
			}
			b.WriteString(t.emitBlockInner(c, "\t"))
			b.WriteString("}")
			break
		}
	}
	b.WriteString("\n")

	return b.String()
}

func (t *dmjTranspiler) emitTagDirectives(tags []string, testVar string) string {
	var b strings.Builder
	seen := make(map[string]bool)
	for _, tag := range tags {
		label := strings.TrimSpace(strings.TrimPrefix(tag, "@"))
		if seen[label] {
			continue
		}
		seen[label] = true

		switch label {
		case "skip":
			fmt.Fprintf(&b, "\t%s.Skip(\"skipping @skip\")\n", testVar)
		case "slow":
			t.addImport("testing")
			fmt.Fprintf(&b, "\tif testing.Short() {\n\t\t%s.Skip(\"skipping @slow in short mode\")\n\t}\n", testVar)
		case "parallel":
			fmt.Fprintf(&b, "\t%s.Parallel()\n", testVar)
		}
	}

	return b.String()
}
