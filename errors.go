package danmuji

import (
	"fmt"
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ---------------------------------------------------------------------------
// Type stubs for parse-error recovery (filled in by subsequent tasks)
// ---------------------------------------------------------------------------

// ProductionExpectations describes what a grammar production expects.
type ProductionExpectations struct {
	NodeType   string
	Expansions []LinearExpansion
}

// LinearExpansion is a single linear path through a production.
type LinearExpansion struct {
	Steps []ExpectedStep
}

// ExpectedStep is one element in a LinearExpansion.
type ExpectedStep struct {
	Type     string
	Keyword  string
	Field    string
	Optional bool
}

// ---------------------------------------------------------------------------
// Grammar introspection: buildExpectationMap + expandRule
// ---------------------------------------------------------------------------

// buildExpectationMap walks all rules in the grammar and enumerates the valid
// linear expansions for each production. This powers Layer 1 error messages
// ("expected X after Y").
func buildExpectationMap(g *Grammar) map[string]*ProductionExpectations {
	m := make(map[string]*ProductionExpectations, len(g.Rules))
	for name, rule := range g.Rules {
		exps := expandRule(rule, "")
		m[name] = &ProductionExpectations{
			NodeType:   name,
			Expansions: exps,
		}
	}
	return m
}

// expandRule recursively flattens a rule tree into all possible linear
// sequences of expected children. fieldName propagates any enclosing
// Field annotation.
func expandRule(r *Rule, fieldName string) []LinearExpansion {
	if r == nil {
		return []LinearExpansion{{}}
	}

	kind := r.Kind

	// Leaf nodes
	if kind == RuleString {
		step := ExpectedStep{Type: "string", Keyword: r.Value, Field: fieldName}
		return []LinearExpansion{{Steps: []ExpectedStep{step}}}
	}
	if kind == RulePattern {
		step := ExpectedStep{Type: "pattern", Field: fieldName}
		return []LinearExpansion{{Steps: []ExpectedStep{step}}}
	}
	if kind == RuleSymbol {
		step := ExpectedStep{Type: r.Value, Field: fieldName}
		return []LinearExpansion{{Steps: []ExpectedStep{step}}}
	}
	if kind == RuleBlank {
		return []LinearExpansion{{}} // one empty expansion
	}
	if kind == RuleToken || kind == RuleImmToken {
		step := ExpectedStep{Type: "token", Field: fieldName}
		return []LinearExpansion{{Steps: []ExpectedStep{step}}}
	}

	// Seq: cartesian product of all children's expansions
	if kind == RuleSeq {
		result := []LinearExpansion{{}} // start with one empty expansion
		for _, child := range r.Children {
			childExps := expandRule(child, "")
			var next []LinearExpansion
			for _, prefix := range result {
				for _, suffix := range childExps {
					combined := LinearExpansion{
						Steps: make([]ExpectedStep, 0, len(prefix.Steps)+len(suffix.Steps)),
					}
					combined.Steps = append(combined.Steps, prefix.Steps...)
					combined.Steps = append(combined.Steps, suffix.Steps...)
					next = append(next, combined)
					if len(next) >= 100 {
						break
					}
				}
				if len(next) >= 100 {
					break
				}
			}
			result = next
			if len(result) >= 100 {
				result = result[:100]
				break
			}
		}
		// Propagate field name to all steps if set at this level
		if fieldName != "" {
			for i := range result {
				for j := range result[i].Steps {
					if result[i].Steps[j].Field == "" {
						result[i].Steps[j].Field = fieldName
					}
				}
			}
		}
		return result
	}

	// Choice: union of all alternatives
	if kind == RuleChoice {
		var result []LinearExpansion
		for _, child := range r.Children {
			childExps := expandRule(child, fieldName)
			result = append(result, childExps...)
			if len(result) >= 100 {
				result = result[:100]
				break
			}
		}
		return result
	}

	// Optional: expand child with all steps marked Optional, prepend empty expansion
	if kind == RuleOptional {
		childExps := expandRule(r.Children[0], fieldName)
		result := make([]LinearExpansion, 0, 1+len(childExps))
		result = append(result, LinearExpansion{}) // the skip case
		for _, exp := range childExps {
			marked := LinearExpansion{Steps: make([]ExpectedStep, len(exp.Steps))}
			copy(marked.Steps, exp.Steps)
			for k := range marked.Steps {
				marked.Steps[k].Optional = true
			}
			result = append(result, marked)
		}
		return result
	}

	// Repeat: same as Optional for error reporting (0 or 1)
	if kind == RuleRepeat {
		childExps := expandRule(r.Children[0], fieldName)
		result := make([]LinearExpansion, 0, 1+len(childExps))
		result = append(result, LinearExpansion{}) // the skip case
		for _, exp := range childExps {
			marked := LinearExpansion{Steps: make([]ExpectedStep, len(exp.Steps))}
			copy(marked.Steps, exp.Steps)
			for k := range marked.Steps {
				marked.Steps[k].Optional = true
			}
			result = append(result, marked)
		}
		return result
	}

	// Repeat1: at least one occurrence
	if kind == RuleRepeat1 {
		return expandRule(r.Children[0], fieldName)
	}

	// Field: expand child with the field name set
	if kind == RuleField {
		return expandRule(r.Children[0], r.Value)
	}

	// Prec wrappers: transparent — expand child
	if kind == RulePrec || kind == RulePrecLeft || kind == RulePrecRight || kind == RulePrecDynamic {
		return expandRule(r.Children[0], fieldName)
	}

	// Alias: expand child
	if kind == RuleAlias {
		return expandRule(r.Children[0], fieldName)
	}

	// Fallback: unknown kind — return one empty expansion
	return []LinearExpansion{{}}
}

// ---------------------------------------------------------------------------
// Layer 2: hand-written error overlay table
// ---------------------------------------------------------------------------

// ErrorOverlay provides a curated message and example for a specific error
// context, keyed by "parent_node_type|prefix_signature".
type ErrorOverlay struct {
	Message string
	Example string
}

// errorOverlays maps "parent_type|prefix_signature" to hand-written error
// messages. The prefix signature is a comma-separated list of node types for
// successfully-parsed children before the ERROR node.
var errorOverlays = map[string]ErrorOverlay{
	// BDD structure
	"given_block|given":                            {"expected string after \"given\"", "given \"description\" { ... }"},
	"given_block|given,interpreted_string_literal":  {"expected { to open block", "given \"description\" { ... }"},
	"when_block|when":                              {"expected string after \"when\"", "when \"description\" { ... }"},
	"when_block|when,interpreted_string_literal":    {"expected { to open block", "when \"description\" { ... }"},
	"then_block|then":                              {"expected string after \"then\"", "then \"description\" { ... }"},
	"then_block|then,interpreted_string_literal":    {"expected { to open block", "then \"description\" { ... }"},

	// Test blocks
	"test_block|unit":                              {"expected string after test category", "unit \"name\" { ... }"},
	"test_block|unit,interpreted_string_literal":    {"expected { to open test block", "unit \"name\" { ... }"},
	"test_block|integration":                       {"expected string after test category", "integration \"name\" { ... }"},
	"test_block|e2e":                               {"expected string after test category", "e2e \"name\" { ... }"},

	// Assertions
	"expect_statement|expect":                      {"expected expression after \"expect\"", "expect x == 1"},
	"reject_statement|reject":                      {"expected expression after \"reject\"", "reject ok"},
	"verify_statement|verify":                      {"expected target after \"verify\"", "verify repo.Save called 1 times"},

	// Test doubles
	"mock_declaration|mock":                        {"expected name after \"mock\"", "mock RepoName { ... }"},
	"mock_declaration|mock,identifier":             {"expected { to open mock body", "mock RepoName { ... }"},
	"fake_declaration|fake":                        {"expected name after \"fake\"", "fake StoreName { ... }"},
	"spy_declaration|spy":                          {"expected name after \"spy\"", "spy BusName { ... }"},
	"mock_method|identifier,parameter_list":        {"expected -> return_type after parameters", "Save(u User) -> error = nil"},

	// Data-driven
	"each_do_block|each":                                            {"expected string after \"each\"", "each \"scenarios\" { ... } do { ... }"},
	"each_do_block|each,interpreted_string_literal,block":           {"expected \"do\" keyword", "each \"scenarios\" { ... } do { ... }"},
	"matrix_block|matrix":                                           {"expected string after \"matrix\"", "matrix \"dimensions\" { ... } do { ... }"},
	"property_block|property":                                       {"expected string after \"property\"", "property \"name\" for all (x int) { ... }"},
	"property_block|property,interpreted_string_literal":            {"expected \"for\" keyword", "property \"name\" for all (params) { ... }"},

	// Temporal
	"eventually_block|eventually":                  {"expected string after \"eventually\"", "eventually \"name\" within 5s { ... }"},
	"consistently_block|consistently":              {"expected string after \"consistently\"", "consistently \"name\" for 2s { ... }"},

	// Infrastructure
	"needs_block|needs":                            {"expected service type after \"needs\"", "needs postgres db { ... }"},
	"needs_block|needs,service_type":               {"expected identifier for service name", "needs postgres db { ... }"},
	"benchmark_block|benchmark":                    {"expected string after \"benchmark\"", "benchmark \"name\" { ... }"},
	"exec_block|exec":                              {"expected string after \"exec\"", "exec \"name\" { ... }"},
	"snapshot_block|snapshot":                      {"expected string after \"snapshot\"", "snapshot \"name\" { ... }"},

	// Process
	"process_block|process":                        {"expected path after \"process\"", "process \"./cmd/server\" { ... }"},
	"stop_block|stop":                              {"expected { to open stop block", "stop { signal SIGTERM ... }"},
	"ready_clause|ready":                           {"expected mode (http, tcp, stdout, delay) after \"ready\"", "ready http \"http://host/health\""},
}

// ---------------------------------------------------------------------------
// Layer 3: keyword-based fallback inference
// ---------------------------------------------------------------------------

// keywordToProduction maps danmuji keywords to their expected grammar
// production. Used as a fallback when the ERROR node's parent isn't a
// recognized production — we extract the first keyword from the error text
// and infer which production the user was trying to write.
var keywordToProduction = map[string]string{
	"given": "given_block", "when": "when_block", "then": "then_block",
	"expect": "expect_statement", "reject": "reject_statement", "verify": "verify_statement",
	"mock": "mock_declaration", "fake": "fake_declaration", "spy": "spy_declaration",
	"unit": "test_block", "integration": "test_block", "e2e": "test_block",
	"benchmark": "benchmark_block", "load": "load_block",
	"needs": "needs_block", "exec": "exec_block",
	"process": "process_block", "stop": "stop_block",
	"eventually": "eventually_block", "consistently": "consistently_block",
	"each": "each_do_block", "matrix": "matrix_block", "property": "property_block",
	"snapshot": "snapshot_block", "profile": "profile_block", "table": "table_declaration",
	"before": "lifecycle_hook", "after": "lifecycle_hook",
	"ready": "ready_clause", "signal": "signal_directive", "timeout": "timeout_directive",
	"no_leaks": "no_leaks_directive", "fake_clock": "fake_clock_directive",
}

// inferFromKeyword extracts the first word from text and looks it up in
// kwMap to determine which grammar production the user was attempting.
// Compound keywords (no_leaks, fake_clock) are checked first.
// Returns the production name or "" if no keyword matches.
func inferFromKeyword(text string, kwMap map[string]string) string {
	if text == "" {
		return ""
	}

	// Check compound keywords first (underscore-joined multi-word keywords).
	// These must be checked before splitting on whitespace because the
	// individual words may also be valid keywords.
	compoundKeywords := []string{"no_leaks", "fake_clock"}
	for _, ck := range compoundKeywords {
		if strings.HasPrefix(text, ck) {
			// Ensure the compound keyword is followed by whitespace or end-of-string.
			rest := text[len(ck):]
			if rest == "" || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n' {
				if prod, ok := kwMap[ck]; ok {
					return prod
				}
			}
		}
	}

	// Extract the first whitespace-delimited word.
	word := text
	if idx := strings.IndexAny(text, " \t\n"); idx >= 0 {
		word = text[:idx]
	}

	if prod, ok := kwMap[word]; ok {
		return prod
	}
	return ""
}

