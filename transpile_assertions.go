package danmuji

import (
	"fmt"
	"strings"
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ---------------------------------------------------------------------------
// expect → assertion
//
// CRITICAL: Go's grammar absorbs == / != into binary_expression, so when
// expect's "actual" field is a binary_expression node we must extract
// left/op/right from its children (Child(0), Child(1), Child(2)) and emit
// the appropriate assertion. For bare expect (no binary op), emit truthiness.
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitExpect(n *gotreesitter.Node) string {
	if t.inPollingBlock || t.inPropertyBlock {
		return t.emitExpectCondition(n)
	}
	return t.emitExpectAssertion(n)
}

func (t *dmjTranspiler) emitExpectCondition(n *gotreesitter.Node) string {
	actual := t.childByField(n, "actual")
	expected := t.childByField(n, "expected")
	matcher := t.childByField(n, "matcher")

	if actual == nil {
		return "false"
	}
	nodeText := t.text(n)

	if matcher != nil {
		actualText := t.emit(actual)
		matcherText := strings.TrimSpace(t.emit(matcher))
		if expected != nil {
			expectedText := t.emit(expected)
			return fmt.Sprintf("%s(%s, %s)", matcherText, actualText, expectedText)
		}
		return fmt.Sprintf("%s(%s)", matcherText, actualText)
	}

	if strings.Contains(nodeText, "is_nil") {
		actualText := t.emit(actual)
		return fmt.Sprintf("%s == nil", actualText)
	}
	if strings.Contains(nodeText, "not_nil") {
		actualText := t.emit(actual)
		return fmt.Sprintf("%s != nil", actualText)
	}
	if strings.Contains(nodeText, "contains") && expected != nil {
		actualText := t.emit(actual)
		expectedText := t.emit(expected)
		return fmt.Sprintf("danmujiContains(%s, %s)", actualText, expectedText)
	}

	// If the grammar's explicit expected field is populated, use it directly.
	if expected != nil {
		actualText := t.emit(actual)
		expectedText := t.emit(expected)
		if strings.Contains(nodeText, "!=") {
			// Special case: x != nil
			if expectedText == "nil" {
				return fmt.Sprintf("%s != nil", actualText)
			}
			return fmt.Sprintf("!danmujiDeepEqual(%s, %s)", expectedText, actualText)
		}
		// Special case: err == nil → require.NoError
		if expectedText == "nil" && strings.HasSuffix(actualText, "err") {
			return fmt.Sprintf("%s == nil", actualText)
		}
		// Special case: x == nil
		if expectedText == "nil" {
			return fmt.Sprintf("%s == nil", actualText)
		}
		return fmt.Sprintf("danmujiDeepEqual(%s, %s)", expectedText, actualText)
	}

	// If actual is a binary_expression (e.g. Go absorbed "x == 5" into one node),
	// extract left/op/right from its children.
	if t.nodeType(actual) == "binary_expression" && actual.ChildCount() >= 3 {
		left := actual.Child(0)
		op := actual.Child(1)
		right := actual.Child(2)
		lT := t.emit(left)
		opT := t.text(op)
		rT := t.emit(right)
		switch opT {
		case "==":
			// Special case: err == nil → require.NoError
			if rT == "nil" && strings.HasSuffix(lT, "err") {
				return fmt.Sprintf("%s == nil", lT)
			}
			// Special case: x == nil
			if rT == "nil" {
				return fmt.Sprintf("%s == nil", lT)
			}
			return fmt.Sprintf("danmujiDeepEqual(%s, %s)", rT, lT)
		case "!=":
			if rT == "nil" {
				return fmt.Sprintf("%s != nil", lT)
			}
			return fmt.Sprintf("!danmujiDeepEqual(%s, %s)", rT, lT)
		case "<":
			return fmt.Sprintf("%s < %s", lT, rT)
		case ">":
			return fmt.Sprintf("%s > %s", lT, rT)
		case "<=":
			return fmt.Sprintf("%s <= %s", lT, rT)
		case ">=":
			return fmt.Sprintf("%s >= %s", lT, rT)
		}
	}

	// Bare expect (truthiness check)
	actualText := t.emit(actual)
	return actualText
}

func (t *dmjTranspiler) emitExpectAssertion(n *gotreesitter.Node) string {
	actual := t.childByField(n, "actual")
	expected := t.childByField(n, "expected")
	matcher := t.childByField(n, "matcher")

	if actual == nil {
		return t.text(n)
	}

	ld := t.lineDirective(n)
	nodeText := t.text(n)
	msg := t.expectFailureContext("expect", strings.TrimSpace(nodeText), n)

	if matcher != nil {
		actualText := t.emit(actual)
		matcherText := strings.TrimSpace(t.emit(matcher))
		t.addImport("github.com/stretchr/testify/assert")
		if expected != nil {
			expectedText := t.emit(expected)
			return ld + fmt.Sprintf("assert.True(%s, %s(%s, %s), %s)", t.testVar, matcherText, actualText, expectedText, msg)
		}
		return ld + fmt.Sprintf("assert.True(%s, %s(%s), %s)", t.testVar, matcherText, actualText, msg)
	}

	if strings.Contains(nodeText, "is_nil") {
		t.addImport("github.com/stretchr/testify/assert")
		actualText := t.emit(actual)
		return ld + fmt.Sprintf("assert.Nil(%s, %s, %s)", t.testVar, actualText, msg)
	}
	if strings.Contains(nodeText, "not_nil") {
		t.addImport("github.com/stretchr/testify/assert")
		actualText := t.emit(actual)
		return ld + fmt.Sprintf("assert.NotNil(%s, %s, %s)", t.testVar, actualText, msg)
	}
	if strings.Contains(nodeText, "contains") && expected != nil {
		t.addImport("github.com/stretchr/testify/assert")
		actualText := t.emit(actual)
		expectedText := t.emit(expected)
		return ld + fmt.Sprintf("assert.Contains(%s, %s, %s, %s)", t.testVar, actualText, expectedText, msg)
	}

	if expected != nil {
		actualText := t.emit(actual)
		expectedText := t.emit(expected)
		if strings.Contains(nodeText, "!=") {
			if expectedText == "nil" {
				t.addImport("github.com/stretchr/testify/assert")
				return ld + fmt.Sprintf("assert.NotNil(%s, %s, %s)", t.testVar, actualText, msg)
			}
			t.addImport("github.com/stretchr/testify/assert")
			return ld + fmt.Sprintf("assert.NotEqual(%s, %s, %s, %s)", t.testVar, expectedText, actualText, msg)
		}
		if expectedText == "nil" && strings.HasSuffix(actualText, "err") {
			t.addImport("github.com/stretchr/testify/require")
			return ld + fmt.Sprintf("require.NoError(%s, %s, %s)", t.testVar, actualText, msg)
		}
		if expectedText == "nil" {
			t.addImport("github.com/stretchr/testify/assert")
			return ld + fmt.Sprintf("assert.Nil(%s, %s, %s)", t.testVar, actualText, msg)
		}
		t.addImport("github.com/stretchr/testify/assert")
		return ld + fmt.Sprintf("assert.Equal(%s, %s, %s, %s)", t.testVar, expectedText, actualText, msg)
	}

	if t.nodeType(actual) == "binary_expression" && actual.ChildCount() >= 3 {
		left := actual.Child(0)
		op := actual.Child(1)
		right := actual.Child(2)
		lT := t.emit(left)
		opT := t.text(op)
		rT := t.emit(right)
		switch opT {
		case "==":
			if rT == "nil" && strings.HasSuffix(lT, "err") {
				t.addImport("github.com/stretchr/testify/require")
				return ld + fmt.Sprintf("require.NoError(%s, %s, %s)", t.testVar, lT, msg)
			}
			if rT == "nil" {
				t.addImport("github.com/stretchr/testify/assert")
				return ld + fmt.Sprintf("assert.Nil(%s, %s, %s)", t.testVar, lT, msg)
			}
			t.addImport("github.com/stretchr/testify/assert")
			return ld + fmt.Sprintf("assert.Equal(%s, %s, %s, %s)", t.testVar, rT, lT, msg)
		case "!=":
			if rT == "nil" {
				t.addImport("github.com/stretchr/testify/assert")
				return ld + fmt.Sprintf("assert.NotNil(%s, %s, %s)", t.testVar, lT, msg)
			}
			t.addImport("github.com/stretchr/testify/assert")
			return ld + fmt.Sprintf("assert.NotEqual(%s, %s, %s, %s)", t.testVar, rT, lT, msg)
		case "<":
			t.addImport("github.com/stretchr/testify/assert")
			return ld + fmt.Sprintf("assert.Less(%s, %s, %s, %s)", t.testVar, lT, rT, msg)
		case ">":
			t.addImport("github.com/stretchr/testify/assert")
			return ld + fmt.Sprintf("assert.Greater(%s, %s, %s, %s)", t.testVar, lT, rT, msg)
		case "<=":
			t.addImport("github.com/stretchr/testify/assert")
			return ld + fmt.Sprintf("assert.LessOrEqual(%s, %s, %s, %s)", t.testVar, lT, rT, msg)
		case ">=":
			t.addImport("github.com/stretchr/testify/assert")
			return ld + fmt.Sprintf("assert.GreaterOrEqual(%s, %s, %s, %s)", t.testVar, lT, rT, msg)
		}
	}

	t.addImport("github.com/stretchr/testify/assert")
	actualText := t.emit(actual)
	return ld + fmt.Sprintf("assert.True(%s, %s, %s)", t.testVar, actualText, msg)
}

// ---------------------------------------------------------------------------
// reject → inverse truthiness
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitReject(n *gotreesitter.Node) string {
	if t.inPollingBlock || t.inPropertyBlock {
		return fmt.Sprintf("!(%s)", t.emitExpectCondition(n))
	}

	actual := t.childByField(n, "actual")
	if actual == nil {
		return t.text(n)
	}
	msg := t.expectFailureContext("reject", strings.TrimSpace(t.text(n)), n)
	t.addImport("github.com/stretchr/testify/assert")
	actualText := t.emit(actual)
	return t.lineDirective(n) + fmt.Sprintf("assert.False(%s, %s, %s)", t.testVar, actualText, msg)
}

// ---------------------------------------------------------------------------
// eventually / consistently
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitEventually(n *gotreesitter.Node) string {
	return t.emitPolling(n, "eventually")
}

func (t *dmjTranspiler) emitConsistently(n *gotreesitter.Node) string {
	return t.emitPolling(n, "consistently")
}

func (t *dmjTranspiler) emitPolling(n *gotreesitter.Node, mode string) string {
	nameNode := t.childByField(n, "name")
	name := "assertion window"
	if nameNode != nil {
		name = strings.Trim(t.text(nameNode), "\"'`")
	}
	durationNode := t.childByField(n, "duration")
	timeout := t.pollingDuration(durationNode, mode)
	bodyNode := t.childByField(n, "body")
	if bodyNode == nil {
		return t.text(n)
	}

	t.addImport("time")

	var b strings.Builder
	line := t.lineOf(n)
	fmt.Fprintf(&b, "{\n")
	fmt.Fprintf(&b, "\tsatisfied := false\n")
	fmt.Fprintf(&b, "\tdeadline := time.Now().Add(%s)\n", timeout)
	if mode == "eventually" {
		fmt.Fprintf(&b, "\tfor time.Now().Before(deadline) {\n")
	} else {
		fmt.Fprintf(&b, "\tfor {\n")
	}
	fmt.Fprintf(&b, "\t\tsucceeded := true\n")
	oldInPolling := t.inPollingBlock
	t.inPollingBlock = true
	t.emitPollingBody(&b, bodyNode, "\t")
	t.inPollingBlock = oldInPolling
	if mode == "eventually" {
		fmt.Fprintf(&b, "\t\tif succeeded {\n")
		fmt.Fprintf(&b, "\t\t\tsatisfied = true\n")
		fmt.Fprintf(&b, "\t\t\tbreak\n")
		fmt.Fprintf(&b, "\t\t}\n")
		fmt.Fprintf(&b, "\t\ttime.Sleep(10 * time.Millisecond)\n")
		fmt.Fprintf(&b, "\t}\n")
	} else {
		fmt.Fprintf(&b, "\t\tif !succeeded {\n")
		fmt.Fprintf(&b, "\t\t\tbreak\n")
		fmt.Fprintf(&b, "\t\t}\n")
		fmt.Fprintf(&b, "\t\tif time.Now().After(deadline) {\n")
		fmt.Fprintf(&b, "\t\t\tsatisfied = true\n")
		fmt.Fprintf(&b, "\t\t\tbreak\n")
		fmt.Fprintf(&b, "\t\t}\n")
		fmt.Fprintf(&b, "\t\ttime.Sleep(10 * time.Millisecond)\n")
		fmt.Fprintf(&b, "\t}\n")
	}
	fmt.Fprintf(&b, "\tif !satisfied {\n")
	fmt.Fprintf(&b, "\t\t%[1]s.Errorf(\"danmuji:%[2]d %[3]s check %[4]s failed after %[5]s\")\n", t.testVar, line, mode, name, timeout)
	fmt.Fprintf(&b, "\t}\n")
	fmt.Fprintf(&b, "}\n")
	return b.String()
}

// ---------------------------------------------------------------------------
// property_block → testing/quick.Check for invariant-style specs
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitProperty(n *gotreesitter.Node) string {
	nameNode := t.childByField(n, "name")
	name := "property"
	if nameNode != nil {
		name = strings.Trim(t.text(nameNode), "\"'`")
	}

	paramsNode := t.childByField(n, "params")
	params := "()"
	if paramsNode != nil {
		params = strings.TrimSpace(t.text(paramsNode))
	}

	maxCountNode := t.childByField(n, "max_count")
	maxCount := "100"
	if maxCountNode != nil {
		maxCount = normalizeIntExpression(strings.TrimSpace(t.text(maxCountNode)), 100)
	}

	bodyNode := t.childByField(n, "body")
	if bodyNode == nil {
		return t.text(n)
	}

	t.addImport("testing/quick")

	line := t.lineOf(n)

	var b strings.Builder
	fmt.Fprintf(&b, "if err := quick.Check(func%s bool {\n", params)
	oldInProperty := t.inPropertyBlock
	t.inPropertyBlock = true
	t.emitPropertyBody(&b, bodyNode, "\t")
	t.inPropertyBlock = oldInProperty

	fmt.Fprintf(&b, "}, &quick.Config{MaxCount: %s}); err != nil {\n", maxCount)
	if len(name) > 0 {
		fmt.Fprintf(&b, "\t\t%[1]s.Fatalf(\"danmuji:%[2]d property %%s failed: %%v\", %[3]q, err)\n", t.testVar, line, name)
	} else {
		fmt.Fprintf(&b, "\t\t%[1]s.Fatalf(\"danmuji:%[2]d property failed: %%v\", err)\n", t.testVar, line)
	}
	fmt.Fprintf(&b, "}\n")

	return b.String()
}

func (t *dmjTranspiler) emitPropertyBody(b *strings.Builder, n *gotreesitter.Node, indent string) {
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if t.nodeType(c) == "statement_list" {
			for j := 0; j < int(c.NamedChildCount()); j++ {
				stmt := c.NamedChild(j)
				switch t.nodeType(stmt) {
				case "expect_statement", "reject_statement":
					b.WriteString(indent)
					fmt.Fprintf(b, "if !(%s) {\n", t.emit(stmt))
					b.WriteString(indent + "\t")
					b.WriteString("return false\n")
					b.WriteString(indent)
					b.WriteString("}\n")
				default:
					t.appendIndented(b, t.emit(stmt), indent)
				}
			}
			b.WriteString(indent + "return true\n")
			return
		}
	}
	b.WriteString(indent + "return true\n")
}

func (t *dmjTranspiler) emitPollingBody(b *strings.Builder, n *gotreesitter.Node, indent string) {
	// Emit all statements in a block, converting expect/reject into bool checks.
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if t.nodeType(c) == "statement_list" {
			for j := 0; j < int(c.NamedChildCount()); j++ {
				stmt := c.NamedChild(j)
				switch t.nodeType(stmt) {
				case "expect_statement", "reject_statement":
					b.WriteString(indent)
					fmt.Fprintf(b, "if !(%s) {\n", t.emit(stmt))
					b.WriteString(indent + "\t")
					b.WriteString("succeeded = false\n")
					b.WriteString(indent)
					b.WriteString("}\n")
				default:
					t.appendIndented(b, t.emit(stmt), indent)
				}
			}
			return
		}
	}
}

func (t *dmjTranspiler) pollingDuration(durationNode *gotreesitter.Node, mode string) string {
	if durationNode == nil {
		if mode == "eventually" {
			return "5 * time.Second"
		}
		return "2 * time.Second"
	}
	return normalizeDurationExpression(strings.TrimSpace(t.text(durationNode)), "1 * time.Second")
}

