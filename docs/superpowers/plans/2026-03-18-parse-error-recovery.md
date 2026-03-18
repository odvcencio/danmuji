# Parse Error Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace raw s-expression error dumps with human-readable error messages showing file, line, source context, and hints about what was expected.

**Architecture:** Three-layer error reporting: (1) grammar-derived expectations via linear expansion enumeration, (2) hand-written overlays for common mistakes, (3) keyword-based inference when the parser doesn't create the expected parent. New file `errors.go` with cached expectation map built from `DanmujiGrammar()`.

**Tech Stack:** Go 1.24, gotreesitter (grammargen Rule introspection, Node tree walking)

**Spec:** `docs/superpowers/specs/2026-03-18-parse-error-recovery-design.md`

---

## File Structure

| File | Role | Change |
|------|------|--------|
| `errors.go` | Error formatting (new) | `FormatParseError`, `buildExpectationMap`, `findErrors`, `matchPrefix`, `inferFromKeyword`, overlays, source line rendering |
| `errors_test.go` | Error tests (new) | Overlay tests, fallback tests, keyword inference tests, multi-error tests, edge cases |
| `transpile.go` | Integration | Cache expectation map in `getDanmujiLanguage`, replace `HasError` path with `FormatParseError` |

---

### Task 1: Source line rendering and error formatting

Build the output formatting functions first — everything else builds on these.

**Files:**
- Create: `errors.go`
- Create: `errors_test.go`

- [ ] **Step 1: Write failing test for source line rendering**

Add to `errors_test.go`:

```go
package danmuji

import (
	"strings"
	"testing"
)

func TestFormatSourceLine(t *testing.T) {
	source := []byte("package main\n\nunit \"test\" {\n\tgiven valid {\n\t}\n}\n")
	// Line 4 (0-indexed row 3): "\tgiven valid {"
	// Error at columns 7-12 (0-indexed): "valid"
	result := formatSourceLine(source, 3, 7, 12)
	t.Logf("Result:\n%s", result)
	if !strings.Contains(result, "4 |") {
		t.Error("expected 1-indexed line number 4")
	}
	if !strings.Contains(result, "given valid {") {
		t.Error("expected source line content")
	}
	if !strings.Contains(result, "^^^^^") {
		t.Error("expected caret underline")
	}
}

func TestFormatSourceLineFirstLine(t *testing.T) {
	source := []byte("packge main\n")
	result := formatSourceLine(source, 0, 0, 6)
	if !strings.Contains(result, "1 |") {
		t.Error("expected line 1")
	}
	if !strings.Contains(result, "^^^^^^") {
		t.Error("expected 6-char underline")
	}
}

func TestFormatError(t *testing.T) {
	result := formatError("/tmp/test.dmj", 3, 7, "expected string after \"given\"", "    4 | \tgiven valid {\n      | \t       ^^^^^", `given "description" { ... }`)
	if !strings.Contains(result, "/tmp/test.dmj:4:8") {
		t.Error("expected 1-indexed location")
	}
	if !strings.Contains(result, "expected string") {
		t.Error("expected message")
	}
	if !strings.Contains(result, "hint:") {
		t.Error("expected hint")
	}
}

func TestFormatErrorNoFile(t *testing.T) {
	result := formatError("", 3, 7, "unexpected token", "    4 | \tgiven valid {\n      | \t       ^^^^^", "")
	if !strings.Contains(result, "4:8:") {
		t.Error("expected line:col without filename")
	}
	if strings.Contains(result, "hint:") {
		t.Error("expected no hint when example is empty")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run "TestFormatSource|TestFormatError" -v ./...`
Expected: FAIL — functions not defined.

- [ ] **Step 3: Implement formatting functions**

Create `errors.go`:

