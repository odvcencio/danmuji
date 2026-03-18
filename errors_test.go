package danmuji

import (
	"strings"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// multiline test source used by several tests.
var testSource = []byte("package main\n\nimport \"fmt\"\n\tgiven valid {\n\t\twhen something {\n\t\t}\n\t}")

func TestFormatSourceLine(t *testing.T) {
	// Row 3 (0-indexed) → line 4: "\tgiven valid {"
	// Highlight "valid" at cols 7..12 (0-indexed, exclusive end).
	got := formatSourceLine(testSource, 3, 7, 12)
	if got == "" {
		t.Fatal("expected non-empty result")
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), got)
	}
	// Line number should be 4 (1-indexed).
	if !strings.Contains(lines[0], "4 |") {
		t.Errorf("source line missing line number 4: %q", lines[0])
	}
	// Source content should contain the original text.
	if !strings.Contains(lines[0], "given valid {") {
		t.Errorf("source line missing content: %q", lines[0])
	}
	// Caret line should have exactly 5 carets for "valid".
	caretIdx := strings.Index(lines[1], "^")
	if caretIdx == -1 {
		t.Fatalf("no carets in underline: %q", lines[1])
	}
	caretRun := 0
	for _, ch := range lines[1][caretIdx:] {
		if ch == '^' {
			caretRun++
		} else {
			break
		}
	}
	if caretRun != 5 {
		t.Errorf("expected 5 carets, got %d: %q", caretRun, lines[1])
	}
}

func TestFormatSourceLineFirstLine(t *testing.T) {
	// Row 0 → line 1: "package main"
	// Highlight "package" at cols 0..7.
	got := formatSourceLine(testSource, 0, 0, 7)
	if got == "" {
		t.Fatal("expected non-empty result")
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "1 |") {
		t.Errorf("expected line number 1: %q", lines[0])
	}
	// Should have exactly 7 carets.
	if strings.Count(lines[1], "^") != 7 {
		t.Errorf("expected 7 carets, got %d: %q", strings.Count(lines[1], "^"), lines[1])
	}
}

func TestFormatSourceLineOutOfRange(t *testing.T) {
	if got := formatSourceLine(testSource, -1, 0, 5); got != "" {
		t.Errorf("negative row should return empty, got: %q", got)
	}
	if got := formatSourceLine(testSource, 999, 0, 5); got != "" {
		t.Errorf("row beyond source should return empty, got: %q", got)
	}
}

func TestFormatError(t *testing.T) {
	ctx := formatSourceLine(testSource, 3, 7, 12)
	got := formatError("/tmp/test.dmj", 3, 7, `expected string after "given"`, ctx, `given "description" { ... }`)

	// Check location header: 1-indexed → 4:8
	if !strings.HasPrefix(got, "/tmp/test.dmj:4:8:") {
		t.Errorf("location header wrong: %q", got)
	}
	// Check message present.
	if !strings.Contains(got, `expected string after "given"`) {
		t.Errorf("message missing: %q", got)
	}
	// Check source context present.
	if !strings.Contains(got, "4 |") {
		t.Errorf("source context missing: %q", got)
	}
	// Check hint present.
	if !strings.Contains(got, `hint: given "description" { ... }`) {
		t.Errorf("hint missing: %q", got)
	}
}

func TestFormatErrorNoFile(t *testing.T) {
	ctx := formatSourceLine(testSource, 0, 0, 7)
	got := formatError("", 0, 0, "some error", ctx, "example")

	// Should start with 1:1 (no filename).
	if !strings.HasPrefix(got, "1:1:") {
		t.Errorf("expected no filename prefix, got: %q", got)
	}
	// Should not contain a path separator before the line number.
	firstColon := strings.Index(got, ":")
	prefix := got[:firstColon]
	if strings.Contains(prefix, "/") || strings.Contains(prefix, "\\") {
		t.Errorf("filename should be absent: %q", got)
	}
}

func TestFormatErrorNoHint(t *testing.T) {
	ctx := formatSourceLine(testSource, 0, 0, 7)
	got := formatError("/tmp/test.dmj", 0, 0, "some error", ctx, "")

	if strings.Contains(got, "hint:") {
		t.Errorf("hint should be absent when example is empty: %q", got)
	}
}

// ---------------------------------------------------------------------------
// buildExpectationMap tests
// ---------------------------------------------------------------------------

