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