// buildPrefixSignature walks the parent's children up to (but not including)
// errNode and returns a comma-separated string of their Tree-sitter node types.
// This signature is used as the suffix of overlay map keys.
func buildPrefixSignature(parent, errNode *gotreesitter.Node, lang *gotreesitter.Language) string {
	var parts []string
	childCount := parent.ChildCount()
	errStart := errNode.StartByte()
	errEnd := errNode.EndByte()

	for i := 0; i < childCount; i++ {
		child := parent.Child(i)
		// Stop when we reach the error node.
		if child.StartByte() == errStart && child.EndByte() == errEnd {
			break
		}
		parts = append(parts, child.Type(lang))
	}

	return strings.Join(parts, ",")
}

// FormatParseError is a placeholder — implemented in Task 6.
func FormatParseError(source []byte, root *gotreesitter.Node, lang *gotreesitter.Language,
	sourceFile string, expectations map[string]*ProductionExpectations) string {
	return "parse error (not yet implemented)"
}

// ---------------------------------------------------------------------------
// Source-line rendering helpers
// ---------------------------------------------------------------------------

// formatSourceLine renders a source line with a caret underline highlighting
// the span [startCol, endCol). All positional arguments are 0-indexed
// (Tree-sitter convention); the output uses 1-indexed line numbers.
//
// Returns two lines joined by a newline, e.g.:
//
//	  4 | 	given valid {
//	    | 	      ^^^^^
//
// Returns "" when row is out of range.
func formatSourceLine(source []byte, row, startCol, endCol int) string {
	lines := strings.Split(string(source), "\n")
	if row < 0 || row >= len(lines) {
		return ""
	}

	line := lines[row]

	// Clamp endCol to line length.
	if endCol > len(line) {
		endCol = len(line)
	}
	// Ensure endCol > startCol.
	if endCol <= startCol {
		endCol = startCol + 1
	}

	// Build the line-number prefix. 1-indexed for display.
	lineNum := row + 1
	numStr := fmt.Sprintf("%d", lineNum)
	pad := strings.Repeat(" ", len(numStr))

	// Source line.
	srcLine := fmt.Sprintf("  %s | %s", numStr, line)

	// Caret line: preserve tabs for alignment, spaces otherwise.
	var caret strings.Builder
	caret.WriteString(fmt.Sprintf("  %s | ", pad))
	for i := 0; i < endCol; i++ {
		if i < startCol {
			// Mirror whitespace for alignment.
			if i < len(line) && line[i] == '\t' {
				caret.WriteByte('\t')
			} else {
				caret.WriteByte(' ')
			}
		} else {
			caret.WriteByte('^')
		}
	}

	return srcLine + "\n" + caret.String()
}