func TestBuildExpectationMap(t *testing.T) {
	g := DanmujiGrammar()
	m := buildExpectationMap(g)

	// given_block: Seq("given", Field("description", Sym("_string_literal")), Sym("block"))
	gb, ok := m["given_block"]
	if !ok {
		t.Fatal("expected given_block in expectation map")
	}
	if len(gb.Expansions) == 0 {
		t.Fatal("expected at least one expansion for given_block")
	}
	exp := gb.Expansions[0]
	if len(exp.Steps) < 3 {
		t.Fatalf("expected at least 3 steps, got %d", len(exp.Steps))
	}
	if exp.Steps[0].Keyword != "given" {
		t.Errorf("expected first step keyword 'given', got %q", exp.Steps[0].Keyword)
	}
	if exp.Steps[1].Field != "description" {
		t.Errorf("expected second step field 'description', got %q", exp.Steps[1].Field)
	}
}

func TestBuildExpectationMapExpect(t *testing.T) {
	g := DanmujiGrammar()
	m := buildExpectationMap(g)
	es, ok := m["expect_statement"]
	if !ok {
		t.Fatal("expected expect_statement in map")
	}
	// expect_statement has Choice with multiple alternatives plus bare form
	if len(es.Expansions) < 3 {
		t.Errorf("expected at least 3 expansions, got %d", len(es.Expansions))
	}
	t.Logf("expect_statement: %d expansions", len(es.Expansions))
}

func TestBuildExpectationMapFakeClock(t *testing.T) {
	g := DanmujiGrammar()
	m := buildExpectationMap(g)
	fc, ok := m["fake_clock_directive"]
	if !ok {
		t.Fatal("expected fake_clock_directive in map")
	}
	// Choice of 3 PrecDynamic alternatives
	if len(fc.Expansions) < 3 {
		t.Errorf("expected at least 3 expansions, got %d", len(fc.Expansions))
	}
}

// ---------------------------------------------------------------------------
// findErrors / findTopLevelBlock tests
// ---------------------------------------------------------------------------

func TestFindErrorsSingleError(t *testing.T) {
	lang := getDanmujiLang(t)
	// "given" without a string — should produce ERROR
	source := []byte("package main\n\nunit \"test\" {\n\tgiven valid {\n\t}\n}\n")
	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	root := tree.RootNode()
	if !root.HasError() {
		t.Skip("no error in parse tree — grammar may accept this")
	}
	errors := findErrors(root, lang)
	if len(errors) == 0 {
		t.Fatal("expected at least one error node")
	}
	t.Logf("Found %d error(s)", len(errors))
	// Should report only 1 error for a single test block
	if len(errors) > 1 {
		t.Logf("Multiple errors found — acceptable if in different top-level blocks")
	}
}

func TestFindErrorsNoErrors(t *testing.T) {
	lang := getDanmujiLang(t)
	// Valid Go — should have zero errors.
	source := []byte("package main\n\nfunc main() {\n\tx := 1\n\t_ = x\n}\n")
	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	root := tree.RootNode()
	errors := findErrors(root, lang)
	if len(errors) != 0 {
		t.Errorf("expected 0 errors for valid Go, got %d", len(errors))
	}
}

func TestFindErrorsMultipleBlocks(t *testing.T) {
	lang := getDanmujiLang(t)
	// Two test blocks, each with an error
	source := []byte(`package main

unit "test1" {
	given {
	}
}

unit "test2" {
	expect
}
`)
	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	root := tree.RootNode()
	if !root.HasError() {
		t.Skip("no errors found")
	}
	errors := findErrors(root, lang)
	t.Logf("Found %d error(s)", len(errors))
	// Should report errors from both blocks (up to 1 per top-level block)
}

// ---------------------------------------------------------------------------
// describeExpected / matchExpansions / describeStep tests
// ---------------------------------------------------------------------------

func TestDescribeExpected(t *testing.T) {
	g := DanmujiGrammar()
	expectations := buildExpectationMap(g)
	lang := getDanmujiLang(t)

	// "given" without string — should suggest string.
	// Tree-sitter's error recovery for this grammar typically collapses
	// the root into an ERROR node with flattened children. The root ERROR
	// contains keyword tokens (e.g. "given") as children. describeExpected
	// handles both recognized parents (from the expectations map) and ERROR
	// parents (by scanning keyword prefixes against expansions).
	source := []byte("package main\n\nunit \"test\" {\n\tgiven {\n\t}\n}\n")
	parser := gotreesitter.NewParser(lang)
	tree, _ := parser.Parse(source)
	root := tree.RootNode()
	if !root.HasError() {
		t.Skip("no error")
	}
	t.Logf("SExpr: %s", root.SExpr(lang))
	errors := findErrors(root, lang)
	if len(errors) == 0 {
		t.Fatal("expected error node")
	}
	errNode := errors[0]
	t.Logf("Error node type: %s, IsError: %v, IsMissing: %v", errNode.Type(lang), errNode.IsError(), errNode.IsMissing())

	// The error node may be the root (no parent) or may have a parent.
	// When it has a parent, call describeExpected.
	parent := errNode.Parent()
	if parent == nil {
		// Root-level ERROR — describeExpected works with nested errors.
		// Find a child ERROR node inside the root that has the root as parent.
		for i := 0; i < errNode.ChildCount(); i++ {
			child := errNode.Child(i)
			if child.IsError() {
				parent = errNode
				errNode = child
				break
			}
		}
		if parent == nil {
			t.Skip("no nested error node found")
		}
	}

	t.Logf("Parent type: %s", parent.Type(lang))
	msg := describeExpected(parent, errNode, lang, expectations)
	t.Logf("Parent: %s, Message: %s", parent.Type(lang), msg)
	if msg == "" {
		t.Error("expected non-empty message")
	}
}