```go
package danmuji

import (
	"fmt"
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// formatSourceLine renders a source line with a caret underline at the error span.
// row, startCol, endCol are 0-indexed. Output uses 1-indexed line numbers.
func formatSourceLine(source []byte, row, startCol, endCol int) string {
	lines := strings.Split(string(source), "\n")
	if row < 0 || row >= len(lines) {
		return ""
	}
	line := lines[row]
	lineNum := row + 1

	// Build the display
	var b strings.Builder
	prefix := fmt.Sprintf("  %4d | ", lineNum)
	fmt.Fprintf(&b, "%s%s\n", prefix, line)

	// Caret line: spaces up to startCol, then carets
	padding := strings.Repeat(" ", len(prefix))
	if startCol > len(line) {
		startCol = len(line)
	}
	if endCol > len(line) {
		endCol = len(line)
	}
	if endCol <= startCol {
		endCol = startCol + 1
	}
	caretPad := ""
	if startCol > 0 {
		// Preserve tabs for alignment
		leadingPart := line[:startCol]
		caretPad = strings.Map(func(r rune) rune {
			if r == '\t' {
				return '\t'
			}
			return ' '
		}, leadingPart)
	}
	carets := strings.Repeat("^", endCol-startCol)
	fmt.Fprintf(&b, "%s%s%s", padding, caretPad, carets)

	return b.String()
}

// formatError assembles the final error string.
// row and col are 0-indexed. Output uses 1-indexed.
func formatError(sourceFile string, row, col int, message, sourceContext, example string) string {
	var b strings.Builder

	// Location
	lineNum := row + 1
	colNum := col + 1
	if sourceFile != "" {
		fmt.Fprintf(&b, "%s:%d:%d: %s\n", sourceFile, lineNum, colNum, message)
	} else {
		fmt.Fprintf(&b, "%d:%d: %s\n", lineNum, colNum, message)
	}

	// Source context
	if sourceContext != "" {
		fmt.Fprintf(&b, "%s\n", sourceContext)
	}

	// Hint
	if example != "" {
		fmt.Fprintf(&b, "   hint: %s", example)
	}

	return b.String()
}

// FormatParseError formats parse errors from a tree with ERROR/MISSING nodes.
// This is a placeholder that will be expanded in subsequent tasks.
func FormatParseError(source []byte, root *gotreesitter.Node, lang *gotreesitter.Language,
	sourceFile string, expectations map[string]*ProductionExpectations) string {
	return "parse error (formatting not yet implemented)"
}

// ProductionExpectations holds all valid linear expansions for a grammar production.
type ProductionExpectations struct {
	NodeType   string
	Expansions []LinearExpansion
}

// LinearExpansion is one valid sequence of expected children for a production.
type LinearExpansion struct {
	Steps []ExpectedStep
}

// ExpectedStep is one expected element in a linear expansion.
type ExpectedStep struct {
	Type     string // "string_literal", "block", "identifier", etc.
	Keyword  string // if Str("given"), the literal text
	Field    string // if wrapped in Field("name", ...), the field name
	Optional bool   // if this step can be skipped
}
```

- [ ] **Step 4: Run tests**

Run: `go test -run "TestFormatSource|TestFormatError" -v ./...`
Expected: PASS

- [ ] **Step 5: Commit**

`buckley commit --yes -min`

---

### Task 2: Error discovery — finding ERROR and MISSING nodes

Walk the parse tree to collect error nodes, deduplicating per top-level block.

**Files:**
- Modify: `errors.go`
- Modify: `errors_test.go`

- [ ] **Step 1: Write failing tests for error discovery**

Add to `errors_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run "TestFindErrors" -v ./...`
Expected: FAIL — `findErrors` not implemented (currently a placeholder).

- [ ] **Step 3: Implement `findErrors`**

In `errors.go`, implement:

```go
// findErrors walks the tree and collects ERROR/MISSING nodes.
// Returns at most one error per top-level block (direct child of source_file
// that is a test_block, benchmark_block, or load_block).
// For errors outside top-level blocks, returns the first error only.
func findErrors(root *gotreesitter.Node, lang *gotreesitter.Language) []*gotreesitter.Node {
	var errors []*gotreesitter.Node
	seenTopLevel := make(map[uint32]bool) // start byte of top-level block -> already reported

	gotreesitter.Walk(root, func(n *gotreesitter.Node, depth int) gotreesitter.WalkAction {
		if !n.IsError() && !n.IsMissing() {
			if n.HasError() {
				return gotreesitter.WalkContinue
			}
			return gotreesitter.WalkSkipChildren
		}

		// Found an error node. Determine its top-level block.
		topLevel := findTopLevelBlock(n, lang)
		if topLevel != nil {
			key := topLevel.StartByte()
			if seenTopLevel[key] {
				return gotreesitter.WalkSkipChildren
			}
			seenTopLevel[key] = true
		} else {
			// Error outside a top-level block — only report if first
			if len(errors) > 0 {
				// Check if we already have a non-top-level error
				hasNonTopLevel := false
				for _, e := range errors {
					if findTopLevelBlock(e, lang) == nil {
						hasNonTopLevel = true
						break
					}
				}
				if hasNonTopLevel {
					return gotreesitter.WalkSkipChildren
				}
			}
		}

		errors = append(errors, n)
		return gotreesitter.WalkSkipChildren
	})

	return errors
}

// findTopLevelBlock walks up from a node to find the enclosing test_block,
// benchmark_block, or load_block that is a direct child of source_file.
func findTopLevelBlock(n *gotreesitter.Node, lang *gotreesitter.Language) *gotreesitter.Node {
	for p := n.Parent(); p != nil; p = p.Parent() {
		parentType := p.Type(lang)
		if parentType == "source_file" {
			// n's ancestor just below source_file
			return nil
		}
		grandparent := p.Parent()
		if grandparent != nil && grandparent.Type(lang) == "source_file" {
			switch p.Type(lang) {
			case "test_block", "benchmark_block", "load_block":
				return p
			}
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test -run "TestFindErrors" -v ./...`
Expected: PASS

- [ ] **Step 5: Commit**

`buckley commit --yes -min`

---

### Task 3: Grammar introspection — building the expectation map

Walk `DanmujiGrammar()` rules to enumerate all valid linear expansions per production.

**Files:**
- Modify: `errors.go`
- Modify: `errors_test.go`

- [ ] **Step 1: Write failing tests for expectation map**

Add to `errors_test.go`:

```go
func TestBuildExpectationMap(t *testing.T) {
	g := DanmujiGrammar()
	m := buildExpectationMap(g)

	// given_block should have expansions
	gb, ok := m["given_block"]
	if !ok {
		t.Fatal("expected given_block in expectation map")
	}
	if len(gb.Expansions) == 0 {
		t.Fatal("expected at least one expansion for given_block")
	}
	// First expansion should be: Str("given"), Field("description", Sym("_string_literal")), Sym("block")
	exp := gb.Expansions[0]
	if len(exp.Steps) < 3 {
		t.Fatalf("expected at least 3 steps in given_block expansion, got %d", len(exp.Steps))
	}
	if exp.Steps[0].Keyword != "given" {
		t.Errorf("expected first step keyword 'given', got %q", exp.Steps[0].Keyword)
	}
	if exp.Steps[1].Field != "description" {
		t.Errorf("expected second step field 'description', got %q", exp.Steps[1].Field)
	}
	t.Logf("given_block expansions: %d", len(gb.Expansions))
	for i, e := range gb.Expansions {
		t.Logf("  expansion %d: %d steps", i, len(e.Steps))
		for j, s := range e.Steps {
			t.Logf("    step %d: type=%q keyword=%q field=%q optional=%v", j, s.Type, s.Keyword, s.Field, s.Optional)
		}
	}
}

func TestBuildExpectationMapExpect(t *testing.T) {
	g := DanmujiGrammar()
	m := buildExpectationMap(g)

	es, ok := m["expect_statement"]
	if !ok {
		t.Fatal("expected expect_statement in map")
	}
	// expect_statement has multiple alternatives (==, !=, contains, is_nil, not_nil, matcher)
	// Plus bare expect (no operator). Should have multiple expansions.
	if len(es.Expansions) < 3 {
		t.Errorf("expected at least 3 expansions for expect_statement, got %d", len(es.Expansions))
	}
	t.Logf("expect_statement expansions: %d", len(es.Expansions))
}

func TestBuildExpectationMapFakeClock(t *testing.T) {
	g := DanmujiGrammar()
	m := buildExpectationMap(g)

	fc, ok := m["fake_clock_directive"]
	if !ok {
		t.Fatal("expected fake_clock_directive in map")
	}
	// fake_clock_directive is Choice of 3 PrecDynamic alternatives
	if len(fc.Expansions) != 3 {
		t.Errorf("expected 3 expansions for fake_clock_directive, got %d", len(fc.Expansions))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run "TestBuildExpectationMap" -v ./...`
Expected: FAIL — `buildExpectationMap` not implemented.

- [ ] **Step 3: Implement `buildExpectationMap`**

In `errors.go`, implement the grammar rule walker:

```go
// buildExpectationMap walks all grammar rules and enumerates linear expansions.
func buildExpectationMap(g *Grammar) map[string]*ProductionExpectations {
	m := make(map[string]*ProductionExpectations)
	for name, rule := range g.Rules {
		// Skip internal/inherited rules that start with "_"
		expansions := expandRule(rule, "")
		if len(expansions) > 0 {
			m[name] = &ProductionExpectations{
				NodeType:   name,
				Expansions: expansions,
			}
		}
	}
	return m
}

// expandRule recursively flattens a rule tree into all possible linear sequences.
// fieldName is propagated from parent Field() wrappers.
func expandRule(r *Rule, fieldName string) []LinearExpansion {
	if r == nil {
		return []LinearExpansion{{}} // one empty expansion
	}

	switch r.Kind {
	case RuleString:
		step := ExpectedStep{Type: "string", Keyword: r.Value, Field: fieldName}
		return []LinearExpansion{{Steps: []ExpectedStep{step}}}

	case RulePattern:
		step := ExpectedStep{Type: "pattern:" + r.Value, Field: fieldName}
		return []LinearExpansion{{Steps: []ExpectedStep{step}}}

	case RuleSymbol:
		step := ExpectedStep{Type: r.Value, Field: fieldName}
		return []LinearExpansion{{Steps: []ExpectedStep{step}}}

	case RuleBlank:
		return []LinearExpansion{{}} // empty

	case RuleSeq:
		// Cartesian product of all children's expansions
		result := []LinearExpansion{{}}
		for _, child := range r.Children {
			childExps := expandRule(child, "")
			var newResult []LinearExpansion
			for _, prefix := range result {
				for _, suffix := range childExps {
					combined := LinearExpansion{
						Steps: make([]ExpectedStep, 0, len(prefix.Steps)+len(suffix.Steps)),
					}
					combined.Steps = append(combined.Steps, prefix.Steps...)
					combined.Steps = append(combined.Steps, suffix.Steps...)
					newResult = append(newResult, combined)
				}
			}
			result = newResult
			// Cap to prevent explosion from deeply nested choices
			if len(result) > 100 {
				result = result[:100]
			}
		}
		return result

	case RuleChoice:
		// Union of all alternatives
		var result []LinearExpansion
		for _, child := range r.Children {
			result = append(result, expandRule(child, "")...)
		}
		if len(result) > 100 {
			result = result[:100]
		}
		return result

	case RuleOptional:
		if len(r.Children) == 0 {
			return []LinearExpansion{{}}
		}
		childExps := expandRule(r.Children[0], fieldName)
		// Mark all steps as optional
		for i := range childExps {
			for j := range childExps[i].Steps {
				childExps[i].Steps[j].Optional = true
			}
		}
		// Add the empty expansion (the "skip" case)
		return append([]LinearExpansion{{}}, childExps...)

	case RuleRepeat:
		if len(r.Children) == 0 {
			return []LinearExpansion{{}}
		}
		// 0 or 1 occurrence for error reporting purposes
		childExps := expandRule(r.Children[0], fieldName)
		for i := range childExps {
			for j := range childExps[i].Steps {
				childExps[i].Steps[j].Optional = true
			}
		}
		return append([]LinearExpansion{{}}, childExps...)

	case RuleRepeat1:
		if len(r.Children) == 0 {
			return []LinearExpansion{{}}
		}
		return expandRule(r.Children[0], fieldName)

	case RuleField:
		if len(r.Children) == 0 {
			return []LinearExpansion{{}}
		}
		return expandRule(r.Children[0], r.Value)

	case RulePrec, RulePrecLeft, RulePrecRight, RulePrecDynamic:
		if len(r.Children) == 0 {
			return []LinearExpansion{{}}
		}
		return expandRule(r.Children[0], fieldName)

	case RuleToken, RuleImmToken:
		// Tokens are atomic — treat as a single step
		step := ExpectedStep{Type: "token", Field: fieldName}
		return []LinearExpansion{{Steps: []ExpectedStep{step}}}

	case RuleAlias:
		if len(r.Children) == 0 {
			return []LinearExpansion{{}}
		}
		return expandRule(r.Children[0], fieldName)

	default:
		return []LinearExpansion{{}}
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test -run "TestBuildExpectationMap" -v ./...`
Expected: PASS

