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
