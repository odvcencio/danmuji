package danmuji

import (
	"fmt"
	"strings"
	"sync"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

var (
	fmtLangOnce   sync.Once
	fmtLangCached *gotreesitter.Language
	fmtLangErr    error
)

func getFmtLanguage() (*gotreesitter.Language, error) {
	fmtLangOnce.Do(func() {
		fmtLangCached, fmtLangErr = GenerateLanguage(DanmujiGrammar())
	})
	return fmtLangCached, fmtLangErr
}

// FormatDanmuji parses a .dmj source file and returns it with canonical indentation.
// Each nesting level uses one tab. Blank lines between top-level declarations are preserved.
func FormatDanmuji(source []byte) (string, error) {
	lang, err := getFmtLanguage()
	if err != nil {
		return "", fmt.Errorf("generate language: %w", err)
	}

	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse(source)
	if err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}

	root := tree.RootNode()
	f := &dmjFormatter{src: source, lang: lang}
	return f.format(root), nil
}

type dmjFormatter struct {
	src  []byte
	lang *gotreesitter.Language
}

func (f *dmjFormatter) nodeType(n *gotreesitter.Node) string {
	return n.Type(f.lang)
}

func (f *dmjFormatter) text(n *gotreesitter.Node) string {
	return string(f.src[n.StartByte():n.EndByte()])
}

func (f *dmjFormatter) format(n *gotreesitter.Node) string {
	nt := f.nodeType(n)

	if nt == "source_file" {
		return f.formatSourceFile(n)
	}

	// For leaf nodes, return the source text.
	if n.ChildCount() == 0 {
		return f.text(n)
	}

	// For block nodes, format with indentation.
	if nt == "block" {
		return f.formatBlock(n)
	}

	// Default: preserve structure by walking children with gaps.
	return f.formatDefault(n)
}

func (f *dmjFormatter) formatSourceFile(n *gotreesitter.Node) string {
	var b strings.Builder
	prevEnd := n.StartByte()

	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		// Preserve blank lines between top-level declarations.
		gap := string(f.src[prevEnd:child.StartByte()])
		blankLines := strings.Count(gap, "\n")
		if i > 0 && blankLines > 1 {
			b.WriteString("\n\n")
		} else if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(f.format(child))
		prevEnd = child.EndByte()
	}

	// Ensure trailing newline.
	result := b.String()
	if !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return result
}

func (f *dmjFormatter) formatBlock(n *gotreesitter.Node) string {
	var b strings.Builder
	b.WriteString("{")

	// Find statement_list inside the block.
	var stmtList *gotreesitter.Node
	for i := 0; i < n.NamedChildCount(); i++ {
		c := n.NamedChild(i)
		if f.nodeType(c) == "statement_list" {
			stmtList = c
			break
		}
	}

	if stmtList != nil && stmtList.NamedChildCount() > 0 {
		b.WriteString("\n")
		for i := 0; i < stmtList.NamedChildCount(); i++ {
			stmt := stmtList.NamedChild(i)
			formatted := f.format(stmt)
			// Indent each line of the formatted statement.
			lines := strings.Split(formatted, "\n")
			for j, line := range lines {
				if line == "" {
					if j < len(lines)-1 {
						b.WriteString("\n")
					}
					continue
				}
				b.WriteString("\t")
				b.WriteString(line)
				b.WriteString("\n")
			}
		}
	}

	b.WriteString("}")
	return b.String()
}

func (f *dmjFormatter) formatDefault(n *gotreesitter.Node) string {
	cc := n.ChildCount()
	if cc == 0 {
		return f.text(n)
	}

	var b strings.Builder
	prev := n.StartByte()
	for i := 0; i < cc; i++ {
		c := n.Child(i)
		// Preserve the gap between children (whitespace, but normalize it).
		if c.StartByte() > prev {
			gap := string(f.src[prev:c.StartByte()])
			// Collapse whitespace to a single space, but preserve newlines.
			if strings.Contains(gap, "\n") {
				b.WriteString("\n")
			} else if len(gap) > 0 {
				b.WriteString(" ")
			}
		}
		b.WriteString(f.format(c))
		prev = c.EndByte()
	}
	// Trailing content after last child.
	if n.EndByte() > prev {
		trailing := string(f.src[prev:n.EndByte()])
		b.WriteString(trailing)
	}
	return b.String()
}