// formatError produces a human-readable parse-error message.
//
// row and col are 0-indexed; the output is 1-indexed.
//
// Example output:
//
//	/tmp/test.dmj:4:8: expected string after "given"
//	  4 | 	given valid {
//	    | 	      ^^^^^
//	   hint: given "description" { ... }
func formatError(sourceFile string, row, col int, message, sourceContext, example string) string {
	var b strings.Builder

	// Location header.
	lineNum := row + 1
	colNum := col + 1
	if sourceFile != "" {
		fmt.Fprintf(&b, "%s:%d:%d: %s", sourceFile, lineNum, colNum, message)
	} else {
		fmt.Fprintf(&b, "%d:%d: %s", lineNum, colNum, message)
	}

	// Source context (the formatted source line + caret underline).
	if sourceContext != "" {
		b.WriteByte('\n')
		b.WriteString(sourceContext)
	}

	// Optional hint.
	if example != "" {
		b.WriteByte('\n')
		fmt.Fprintf(&b, "   hint: %s", example)
	}

	return b.String()
}

// ---------------------------------------------------------------------------
// Error discovery
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Prefix matching and Layer 1 message generation
// ---------------------------------------------------------------------------

// describeExpected generates a human-readable message describing what was
// expected at the position of errNode within parent. It looks up the parent's
// grammar production, collects the types of children parsed before the error,
// and uses matchExpansions to determine what should come next.
//
// When the parent is an ERROR node (common with tree-sitter's error recovery),
// the function scans the parent's children for keyword tokens that partially
// match known productions, and suggests what should come next.
func describeExpected(parent, errNode *gotreesitter.Node, lang *gotreesitter.Language, expectations map[string]*ProductionExpectations) string {
	parentType := parent.Type(lang)

	// Find the error node's index among siblings by comparing start/end bytes.
	errStart := errNode.StartByte()
	errEnd := errNode.EndByte()
	errIdx := -1
	childCount := parent.ChildCount()
	for i := 0; i < childCount; i++ {
		child := parent.Child(i)
		if child.StartByte() == errStart && child.EndByte() == errEnd {
			errIdx = i
			break
		}
	}
	if errIdx < 0 {
		return fmt.Sprintf("unexpected token in %s", parentType)
	}

	// Collect node types of successfully-parsed children before the error.
	var prefix []string
	for i := 0; i < errIdx; i++ {
		child := parent.Child(i)
		prefix = append(prefix, child.Type(lang))
	}

	// If the parent is a recognized production, match against its expansions.
	if pe, ok := expectations[parentType]; ok {
		next := matchExpansions(prefix, pe.Expansions)
		if len(next) > 0 {
			return formatExpectedMessage(next, parentType)
		}
		return fmt.Sprintf("unexpected token in %s", parentType)
	}

	// Parent is an ERROR or unrecognized node. Scan backward from the error
	// position to find keyword tokens that start a known production, then
	// use the prefix from that keyword onward.
	if msg := describeExpectedFromKeywords(prefix, expectations, parentType); msg != "" {
		return msg
	}

	return fmt.Sprintf("unexpected token in %s", parentType)
}

