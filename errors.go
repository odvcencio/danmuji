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