- [ ] **Step 5: Commit**

`buckley commit --yes -min`

---

### Task 4: Prefix matching and Layer 1 message generation

Match parsed children against expansions to determine what was expected at the error position.

**Files:**
- Modify: `errors.go`
- Modify: `errors_test.go`

- [ ] **Step 1: Write failing tests**

Add to `errors_test.go`:

```go
func TestDescribeExpected(t *testing.T) {
	g := DanmujiGrammar()
	expectations := buildExpectationMap(g)
	lang := getDanmujiLang(t)

	// "given" keyword present, missing string — should suggest string_literal
	source := []byte("package main\nfunc f() {\n\tgiven {\n\t}\n}\n")
	parser := gotreesitter.NewParser(lang)
	tree, _ := parser.Parse(source)
	root := tree.RootNode()
	if !root.HasError() {
		t.Skip("no error — grammar may accept this")
	}

	errors := findErrors(root, lang)
	if len(errors) == 0 {
		t.Fatal("expected error node")
	}

	errNode := errors[0]
	parent := errNode.Parent()
	if parent == nil {
		t.Fatal("error node has no parent")
	}

	msg := describeExpected(parent, errNode, lang, expectations)
	t.Logf("Message: %s", msg)
	if msg == "" {
		t.Error("expected non-empty message")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run "TestDescribeExpected" -v ./...`
Expected: FAIL — `describeExpected` not defined.

- [ ] **Step 3: Implement `describeExpected`**

In `errors.go`:

```go
// describeExpected determines what was expected at the error position
// by matching the parent's successfully-parsed children against the production's expansions.
func describeExpected(parent, errNode *gotreesitter.Node, lang *gotreesitter.Language,
	expectations map[string]*ProductionExpectations) string {

	parentType := parent.Type(lang)
	pe, ok := expectations[parentType]
	if !ok {
		return fmt.Sprintf("unexpected token in %s", parentType)
	}

	// Find the error node's index among siblings
	errIdx := -1
	for i := 0; i < parent.ChildCount(); i++ {
		if parent.Child(i).StartByte() == errNode.StartByte() &&
			parent.Child(i).EndByte() == errNode.EndByte() {
			errIdx = i
			break
		}
	}

	// Collect the types of successfully-parsed children before the error
	var parsedPrefix []string
	for i := 0; i < parent.ChildCount(); i++ {
		child := parent.Child(i)
		if child.StartByte() == errNode.StartByte() {
			break
		}
		if child.IsError() || child.IsMissing() {
			break
		}
		parsedPrefix = append(parsedPrefix, child.Type(lang))
	}

	// Match prefix against expansions to find what comes next
	expected := matchExpansions(parsedPrefix, pe.Expansions)
	if len(expected) == 0 {
		if errIdx == 0 {
			return fmt.Sprintf("unexpected token at start of %s", parentType)
		}
		return fmt.Sprintf("unexpected token in %s", parentType)
	}

	// Deduplicate and format
	seen := make(map[string]bool)
	var unique []string
	for _, e := range expected {
		desc := describeStep(e)
		if !seen[desc] {
			seen[desc] = true
			unique = append(unique, desc)
		}
	}

	if len(unique) == 1 {
		return fmt.Sprintf("expected %s", unique[0])
	}
	if len(unique) <= 4 {
		last := unique[len(unique)-1]
		rest := strings.Join(unique[:len(unique)-1], ", ")
		return fmt.Sprintf("expected %s, or %s", rest, last)
	}
	return fmt.Sprintf("expected one of: %s", strings.Join(unique[:4], ", "))
}

// matchExpansions returns the expected next steps given a prefix of parsed children.
func matchExpansions(prefix []string, expansions []LinearExpansion) []ExpectedStep {
	var nextSteps []ExpectedStep

	for _, exp := range expansions {
		pos := 0 // position in expansion steps
		matched := true
		for _, parsed := range prefix {
			// Advance through expansion steps to find one matching parsed
			found := false
			for pos < len(exp.Steps) {
				step := exp.Steps[pos]
				if stepMatches(step, parsed) {
					pos++
					found = true
					break
				}
				if step.Optional {
					pos++ // skip optional step
					continue
				}
				// Required step didn't match
				matched = false
				break
			}
			if !found {
				matched = false
				break
			}
		}
		if matched && pos < len(exp.Steps) {
			// Skip optional steps to find the next required step
			for pos < len(exp.Steps) {
				nextSteps = append(nextSteps, exp.Steps[pos])
				if !exp.Steps[pos].Optional {
					break
				}
				pos++
			}
		}
	}

	return nextSteps
}

// stepMatches checks if an expected step could match a parsed node type.
func stepMatches(step ExpectedStep, nodeType string) bool {
	if step.Keyword != "" {
		return nodeType == step.Keyword || nodeType == step.Type
	}
	// Symbol match: step.Type is a grammar symbol name
	return step.Type == nodeType || strings.HasSuffix(nodeType, step.Type)
}

// describeStep returns a human-readable description of an expected step.
func describeStep(step ExpectedStep) string {
	if step.Keyword != "" {
		return fmt.Sprintf(`"%s"`, step.Keyword)
	}
	switch step.Type {
	case "_string_literal", "interpreted_string_literal", "raw_string_literal":
		return "string"
	case "block":
		return "{"
	case "identifier":
		return "identifier"
	case "_expression":
		return "expression"
	case "_simple_type":
		return "type"
	case "parameter_list":
		return "parameter list"
	case "int_literal":
		return "integer"
	default:
		if step.Field != "" {
			return step.Field
		}
		return step.Type
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test -run "TestDescribeExpected" -v ./...`
Expected: PASS