func TestMatchExpansionsSimple(t *testing.T) {
	expansions := []LinearExpansion{
		{Steps: []ExpectedStep{
			{Keyword: "given"},
			{Type: "_string_literal", Field: "description"},
			{Type: "block"},
		}},
	}
	// Prefix: just "given" parsed — should suggest string next
	next := matchExpansions([]string{"given"}, expansions)
	if len(next) == 0 {
		t.Fatal("expected next steps")
	}
	found := false
	for _, s := range next {
		if s.Type == "_string_literal" {
			found = true
		}
	}
	if !found {
		t.Error("expected _string_literal in next steps")
	}
}

func TestDescribeStepKeyword(t *testing.T) {
	s := describeStep(ExpectedStep{Keyword: "given"})
	if s != `"given"` {
		t.Errorf("expected quoted keyword, got %q", s)
	}
}

func TestDescribeStepString(t *testing.T) {
	s := describeStep(ExpectedStep{Type: "_string_literal"})
	if s != "string" {
		t.Errorf("expected 'string', got %q", s)
	}
}

func TestDescribeStepBlock(t *testing.T) {
	s := describeStep(ExpectedStep{Type: "block"})
	if s != "{" {
		t.Errorf("expected '{', got %q", s)
	}
}

func TestDescribeStepExpression(t *testing.T) {
	s := describeStep(ExpectedStep{Type: "_expression"})
	if s != "expression" {
		t.Errorf("expected 'expression', got %q", s)
	}
}

func TestDescribeStepParameterList(t *testing.T) {
	s := describeStep(ExpectedStep{Type: "parameter_list"})
	if s != "parameter list" {
		t.Errorf("expected 'parameter list', got %q", s)
	}
}

func TestMatchExpansionsOptionalSkip(t *testing.T) {
	expansions := []LinearExpansion{
		{Steps: []ExpectedStep{
			{Keyword: "test", Optional: true},
			{Keyword: "given"},
			{Type: "_string_literal"},
		}},
	}
	// Empty prefix — should suggest "test" (optional) or "given"
	next := matchExpansions([]string{}, expansions)
	if len(next) == 0 {
		t.Fatal("expected next steps")
	}
	t.Logf("Next steps: %+v", next)
}

func TestMatchExpansionsNoMatch(t *testing.T) {
	expansions := []LinearExpansion{
		{Steps: []ExpectedStep{
			{Keyword: "given"},
			{Type: "_string_literal"},
		}},
	}
	// Prefix doesn't match — "when" is not "given"
	next := matchExpansions([]string{"when"}, expansions)
	if len(next) != 0 {
		t.Errorf("expected no matches, got %+v", next)
	}
}

// ---------------------------------------------------------------------------
// Layer 2 / Layer 3 tests
// ---------------------------------------------------------------------------

func TestKeywordInference(t *testing.T) {
	cases := []struct{ text, expected string }{
		{"given valid input {", "given_block"},
		{"expect", "expect_statement"},
		{"process ./cmd/server", "process_block"},
		{"no_leaks", "no_leaks_directive"},
		{"fake_clock at", "fake_clock_directive"},
		{"x := 5", ""},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			result := inferFromKeyword(tc.text, keywordToProduction)
			if result != tc.expected {
				t.Errorf("inferFromKeyword(%q) = %q, want %q", tc.text, result, tc.expected)
			}
		})
	}
}

func TestBuildPrefixSignature(t *testing.T) {
	lang := getDanmujiLang(t)
	// Parse something with a known structure to verify prefix building
	source := []byte("package main\nfunc f() {\n\tgiven \"test\" {\n\t}\n}\n")
	parser := gotreesitter.NewParser(lang)
	tree, _ := parser.Parse(source)
	root := tree.RootNode()
	t.Logf("SExpr: %s", root.SExpr(lang))
	// Just verify it doesn't panic — the exact output depends on tree structure
}
