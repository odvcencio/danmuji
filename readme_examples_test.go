package danmuji

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

var readmeDanmujiFence = regexp.MustCompile("(?s)```dmj\\n(.*?)```")

func TestReadmeDanmujiExamplesTranspile(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}

	matches := readmeDanmujiFence.FindAllStringSubmatch(string(readme), -1)
	if len(matches) == 0 {
		t.Fatal("expected at least one ```dmj fenced block in README.md")
	}

	for i, match := range matches {
		snippet := strings.TrimSpace(match[1])
		source := wrapReadmeDanmujiSnippet(normalizeReadmeDanmujiSnippet(snippet))

		if _, err := TranspileDanmuji([]byte(source), TranspileOptions{
			SourceFile: fmt.Sprintf("README.md:dmj:%d", i+1),
		}); err != nil {
			t.Fatalf("README dmj example %d failed to transpile:\n%s\n\nerror: %v", i+1, source, err)
		}
	}
}

func normalizeReadmeDanmujiSnippet(snippet string) string {
	lines := strings.Split(snippet, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "..." {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = indent + "expect true"
			continue
		}
		if strings.Contains(line, "{ ... }") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = strings.Replace(line, "{ ... }", "{\n"+indent+"\texpect true\n"+indent+"}", 1)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func wrapReadmeDanmujiSnippet(snippet string) string {
	trimmed := strings.TrimSpace(snippet)
	if strings.HasPrefix(trimmed, "package ") {
		return trimmed
	}

	header := "package readme_test\n\nimport \"testing\"\n\n"
	topLevelBlock := regexp.MustCompile(`(?m)^\s*(?:@\w+\s+)*(unit|integration|e2e|benchmark|load)\b`)
	if topLevelBlock.MatchString(trimmed) {
		return header + trimmed + "\n"
	}

	return header + "unit \"README snippet\" {\n" + indentReadmeSnippet(trimmed, "\t") + "\n}\n"
}

func indentReadmeSnippet(snippet, indent string) string {
	lines := strings.Split(snippet, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			lines[i] = ""
			continue
		}
		lines[i] = indent + line
	}
	return strings.Join(lines, "\n")
}