// describeExpectedFromKeywords tries to match the tail of a prefix of parsed
// children against known productions. It scans backward for a keyword that
// starts a known expansion and suggests what should come next.
func describeExpectedFromKeywords(prefix []string, expectations map[string]*ProductionExpectations, parentType string) string {
	// Scan backward through the prefix for keywords that start known productions.
	for i := len(prefix) - 1; i >= 0; i-- {
		candidate := prefix[i]
		// Try each production to see if candidate matches its first step.
		for _, pe := range expectations {
			subPrefix := prefix[i:]
			next := matchExpansions(subPrefix, pe.Expansions)
			if len(next) > 0 {
				return formatExpectedMessage(next, parentType)
			}
		}
		_ = candidate
	}
	return ""
}

// formatExpectedMessage formats a list of expected next steps into a
// human-readable message like "expected X", "expected X or Y", or
// "expected X, Y, or Z".
func formatExpectedMessage(next []ExpectedStep, parentType string) string {
	// Deduplicate expected descriptions.
	seen := make(map[string]bool)
	var descriptions []string
	for _, step := range next {
		desc := describeStep(step)
		if !seen[desc] {
			seen[desc] = true
			descriptions = append(descriptions, desc)
		}
	}
	if len(descriptions) == 0 {
		return fmt.Sprintf("unexpected token in %s", parentType)
	}

	switch len(descriptions) {
	case 1:
		return fmt.Sprintf("expected %s", descriptions[0])
	case 2:
		return fmt.Sprintf("expected %s or %s", descriptions[0], descriptions[1])
	default:
		return fmt.Sprintf("expected %s, or %s",
			strings.Join(descriptions[:len(descriptions)-1], ", "),
			descriptions[len(descriptions)-1])
	}
}

