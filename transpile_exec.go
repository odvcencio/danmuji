package danmuji

import (
	"fmt"
	gotreesitter "github.com/odvcencio/gotreesitter"
	"strings"
)

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

	// Find statement list so we can preserve exact child order.
	var statements *gotreesitter.Node
	for i := 0; i < int(bodyNode.NamedChildCount()); i++ {
		c := bodyNode.NamedChild(i)
		if t.nodeType(c) == "statement_list" {
			statements = c
			break
		}
	}
	if statements == nil {
		// Fallback: if grammar shape changes, preserve direct named children.
		for i := 0; i < int(bodyNode.NamedChildCount()); i++ {
			c := bodyNode.NamedChild(i)
			if t.nodeType(c) == "run_command" || t.nodeType(c) == "expect_statement" || t.nodeType(c) == "reject_statement" {
				statements = bodyNode
				break
			}
		}
	}
	if statements == nil {
		return t.text(n)
	}

	var b strings.Builder
	b.WriteString(t.lineDirective(n))
	fmt.Fprintf(&b, "%s.Run(%q, func(%s *testing.T) {\n", t.testVar, name, t.testVar)
	fmt.Fprintf(&b, "\tvar stdout, stderr bytes.Buffer\n")
	fmt.Fprintf(&b, "\tvar exitCode int\n")
	fmt.Fprintf(&b, "\tvar err error\n")
	fmt.Fprintf(&b, "\t_ = exitCode\n")
	fmt.Fprintf(&b, "\t_ = err\n")

	oldInExec := t.inExecBlock
	t.inExecBlock = true
	defer func() { t.inExecBlock = oldInExec }()

	// Preserve statement order and allow all inner statements (run commands, asserts, setup code).
	for i := 0; i < int(statements.NamedChildCount()); i++ {
		stmt := statements.NamedChild(i)
		nt := t.nodeType(stmt)
		switch nt {
		case "run_command":
			t.appendIndented(&b, t.emitExecRunCommand(stmt), "\t")
		case "expect_statement", "reject_statement":
			t.appendIndented(&b, t.emit(stmt), "\t")
		default:
			t.appendIndented(&b, t.emit(stmt), "\t")
		}
	}

	fmt.Fprintf(&b, "})")
	return b.String()
}

func (t *dmjTranspiler) appendIndented(b *strings.Builder, code, indent string) {
	if code == "" {
		return
	}
	lines := strings.Split(code, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if line == "" {
			continue
		}
		b.WriteString(indent)
		b.WriteString(line)
		if i < len(lines)-1 || line != "" {
			b.WriteByte('\n')
		}
	}
}

func (t *dmjTranspiler) emitExecRunCommand(n *gotreesitter.Node) string {
	cmdNode := t.childByField(n, "command")
	if cmdNode == nil {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "stdout.Reset()\n")
	fmt.Fprintf(&b, "stderr.Reset()\n")
	fmt.Fprintf(&b, "cmd := exec.Command(\"sh\", \"-c\", %s)\n", t.text(cmdNode))
	fmt.Fprintf(&b, "cmd.Stdout = &stdout\n")
	fmt.Fprintf(&b, "cmd.Stderr = &stderr\n")
	fmt.Fprintf(&b, "exitCode = 0\n")
	fmt.Fprintf(&b, "err = cmd.Run()\n")
	fmt.Fprintf(&b, "if err != nil {\n")
	fmt.Fprintf(&b, "\tif exitErr, ok := err.(*exec.ExitError); ok {\n")
	fmt.Fprintf(&b, "\t\texitCode = exitErr.ExitCode()\n")
	fmt.Fprintf(&b, "\t} else {\n")
	fmt.Fprintf(&b, "\t\texitCode = -1\n")
	fmt.Fprintf(&b, "\t}\n")
	fmt.Fprintf(&b, "}\n")

	return b.String()
}

func (t *dmjTranspiler) emitExecReject(n *gotreesitter.Node) string {
	if t.inPollingBlock {
		return fmt.Sprintf("!(%s)", t.emitExecExpectCondition(n))
	}

	actual := t.childByField(n, "actual")
	if actual == nil {
		return t.text(n)
	}
	actualText := strings.TrimSpace(t.emit(actual))
	translated := t.translateExecIdent(actualText)

	t.addImport("github.com/stretchr/testify/assert")
	if actualText == "stdout" {
		return fmt.Sprintf("assert.False(%s, %s == \"\")", t.testVar, translated)
	}
	if actualText == "stderr" {
		return fmt.Sprintf("assert.False(%s, %s == \"\")", t.testVar, translated)
	}
	if actualText == "exit_code" {
		return fmt.Sprintf("assert.NotZero(%s, %s)", t.testVar, translated)
	}
	return fmt.Sprintf("assert.False(%s, %s)", t.testVar, translated)
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
	if t.inPollingBlock {
		return t.emitExecExpectCondition(n)
	}

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
				assertionName := t.equalityAssertionName(left, right)
				return fmt.Sprintf("%s(%s, %s, %s)", assertionName, t.testVar, rT, lT)
			case "!=":
				assertionName := t.inequalityAssertionName(left, right)
				return fmt.Sprintf("%s(%s, %s, %s)", assertionName, t.testVar, rT, lT)
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

func (t *dmjTranspiler) emitExecExpectCondition(n *gotreesitter.Node) string {
	nodeText := t.text(n)
	// Handle "expect stdout contains X"
	if strings.Contains(nodeText, "stdout") && strings.Contains(nodeText, "contains") {
		expected := t.childByField(n, "expected")
		if expected != nil {
			return fmt.Sprintf("danmujiContains(stdout.String(), %s)", t.emit(expected))
		}
	}

	// Handle "expect stderr contains X"
	if strings.Contains(nodeText, "stderr") && strings.Contains(nodeText, "contains") {
		expected := t.childByField(n, "expected")
		if expected != nil {
			return fmt.Sprintf("danmujiContains(stderr.String(), %s)", t.emit(expected))
		}
	}

	// Handle exit_code comparisons.
	actual := t.childByField(n, "actual")
	if actual != nil {
		actualText := t.text(actual)
		if t.nodeType(actual) == "binary_expression" && actual.ChildCount() >= 3 {
			left := actual.Child(0)
			op := actual.Child(1)
			right := actual.Child(2)
			leftText := t.translateExecIdent(t.emit(left))
			rightText := t.emit(right)
			opText := t.text(op)
			switch opText {
			case "==":
				return fmt.Sprintf("%s == %s", leftText, rightText)
			case "!=":
				return fmt.Sprintf("%s != %s", leftText, rightText)
			case "<":
				return fmt.Sprintf("%s < %s", leftText, rightText)
			case ">":
				return fmt.Sprintf("%s > %s", leftText, rightText)
			case "<=":
				return fmt.Sprintf("%s <= %s", leftText, rightText)
			case ">=":
				return fmt.Sprintf("%s >= %s", leftText, rightText)
			}
		}
		if strings.Contains(actualText, "exit_code") {
			return fmt.Sprintf("%s != 0", t.translateExecIdent(actualText))
		}
		if strings.Contains(actualText, "stdout") {
			return fmt.Sprintf("%s != \"\"", t.translateExecIdent(actualText))
		}
		if strings.Contains(actualText, "stderr") {
			return fmt.Sprintf("%s != \"\"", t.translateExecIdent(actualText))
		}
	}

	return "false"
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