- [ ] **Step 5: Commit**

`buckley commit --yes -min`

---

### Task 5: Layer 2 overlays and Layer 3 keyword inference

Add the hand-written overlay table and keyword-based fallback.

**Files:**
- Modify: `errors.go`
- Modify: `errors_test.go`

- [ ] **Step 1: Write failing tests**

Add to `errors_test.go`:

```go
func TestOverlayGivenMissingString(t *testing.T) {
	lang := getDanmujiLang(t)
	source := []byte("package main\n\nunit \"test\" {\n\tgiven {\n\t}\n}\n")
	parser := gotreesitter.NewParser(lang)
	tree, _ := parser.Parse(source)
	root := tree.RootNode()
	if !root.HasError() {
		t.Skip("no error")
	}
	g := DanmujiGrammar()
	expectations := buildExpectationMap(g)
	result := FormatParseError(source, root, lang, "/tmp/test.dmj", expectations)
	t.Logf("Error:\n%s", result)
	// Should contain friendly message about missing string
	if !strings.Contains(result, "/tmp/test.dmj:") {
		t.Error("expected filename in error")
	}
}

func TestKeywordInference(t *testing.T) {
	// Test that keyword inference works when ERROR node contains a danmuji keyword
	result := inferFromKeyword("given valid input {", keywordToProduction)
	if result != "given_block" {
		t.Errorf("expected given_block, got %q", result)
	}

	result = inferFromKeyword("expect", keywordToProduction)
	if result != "expect_statement" {
		t.Errorf("expected expect_statement, got %q", result)
	}

	result = inferFromKeyword("x := 5", keywordToProduction)
	if result != "" {
		t.Errorf("expected empty for Go code, got %q", result)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run "TestOverlay|TestKeywordInference" -v ./...`
Expected: FAIL

- [ ] **Step 3: Add overlay table and keyword inference**

In `errors.go`, add the overlay table:

```go
// ErrorOverlay provides a human-friendly error message for a specific error pattern.
type ErrorOverlay struct {
	Message string
	Example string
}

// errorOverlays maps "parent_type|prefix_signature" to friendly messages.
// The prefix signature is a compact representation of successfully-parsed children.
var errorOverlays = map[string]ErrorOverlay{
	// BDD structure
	"given_block|given":        {`expected string after "given"`, `given "description" { ... }`},
	"given_block|given,string": {`expected { to open block`, `given "description" { ... }`},
	"when_block|when":          {`expected string after "when"`, `when "description" { ... }`},
	"when_block|when,string":   {`expected { to open block`, `when "description" { ... }`},
	"then_block|then":          {`expected string after "then"`, `then "description" { ... }`},
	"then_block|then,string":   {`expected { to open block`, `then "description" { ... }`},

	// Test blocks
	"test_block|unit":          {`expected string after test category`, `unit "name" { ... }`},
	"test_block|unit,string":   {`expected { to open test block`, `unit "name" { ... }`},
	"test_block|integration":   {`expected string after test category`, `integration "name" { ... }`},
	"test_block|e2e":           {`expected string after test category`, `e2e "name" { ... }`},

	// Assertions
	"expect_statement|expect":           {`expected expression after "expect"`, `expect x == 1`},
	"reject_statement|reject":           {`expected expression after "reject"`, `reject ok`},
	"verify_statement|verify":           {`expected target expression after "verify"`, `verify repo.Save called 1 times`},

	// Test doubles
	"mock_declaration|mock":             {`expected name after "mock"`, `mock RepoName { ... }`},
	"mock_declaration|mock,identifier":  {`expected { to open mock body`, `mock RepoName { ... }`},
	"fake_declaration|fake":             {`expected name after "fake"`, `fake StoreName { ... }`},
	"spy_declaration|spy":               {`expected name after "spy"`, `spy BusName { ... }`},
	"mock_method|identifier,params":     {`expected -> return_type after parameters`, `Save(u User) -> error = nil`},

	// Data-driven
	"each_do_block|each":                {`expected string after "each"`, `each "scenarios" { ... } do { ... }`},
	"each_do_block|each,string,block":   {`expected "do" keyword`, `each "scenarios" { ... } do { ... }`},
	"matrix_block|matrix":               {`expected string after "matrix"`, `matrix "dimensions" { ... } do { ... }`},
	"property_block|property":           {`expected string after "property"`, `property "name" for all (x int) { ... }`},
	"property_block|property,string":    {`expected "for" keyword`, `property "name" for all (params) { ... }`},

	// Temporal
	"eventually_block|eventually":       {`expected string after "eventually"`, `eventually "name" within 5s { ... }`},
	"consistently_block|consistently":   {`expected string after "consistently"`, `consistently "name" for 2s { ... }`},

	// Infrastructure
	"needs_block|needs":                 {`expected service type after "needs"`, `needs postgres db { ... }`},
	"needs_block|needs,service_type":    {`expected identifier for service name`, `needs postgres db { ... }`},
	"benchmark_block|benchmark":         {`expected string after "benchmark"`, `benchmark "name" { ... }`},
	"exec_block|exec":                   {`expected string after "exec"`, `exec "name" { ... }`},
	"snapshot_block|snapshot":           {`expected string after "snapshot"`, `snapshot "name" { ... }`},

	// Process
	"process_block|process":             {`expected path after "process"`, `process "./cmd/server" { ... }`},
	"stop_block|stop":                   {`expected { to open stop block`, `stop { signal SIGTERM ... }`},
	"ready_clause|ready":                {`expected mode (http, tcp, stdout, delay) after "ready"`, `ready http "http://host/health"`},
}

// keywordToProduction maps danmuji keywords to their production names.
var keywordToProduction = map[string]string{
	"given":        "given_block",
	"when":         "when_block",
	"then":         "then_block",
	"expect":       "expect_statement",
	"reject":       "reject_statement",
	"verify":       "verify_statement",
	"mock":         "mock_declaration",
	"fake":         "fake_declaration",
	"spy":          "spy_declaration",
	"unit":         "test_block",
	"integration":  "test_block",
	"e2e":          "test_block",
	"benchmark":    "benchmark_block",
	"load":         "load_block",
	"needs":        "needs_block",
	"exec":         "exec_block",
	"process":      "process_block",
	"stop":         "stop_block",
	"eventually":   "eventually_block",
	"consistently": "consistently_block",
	"each":         "each_do_block",
	"matrix":       "matrix_block",
	"property":     "property_block",
	"snapshot":     "snapshot_block",
	"profile":      "profile_block",
	"table":        "table_declaration",
	"before":       "lifecycle_hook",
	"after":        "lifecycle_hook",
	"ready":        "ready_clause",
	"signal":       "signal_directive",
	"timeout":      "timeout_directive",
	"no_leaks":     "no_leaks_directive",
	"fake_clock":   "fake_clock_directive",
}

// inferFromKeyword looks at the text of an ERROR node and tries to determine
// which production the user was attempting, based on the first keyword.
func inferFromKeyword(text string, kwMap map[string]string) string {
	text = strings.TrimSpace(text)
	// Check for compound keywords first
	for _, compound := range []string{"no_leaks", "fake_clock"} {
		if strings.HasPrefix(text, compound) {
			if prod, ok := kwMap[compound]; ok {
				return prod
			}
		}
	}
	// Check first word
	firstWord := text
	if idx := strings.IndexAny(text, " \t\n{("); idx >= 0 {
		firstWord = text[:idx]
	}
	if prod, ok := kwMap[firstWord]; ok {
		return prod
	}
	return ""
}
```

- [ ] **Step 4: Run tests**

Run: `go test -run "TestOverlay|TestKeywordInference" -v ./...`
Expected: PASS

- [ ] **Step 5: Commit**

`buckley commit --yes -min`

---

### Task 6: Full `FormatParseError` implementation and integration

Wire everything together: the full error formatter and the integration into `TranspileDanmuji`.

**Files:**
- Modify: `errors.go`
- Modify: `errors_test.go`
- Modify: `transpile.go`

- [ ] **Step 1: Write failing integration test**

Add to `errors_test.go`:

```go
func TestFormatParseErrorIntegration(t *testing.T) {
	source := []byte(`package main_test

import "testing"

unit "broken" {
	given {
		x := 1
	}
}
`)
	g := DanmujiGrammar()
	expectations := buildExpectationMap(g)
	lang := getDanmujiLang(t)
	parser := gotreesitter.NewParser(lang)
	tree, _ := parser.Parse(source)
	root := tree.RootNode()
	if !root.HasError() {
		t.Skip("no error")
	}

	result := FormatParseError(source, root, lang, "/tmp/test.dmj", expectations)
	t.Logf("Error output:\n%s", result)

	if !strings.Contains(result, "/tmp/test.dmj:") {
		t.Error("expected filename in output")
	}
	if !strings.Contains(result, "^^^") || !strings.Contains(result, "|") {
		t.Error("expected source line with carets")
	}
}

func TestFormatParseErrorNoFile(t *testing.T) {
	source := []byte(`package main_test

import "testing"

unit "broken" {
	given {
	}
}
`)
	g := DanmujiGrammar()
	expectations := buildExpectationMap(g)
	lang := getDanmujiLang(t)
	parser := gotreesitter.NewParser(lang)
	tree, _ := parser.Parse(source)
	root := tree.RootNode()
	if !root.HasError() {
		t.Skip("no error")
	}

	result := FormatParseError(source, root, lang, "", expectations)
	t.Logf("Error output:\n%s", result)

	// Should not contain a filename
	if strings.Contains(result, ".dmj") {
		t.Error("expected no filename when sourceFile is empty")
	}
}

func TestFormatParseErrorEmptyFile(t *testing.T) {
	source := []byte("")
	g := DanmujiGrammar()
	expectations := buildExpectationMap(g)
	lang := getDanmujiLang(t)
	parser := gotreesitter.NewParser(lang)
	tree, _ := parser.Parse(source)
	root := tree.RootNode()

	// Empty file may or may not have errors depending on grammar
	if !root.HasError() {
		t.Skip("empty file parsed without error")
	}

	result := FormatParseError(source, root, lang, "", expectations)
	t.Logf("Error output:\n%s", result)
	if result == "" {
		t.Error("expected non-empty error for empty file")
	}
}

func TestTranspileDanmujiErrorFormat(t *testing.T) {
	source := []byte(`package main_test

import "testing"

unit "broken" {
	given {
	}
}
`)
	_, err := TranspileDanmuji(source, TranspileOptions{SourceFile: "/tmp/test.dmj"})
	if err == nil {
		t.Skip("transpile succeeded — grammar may accept this")
	}
	errMsg := err.Error()
	t.Logf("Error: %s", errMsg)

	// Should NOT contain raw s-expression
	if strings.Contains(errMsg, "(source_file") {
		t.Error("expected human-readable error, got s-expression")
	}
	// Should contain file reference
	if !strings.Contains(errMsg, "/tmp/test.dmj:") {
		t.Error("expected filename in error")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run "TestFormatParseError|TestTranspileDanmujiErrorFormat" -v ./...`
Expected: FAIL — `FormatParseError` is a placeholder.

- [ ] **Step 3: Implement full `FormatParseError`**

Replace the placeholder in `errors.go`:

```go
// FormatParseError formats parse errors from a tree with ERROR/MISSING nodes.
func FormatParseError(source []byte, root *gotreesitter.Node, lang *gotreesitter.Language,
	sourceFile string, expectations map[string]*ProductionExpectations) string {

	errors := findErrors(root, lang)
	if len(errors) == 0 {
		return "unknown parse error"
	}

	var parts []string
	for _, errNode := range errors {
		parts = append(parts, formatSingleError(source, errNode, lang, sourceFile, expectations))
	}
	return strings.Join(parts, "\n\n")
}

func formatSingleError(source []byte, errNode *gotreesitter.Node, lang *gotreesitter.Language,
	sourceFile string, expectations map[string]*ProductionExpectations) string {

	startPoint := errNode.StartPoint()
	endPoint := errNode.EndPoint()
	row := int(startPoint.Row)
	col := int(startPoint.Column)
	endCol := int(endPoint.Column)
	if endPoint.Row != startPoint.Row {
		// Multi-line error — underline to end of first line
		lines := strings.Split(string(source), "\n")
		if row < len(lines) {
			endCol = len(lines[row])
		}
	}

	// Try Layer 2: overlay based on parent and prefix
	parent := errNode.Parent()
	message := ""
	example := ""

	if parent != nil {
		parentType := parent.Type(lang)
		prefix := buildPrefixSignature(parent, errNode, lang)
		overlayKey := parentType + "|" + prefix

		if overlay, ok := errorOverlays[overlayKey]; ok {
			message = overlay.Message
			example = overlay.Example
		}
	}

	// Try Layer 1: grammar-derived expectation
	if message == "" && parent != nil {
		message = describeExpected(parent, errNode, lang, expectations)
	}

	// Try Layer 3: keyword inference from ERROR node text
	if message == "" && errNode.IsError() {
		errText := string(source[errNode.StartByte():errNode.EndByte()])
		if prod := inferFromKeyword(errText, keywordToProduction); prod != "" {
			// Look up the production's overlay for just the keyword prefix
			kw := strings.Fields(errText)[0]
			overlayKey := prod + "|" + kw
			if overlay, ok := errorOverlays[overlayKey]; ok {
				message = overlay.Message
				example = overlay.Example
			} else {
				message = fmt.Sprintf("syntax error in %s", prod)
			}
		}
	}

	// Final fallback
	if message == "" {
		errText := ""
		if errNode.EndByte() > errNode.StartByte() {
			errText = string(source[errNode.StartByte():errNode.EndByte()])
			if len(errText) > 30 {
				errText = errText[:30] + "..."
			}
		}
		if errText != "" {
			message = fmt.Sprintf("unexpected token %q", errText)
		} else {
			message = "unexpected token"
		}
	}

	sourceContext := formatSourceLine(source, row, col, endCol)
	return formatError(sourceFile, row, col, message, sourceContext, example)
}

// buildPrefixSignature creates a compact string representing the successfully-parsed
// children before the error node. Used as part of the overlay key.
func buildPrefixSignature(parent, errNode *gotreesitter.Node, lang *gotreesitter.Language) string {
	var parts []string
	for i := 0; i < parent.ChildCount(); i++ {
		child := parent.Child(i)
		if child.StartByte() >= errNode.StartByte() {
			break
		}
		if child.IsError() || child.IsMissing() {
			break
		}
		nodeType := child.Type(lang)
		// Simplify common types for overlay matching
		switch {
		case nodeType == "interpreted_string_literal" || nodeType == "raw_string_literal":
			parts = append(parts, "string")
		case strings.HasSuffix(nodeType, "_literal"):
			parts = append(parts, nodeType)
		default:
			parts = append(parts, nodeType)
		}
	}
	return strings.Join(parts, ",")
}
```

- [ ] **Step 4: Integrate into `TranspileDanmuji`**

In `transpile.go`, update `getDanmujiLanguage` to cache the expectation map:

Add a package-level variable:

```go
var danmujiExpectationsCached map[string]*ProductionExpectations
```

In `getDanmujiLanguage`, after `danmujiLangCached, danmujiLangErr = GenerateLanguage(g)`, add:

```go
		if danmujiLangErr == nil {
			danmujiExpectationsCached = buildExpectationMap(g)
		}
```

This requires extracting `g := DanmujiGrammar()` before `GenerateLanguage` and passing it.

Then replace the `HasError` block:

```go
	if root.HasError() {
		return "", fmt.Errorf("%s", FormatParseError(source, root, lang, opts.SourceFile, danmujiExpectationsCached))
	}
```

- [ ] **Step 5: Run all tests**

Run: `go test -v -count=1 ./...`
Expected: All pass (ignore TestHighlightQueryMatchesGenerated if still out of sync).

- [ ] **Step 6: Commit**

`buckley commit --yes -min`

---

### Task 7: Comprehensive overlay tests

Write one test per major production to verify the error messages are correct.

**Files:**
- Modify: `errors_test.go`

- [ ] **Step 1: Write overlay validation tests**

Add a table-driven test that exercises each overlay by providing broken `.dmj` source and checking the error message:

```go
func TestErrorOverlays(t *testing.T) {
	lang := getDanmujiLang(t)
	g := DanmujiGrammar()
	expectations := buildExpectationMap(g)

	cases := []struct {
		name     string
		source   string
		contains string // substring expected in error message
	}{
		// BDD
		{"given missing string", "package main\nfunc f() {\n\tgiven {\n\t}\n}\n", "given"},
		{"when missing string", "package main\nfunc f() {\n\twhen {\n\t}\n}\n", "when"},
		{"then missing string", "package main\nfunc f() {\n\tthen {\n\t}\n}\n", "then"},
		// Test blocks
		{"unit missing string", "package main\nunit {\n}\n", "unit"},
		// Assertions
		{"expect bare", "package main\nfunc f() {\n\texpect\n}\n", "expect"},
		{"reject bare", "package main\nfunc f() {\n\treject\n}\n", "reject"},
		// Test doubles
		{"mock missing name", "package main\nfunc f() {\n\tmock {\n\t}\n}\n", "mock"},
		// Infrastructure
		{"needs missing service", "package main\nfunc f() {\n\tneeds {\n\t}\n}\n", "needs"},
		// Process
		{"process missing path", "package main\nfunc f() {\n\tprocess {\n\t}\n}\n", "process"},
		{"ready missing mode", "package main\nfunc f() {\n\tready\n}\n", "ready"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parser := gotreesitter.NewParser(lang)
			tree, _ := parser.Parse([]byte(tc.source))
			root := tree.RootNode()
			if !root.HasError() {
				t.Skip("no parse error — grammar accepts this")
			}
			result := FormatParseError([]byte(tc.source), root, lang, "test.dmj", expectations)
			t.Logf("Error:\n%s", result)
			if !strings.Contains(strings.ToLower(result), tc.contains) {
				t.Errorf("expected error to contain %q", tc.contains)
			}
			// Should never contain raw s-expression
			if strings.Contains(result, "(source_file") {
				t.Error("error contains raw s-expression")
			}
		})
	}
}
```

- [ ] **Step 2: Run tests**

Run: `go test -run "TestErrorOverlays" -v ./...`
Expected: PASS (some may be skipped if grammar accepts the broken input — that's fine)

- [ ] **Step 3: Commit**

`buckley commit --yes -min`

---

## Task dependency graph

```
Task 1 (source line + formatting)
  └── Task 2 (error discovery)
        └── Task 3 (expectation map)
              └── Task 4 (prefix matching)
                    └── Task 5 (overlays + keyword inference)
                          └── Task 6 (full FormatParseError + integration)
                                └── Task 7 (comprehensive overlay tests)
```

All tasks are sequential — each builds on the previous.