// matchExpansions tries to advance each expansion through the given prefix
// of parsed child types. For each expansion that fully consumes the prefix,
// it collects the next expected step. Optional steps that don't match are
// skipped during prefix consumption.
func matchExpansions(prefix []string, expansions []LinearExpansion) []ExpectedStep {
	var result []ExpectedStep

	for _, exp := range expansions {
		next := matchSingleExpansion(prefix, exp.Steps, 0, 0)
		result = append(result, next...)
	}

	return result
}

// matchSingleExpansion recursively matches prefix[pi:] against steps[si:]
// and collects the next expected step(s) after the prefix is consumed.
func matchSingleExpansion(prefix []string, steps []ExpectedStep, pi, si int) []ExpectedStep {
	// Prefix fully consumed — collect the next step(s).
	if pi >= len(prefix) {
		// Find the next non-consumed step.
		for si < len(steps) {
			step := steps[si]
			return []ExpectedStep{step}
		}
		// All steps consumed too — no next step.
		return nil
	}

	// Steps exhausted but prefix not consumed — mismatch.
	if si >= len(steps) {
		return nil
	}

	step := steps[si]
	nodeType := prefix[pi]

	if stepMatches(step, nodeType) {
		// This step matches the current prefix element — advance both.
		return matchSingleExpansion(prefix, steps, pi+1, si+1)
	}

	// Step doesn't match. If optional, skip it and try the next step.
	if step.Optional {
		return matchSingleExpansion(prefix, steps, pi, si+1)
	}

	// Non-optional step doesn't match — this expansion doesn't apply.
	return nil
}

// stepMatches checks if a step matches a parsed node type.
// If the step has a Keyword, it matches against the keyword.
// Otherwise it matches step.Type against nodeType.
func stepMatches(step ExpectedStep, nodeType string) bool {
	if step.Keyword != "" {
		return step.Keyword == nodeType
	}
	return step.Type == nodeType
}

// describeStep returns a human-readable description of an expected step.
func describeStep(step ExpectedStep) string {
	if step.Keyword != "" {
		return fmt.Sprintf("%q", step.Keyword)
	}

	switch step.Type {
	case "_string_literal":
		return "string"
	case "block":
		return "{"
	case "identifier":
		return "identifier"
	case "_expression":
		return "expression"
	case "parameter_list":
		return "parameter list"
	}

	// Types with underscore prefix are typically hidden rules — strip prefix.
	if strings.HasPrefix(step.Type, "_") {
		return strings.TrimPrefix(step.Type, "_")
	}

	// Use field name as fallback if available.
	if step.Field != "" {
		return step.Field
	}

	return step.Type
}
