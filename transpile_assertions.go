package danmuji

import (
	"fmt"
	gotreesitter "github.com/odvcencio/gotreesitter"
	"strconv"
	"strings"
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

func (t *dmjTranspiler) equalityAssertionName(left, right *gotreesitter.Node) string {
	if t.usesNumericLiteralEquality(left, right) {
		return "assert.EqualValues"
	}
	return "assert.Equal"
}

func (t *dmjTranspiler) inequalityAssertionName(left, right *gotreesitter.Node) string {
	if t.usesNumericLiteralEquality(left, right) {
		return "assert.NotEqualValues"
	}
	return "assert.NotEqual"
}

func (t *dmjTranspiler) usesNumericLiteralEquality(left, right *gotreesitter.Node) bool {
	return t.isNumericLiteral(left) || t.isNumericLiteral(right)
}

func (t *dmjTranspiler) isNumericLiteral(n *gotreesitter.Node) bool {
	if n == nil {
		return false
	}

	switch t.nodeType(n) {
	case "int_literal", "float_literal", "rune_literal", "imaginary_literal":
		return true
	case "parenthesized_expression", "unary_expression":
		for i := 0; i < int(n.NamedChildCount()); i++ {
			if t.isNumericLiteral(n.NamedChild(i)) {
				return true
			}
		}
	}

	return false
}

// expectKind classifies an expect_statement/reject_statement node by walking
// its *direct parse-tree children and fields* — never by substring-matching
// the statement's raw source text. Keyword forms like `is_nil`, `not_nil`,
// and `contains` are grammar literals that show up as distinct unnamed
// child tokens (see grammar.go's expect_statement rule); a string literal
// like "field_not_nil" that merely *contains* those letters produces no
// such child and must never be mistaken for the keyword.
type expectKind int

const (
	expectKindBare expectKind = iota
	expectKindBinary
	expectKindIsNil
	expectKindNotNil
	expectKindContains
	expectKindMessageContains
	expectKindUnorderedEqual
	expectKindIs
	expectKindMatches
	expectKindMatcher
	// expectKindEq / expectKindNeq cover the grammar's explicit
	// `expect X == Y` / `expect X != Y` alternatives. In practice Go's own
	// expression grammar absorbs `==`/`!=` into a binary_expression before
	// these alternatives get a chance to match (see expectKindBinary), but
	// the tokens are classified structurally here too in case a future
	// grammar revision makes this path reachable.
	expectKindEq
	expectKindNeq
)

func (t *dmjTranspiler) classifyExpect(n *gotreesitter.Node) expectKind {
	sawMessage := false
	sawIs := false
	for i := 0; i < int(n.ChildCount()); i++ {
		switch t.nodeType(n.Child(i)) {
		case "is_nil":
			return expectKindIsNil
		case "not_nil":
			return expectKindNotNil
		case "unordered_equal":
			return expectKindUnorderedEqual
		case "contains":
			if sawMessage {
				return expectKindMessageContains
			}
			return expectKindContains
		case "message":
			sawMessage = true
		case "is":
			sawIs = true
		case "==":
			return expectKindEq
		case "!=":
			return expectKindNeq
		}
	}
	if sawIs {
		return expectKindIs
	}
	if t.childByField(n, "match") != nil {
		return expectKindMatches
	}
	if t.childByField(n, "matcher") != nil {
		return expectKindMatcher
	}
	if actual := t.childByField(n, "actual"); actual != nil &&
		t.nodeType(actual) == "binary_expression" && actual.ChildCount() >= 3 {
		return expectKindBinary
	}
	return expectKindBare
}

// expectStatementNeedsPollingHelpers reports whether an expect_statement
// needs the shared danmujiDeepEqual/danmujiMatches/danmujiUnorderedEqual
// helper bundle, based on its actual matcher classification — never on
// whether the DSL keywords happen to appear as a substring of the
// statement's raw source text (see classifyExpect's doc comment for why
// that is unsound).
func (t *dmjTranspiler) expectStatementNeedsPollingHelpers(n *gotreesitter.Node) bool {
	switch t.classifyExpect(n) {
	case expectKindMatches, expectKindUnorderedEqual:
		return true
	default:
		return false
	}
}

// verifyStatementCallsWithArgs reports whether a verify_statement uses the
// `called with (...)` form (which needs the danmuji arg-matching helpers),
// by checking for the assertion's own literal "with" child token rather
// than searching the whole statement's source text for the words "called"
// and "with" (which could also appear inside the verify target's own
// identifier or arguments).
func (t *dmjTranspiler) verifyStatementCallsWithArgs(n *gotreesitter.Node) bool {
	assertion := t.childByField(n, "assertion")
	if assertion == nil {
		return false
	}
	for i := 0; i < int(assertion.ChildCount()); i++ {
		if t.nodeType(assertion.Child(i)) == "with" {
			return true
		}
	}
	return false
}

func (t *dmjTranspiler) emitExpectCondition(n *gotreesitter.Node) string {
	actual := t.childByField(n, "actual")
	expected := t.childByField(n, "expected")
	matcher := t.childByField(n, "matcher")
	matchNode := t.childByField(n, "match")

	if actual == nil {
		return "false"
	}
	actualText := t.emit(actual)

	switch t.classifyExpect(n) {
	case expectKindMatches:
		return fmt.Sprintf("danmujiMatches(%s, %s)", t.emitMatchBlock(matchNode), actualText)
	case expectKindUnorderedEqual:
		return fmt.Sprintf("danmujiUnorderedEqual(%s, %s)", t.emit(expected), actualText)
	case expectKindMessageContains:
		expectedText := t.emit(expected)
		t.addImport("strings")
		return fmt.Sprintf("%s != nil && strings.Contains(%s.Error(), %s)", actualText, actualText, expectedText)
	case expectKindIs:
		expectedText := t.emit(expected)
		t.addImport("errors")
		return fmt.Sprintf("errors.Is(%s, %s)", actualText, expectedText)
	case expectKindMatcher:
		matcherText := strings.TrimSpace(t.emit(matcher))
		if expected != nil {
			return fmt.Sprintf("%s(%s, %s)", matcherText, actualText, t.emit(expected))
		}
		return fmt.Sprintf("%s(%s)", matcherText, actualText)
	case expectKindIsNil:
		return fmt.Sprintf("%s == nil", actualText)
	case expectKindNotNil:
		return fmt.Sprintf("%s != nil", actualText)
	case expectKindContains:
		return fmt.Sprintf("danmujiContains(%s, %s)", actualText, t.emit(expected))
	case expectKindNeq:
		expectedText := t.emit(expected)
		if expectedText == "nil" {
			return fmt.Sprintf("%s != nil", actualText)
		}
		return fmt.Sprintf("!danmujiDeepEqual(%s, %s)", expectedText, actualText)
	case expectKindEq:
		expectedText := t.emit(expected)
		if expectedText == "nil" {
			return fmt.Sprintf("%s == nil", actualText)
		}
		return fmt.Sprintf("danmujiDeepEqual(%s, %s)", expectedText, actualText)
	case expectKindBinary:
		// actual is a binary_expression (Go absorbed "x == 5" into one node);
		// extract left/op/right from its own children.
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
	return actualText
}

func (t *dmjTranspiler) emitExpectAssertion(n *gotreesitter.Node) string {
	actual := t.childByField(n, "actual")
	expected := t.childByField(n, "expected")
	matcher := t.childByField(n, "matcher")
	matchNode := t.childByField(n, "match")

	if actual == nil {
		return t.text(n)
	}

	ld := t.lineDirective(n)
	nodeText := t.text(n)
	msg := t.expectFailureContext("expect", strings.TrimSpace(nodeText), n)
	actualText := t.emit(actual)

	switch t.classifyExpect(n) {
	case expectKindMatches:
		t.addImport("github.com/stretchr/testify/assert")
		matchText := t.emitMatchBlock(matchNode)
		var b strings.Builder
		b.WriteString(ld)
		b.WriteString("{\n")
		fmt.Fprintf(&b, "if _ok, _diff := danmujiPartialMatch(%s, %s); !_ok {\n", matchText, actualText)
		fmt.Fprintf(&b, "\tassert.Fail(%s, %s, _diff)\n", t.testVar, msg)
		b.WriteString("}\n")
		b.WriteString("}")
		return b.String()
	case expectKindUnorderedEqual:
		t.addImport("github.com/stretchr/testify/assert")
		expectedText := t.emit(expected)
		var b strings.Builder
		b.WriteString(ld)
		b.WriteString("{\n")
		fmt.Fprintf(&b, "if _ok, _diff := danmujiUnorderedEqualDetail(%s, %s); !_ok {\n", expectedText, actualText)
		fmt.Fprintf(&b, "\tassert.Fail(%s, %s, _diff)\n", t.testVar, msg)
		b.WriteString("}\n")
		b.WriteString("}")
		return b.String()
	case expectKindMessageContains:
		t.addImport("github.com/stretchr/testify/assert")
		expectedText := t.emit(expected)
		return ld + fmt.Sprintf("assert.ErrorContains(%s, %s, %s, %s)", t.testVar, actualText, expectedText, msg)
	case expectKindIs:
		t.addImport("github.com/stretchr/testify/assert")
		expectedText := t.emit(expected)
		return ld + fmt.Sprintf("assert.ErrorIs(%s, %s, %s, %s)", t.testVar, actualText, expectedText, msg)
	case expectKindMatcher:
		matcherText := strings.TrimSpace(t.emit(matcher))
		t.addImport("github.com/stretchr/testify/assert")
		if expected != nil {
			expectedText := t.emit(expected)
			return ld + fmt.Sprintf("assert.True(%s, %s(%s, %s), %s)", t.testVar, matcherText, actualText, expectedText, msg)
		}
		return ld + fmt.Sprintf("assert.True(%s, %s(%s), %s)", t.testVar, matcherText, actualText, msg)
	case expectKindIsNil:
		t.addImport("github.com/stretchr/testify/assert")
		return ld + fmt.Sprintf("assert.Nil(%s, %s, %s)", t.testVar, actualText, msg)
	case expectKindNotNil:
		t.addImport("github.com/stretchr/testify/assert")
		return ld + fmt.Sprintf("assert.NotNil(%s, %s, %s)", t.testVar, actualText, msg)
	case expectKindContains:
		t.addImport("github.com/stretchr/testify/assert")
		expectedText := t.emit(expected)
		return ld + fmt.Sprintf("assert.Contains(%s, %s, %s, %s)", t.testVar, actualText, expectedText, msg)
	case expectKindNeq:
		expectedText := t.emit(expected)
		if expectedText == "nil" {
			t.addImport("github.com/stretchr/testify/assert")
			return ld + fmt.Sprintf("assert.NotNil(%s, %s, %s)", t.testVar, actualText, msg)
		}
		t.addImport("github.com/stretchr/testify/assert")
		assertionName := t.inequalityAssertionName(actual, expected)
		return ld + fmt.Sprintf("%s(%s, %s, %s, %s)", assertionName, t.testVar, expectedText, actualText, msg)
	case expectKindEq:
		expectedText := t.emit(expected)
		if expectedText == "nil" && strings.HasSuffix(actualText, "err") {
			t.addImport("github.com/stretchr/testify/require")
			return ld + fmt.Sprintf("require.NoError(%s, %s, %s)", t.testVar, actualText, msg)
		}
		if expectedText == "nil" {
			t.addImport("github.com/stretchr/testify/assert")
			return ld + fmt.Sprintf("assert.Nil(%s, %s, %s)", t.testVar, actualText, msg)
		}
		t.addImport("github.com/stretchr/testify/assert")
		assertionName := t.equalityAssertionName(actual, expected)
		return ld + fmt.Sprintf("%s(%s, %s, %s, %s)", assertionName, t.testVar, expectedText, actualText, msg)
	case expectKindBinary:
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
			assertionName := t.equalityAssertionName(left, right)
			return ld + fmt.Sprintf("%s(%s, %s, %s, %s)", assertionName, t.testVar, rT, lT, msg)
		case "!=":
			if rT == "nil" {
				t.addImport("github.com/stretchr/testify/assert")
				return ld + fmt.Sprintf("assert.NotNil(%s, %s, %s)", t.testVar, lT, msg)
			}
			t.addImport("github.com/stretchr/testify/assert")
			assertionName := t.inequalityAssertionName(left, right)
			return ld + fmt.Sprintf("%s(%s, %s, %s, %s)", assertionName, t.testVar, rT, lT, msg)
		case "<":
			t.addImport("github.com/stretchr/testify/assert")
			return ld + fmt.Sprintf("assert.True(%s, %s < %s, %s)", t.testVar, lT, rT, msg)
		case ">":
			t.addImport("github.com/stretchr/testify/assert")
			return ld + fmt.Sprintf("assert.True(%s, %s > %s, %s)", t.testVar, lT, rT, msg)
		case "<=":
			t.addImport("github.com/stretchr/testify/assert")
			return ld + fmt.Sprintf("assert.True(%s, %s <= %s, %s)", t.testVar, lT, rT, msg)
		case ">=":
			t.addImport("github.com/stretchr/testify/assert")
			return ld + fmt.Sprintf("assert.True(%s, %s >= %s, %s)", t.testVar, lT, rT, msg)
		}
	}

	t.addImport("github.com/stretchr/testify/assert")
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

func (t *dmjTranspiler) emitMatchBlock(n *gotreesitter.Node) string {
	var parts []string
	for i := 0; i < int(n.NamedChildCount()); i++ {
		field := n.NamedChild(i)
		if t.nodeType(field) != "match_field" {
			continue
		}
		keyNode := t.childByField(field, "key")
		valueNode := t.childByField(field, "value")
		if keyNode == nil || valueNode == nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("%q: %s", t.text(keyNode), t.emitMatchValue(valueNode)))
	}
	return fmt.Sprintf("map[string]interface{}{%s}", strings.Join(parts, ", "))
}

func (t *dmjTranspiler) emitMatchValue(n *gotreesitter.Node) string {
	if n == nil {
		return "nil"
	}

	nodeText := strings.TrimSpace(t.text(n))
	expectedNode := t.childByField(n, "expected")
	switch {
	case strings.HasPrefix(nodeText, "contains ") && expectedNode != nil:
		return fmt.Sprintf("danmujiMatchContains(%s)", t.emit(expectedNode))
	case nodeText == "is_nil":
		return "danmujiMatchNil()"
	case nodeText == "not_nil":
		return "danmujiMatchNotNil()"
	}

	if t.nodeType(n) == "match_value" {
		for i := 0; i < int(n.NamedChildCount()); i++ {
			return t.emit(n.NamedChild(i))
		}
	}

	return t.emit(n)
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
	// name is arbitrary user text (e.g. `eventually "50% success rate"
	// within 5s`) and MUST NOT be spliced into the format string itself —
	// a literal "%" in name would reach t.Errorf as a bogus verb (go vet:
	// "possible formatting directive in Errorf call"). Pass it as its own
	// %s argument instead.
	fmt.Fprintf(&b, "\t\t%[1]s.Errorf(\"danmuji:%[2]d %[3]s check %%s failed after %[4]s\", %[5]s)\n",
		t.testVar, line, mode, timeout, strconv.Quote(name))
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

	if !t.propertyBodyCanFail(bodyNode) {
		t.addSemanticError(n,
			fmt.Sprintf("property %q has no expect, reject, or return statement — it can never fail, so quick.Check would vacuously pass without checking anything", name),
			`property "name" (x int) { expect x + 0 == x }`)
		return ""
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
			lastWasReturn := false
			stmtCount := int(c.NamedChildCount())
			for j := 0; j < stmtCount; j++ {
				stmt := c.NamedChild(j)
				lastWasReturn = t.nodeType(stmt) == "return_statement"
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
			// Only append the fallthrough "return true" when the body
			// didn't already end in an explicit return: appending one
			// unconditionally produced dead code that `go vet` flags
			// whenever the last statement was itself a return.
			if !lastWasReturn {
				b.WriteString(indent + "return true\n")
			}
			return
		}
	}
	b.WriteString(indent + "return true\n")
}

// propertyBodyCanFail reports whether a property block's body contains at
// least one path that can return false: an expect/reject statement (which
// compiles to `if !(...) { return false }`) or an explicit return
// statement. A property with neither always reports success regardless of
// what testing/quick throws at it — silently vacuous, exactly the kind of
// "test that can never fail" this project exists to catch.
func (t *dmjTranspiler) propertyBodyCanFail(n *gotreesitter.Node) bool {
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if t.nodeType(c) != "statement_list" {
			continue
		}
		for j := 0; j < int(c.NamedChildCount()); j++ {
			switch t.nodeType(c.NamedChild(j)) {
			case "expect_statement", "reject_statement", "return_statement":
				return true
			}
		}
		return false
	}
	return false
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
