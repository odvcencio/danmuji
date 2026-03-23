package danmuji

import (
	"fmt"
	gotreesitter "github.com/odvcencio/gotreesitter"
	"strings"
)

// ---------------------------------------------------------------------------
// each_do_block → scenario-driven table test with defaults
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitEachDo(n *gotreesitter.Node) string {
	t.addImport("fmt")

	// Extract name
	nameNode := t.childByField(n, "name")
	name := "scenarios"
	if nameNode != nil {
		name = strings.Trim(t.text(nameNode), "\"'`")
	}
	structName := sanitizeTestName(name) + "Scenario"

	// Collect defaults
	defaults := make(map[string]string) // field_name → default_value_source

	// Collect scenario entries
	type scenarioEntry struct {
		fields map[string]string // field_name → value_source
	}
	var scenarios []scenarioEntry

	// All field names (for struct generation), preserving order
	allFieldsMap := make(map[string]bool)
	var allFields []string
	addField := func(f string) {
		if !allFieldsMap[f] {
			allFieldsMap[f] = true
			allFields = append(allFields, f)
		}
	}
	addField("name")

	// Walk the scenarios block to find defaults_block and scenario_entry nodes.
	scenariosBlock := t.childByField(n, "scenarios")
	if scenariosBlock != nil {
		t.walkChildren(scenariosBlock, func(child *gotreesitter.Node) {
			switch t.nodeType(child) {
			case "defaults_block":
				t.extractScenarioFields(child, func(key, val string) {
					defaults[key] = val
					addField(key)
				})
			case "scenario_entry":
				entry := scenarioEntry{fields: make(map[string]string)}
				t.extractScenarioFields(child, func(key, val string) {
					entry.fields[key] = val
					addField(key)
				})
				scenarios = append(scenarios, entry)
			}
		})
	}

	// Build body
	bodyNode := t.childByField(n, "body")

	var b strings.Builder

	// Emit struct type
	fmt.Fprintf(&b, "type %s struct {\n", structName)
	for _, f := range allFields {
		fmt.Fprintf(&b, "\t%s interface{}\n", f)
	}
	fmt.Fprintf(&b, "}\n")

	// Emit scenario slice
	fmt.Fprintf(&b, "scenarios := []%s{\n", structName)
	for idx, sc := range scenarios {
		b.WriteString("\t{")
		for i, f := range allFields {
			if i > 0 {
				b.WriteString(", ")
			}
			if val, ok := sc.fields[f]; ok {
				fmt.Fprintf(&b, "%s: %s", f, val)
			} else if defVal, ok := defaults[f]; ok {
				fmt.Fprintf(&b, "%s: %s", f, defVal)
			} else if f == "name" {
				fmt.Fprintf(&b, "%s: %q", f, fmt.Sprintf("scenario_%d", idx+1))
			} else {
				fmt.Fprintf(&b, "%s: nil", f)
			}
		}
		b.WriteString("},\n")
	}
	b.WriteString("}\n")

	// Emit iteration loop
	tv := t.testVar
	fmt.Fprintf(&b, "for _, scenario := range scenarios {\n")
	fmt.Fprintf(&b, "\t_scenarioName := fmt.Sprintf(\"%%v\", scenario.name)\n")
	fmt.Fprintf(&b, "\t%s.Run(_scenarioName, func(%s *testing.T) {\n", tv, tv)
	fmt.Fprintf(&b, "\t\t%s.Parallel()\n", tv)
	for _, f := range allFields {
		if f == "name" {
			continue
		}
		fmt.Fprintf(&b, "\t\t%s := scenario.%s\n", f, f)
		fmt.Fprintf(&b, "\t\t_ = %s\n", f)
	}

	// Emit the body
	if bodyNode != nil {
		if len(t.beforeEachHookContext) > 0 || len(t.afterEachHookContext) > 0 {
			b.WriteString(t.emitSubtestBodyWithHooks(bodyNode, "\t\t", t.beforeEachHookContext, t.afterEachHookContext))
		} else {
			b.WriteString(t.emitBlockInner(bodyNode, "\t\t"))
		}
	}

	fmt.Fprintf(&b, "\t})\n")
	b.WriteString("}\n")

	return b.String()
}

// extractScenarioFields walks a node for scenario_field children and calls fn(key, val).
// Also handles the case where a single-field scenario_entry is parsed by Go's grammar
// as a labeled_statement (e.g., { name: "ok" } → label_name: expression_statement).
func (t *dmjTranspiler) extractScenarioFields(n *gotreesitter.Node, fn func(key, val string)) {
	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		switch t.nodeType(c) {
		case "scenario_field":
			keyNode := t.childByField(c, "key")
			valNode := t.childByField(c, "value")
			if keyNode != nil && valNode != nil {
				fn(t.text(keyNode), t.emit(valNode))
			}
		case "labeled_statement":
			// Fallback: { name: "ok" } parsed as label: expression
			if c.ChildCount() >= 2 {
				label := c.Child(0) // label_name
				if label != nil && t.nodeType(label) == "label_name" {
					key := t.text(label)
					// Find the expression child (skip the ":")
					for j := 1; j < int(c.ChildCount()); j++ {
						expr := c.Child(j)
						if expr.IsNamed() {
							fn(key, t.emit(expr))
							break
						}
					}
				}
			}
		default:
			t.extractScenarioFields(c, fn)
		}
	}
}

// ---------------------------------------------------------------------------
// matrix_block → cartesian product scenario-driven test
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitMatrix(n *gotreesitter.Node) string {
	t.addImport("fmt")

	// Extract name
	nameNode := t.childByField(n, "name")
	name := "matrix"
	if nameNode != nil {
		name = strings.Trim(t.text(nameNode), "\"'`")
	}
	structName := sanitizeTestName(name) + "Scenario"

	// Collect matrix fields from the dimensions block
	type matrixDim struct {
		key    string
		values []string
	}
	var dims []matrixDim

	dimsBlock := t.childByField(n, "dimensions")
	if dimsBlock != nil {
		t.walkChildren(dimsBlock, func(child *gotreesitter.Node) {
			if t.nodeType(child) == "matrix_field" {
				keyNode := t.childByField(child, "key")
				if keyNode == nil {
					return
				}
				dim := matrixDim{key: t.text(keyNode)}
				// Walk children of matrix_field for expression values (skip braces and key)
				for j := 0; j < int(child.ChildCount()); j++ {
					gc := child.Child(j)
					if gc.IsNamed() && t.nodeType(gc) != "identifier" {
						dim.values = append(dim.values, t.emit(gc))
					}
				}
				dims = append(dims, dim)
			}
		})
	}

	// Build body
	bodyNode := t.childByField(n, "body")

	// Generate cartesian product
	type combo map[string]string
	combos := []combo{{}}
	for _, dim := range dims {
		var newCombos []combo
		for _, existing := range combos {
			for _, val := range dim.values {
				c := make(combo)
				for k, v := range existing {
					c[k] = v
				}
				c[dim.key] = val
				newCombos = append(newCombos, c)
			}
		}
		combos = newCombos
	}

	var b strings.Builder

	// Emit struct type
	fmt.Fprintf(&b, "type %s struct {\n", structName)
	for _, dim := range dims {
		fmt.Fprintf(&b, "\t%s interface{}\n", dim.key)
	}
	fmt.Fprintf(&b, "}\n")

	// Emit scenario slice
	fmt.Fprintf(&b, "scenarios := []%s{\n", structName)
	for _, c := range combos {
		b.WriteString("\t{")
		for i, dim := range dims {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%s", c[dim.key])
		}
		b.WriteString("},\n")
	}
	b.WriteString("}\n")

	// Emit iteration loop with auto-generated names
	tv := t.testVar
	fmt.Fprintf(&b, "for _, scenario := range scenarios {\n")

	// Build name from all dim values: fmt.Sprintf("%v_%v", scenario.method, scenario.auth)
	var nameFormatParts []string
	var nameArgParts []string
	for _, dim := range dims {
		nameFormatParts = append(nameFormatParts, "%v")
		nameArgParts = append(nameArgParts, "scenario."+dim.key)
	}
	nameFormat := strings.Join(nameFormatParts, "_")
	nameArgs := strings.Join(nameArgParts, ", ")
	fmt.Fprintf(&b, "\tname := fmt.Sprintf(%q, %s)\n", nameFormat, nameArgs)

	fmt.Fprintf(&b, "\t%s.Run(name, func(%s *testing.T) {\n", tv, tv)
	fmt.Fprintf(&b, "\t\t%s.Parallel()\n", tv)
	for _, dim := range dims {
		fmt.Fprintf(&b, "\t\t%s := scenario.%s\n", dim.key, dim.key)
		fmt.Fprintf(&b, "\t\t_ = %s\n", dim.key)
	}

	// Emit the body
	if bodyNode != nil {
		if len(t.beforeEachHookContext) > 0 || len(t.afterEachHookContext) > 0 {
			b.WriteString(t.emitSubtestBodyWithHooks(bodyNode, "\t\t", t.beforeEachHookContext, t.afterEachHookContext))
		} else {
			b.WriteString(t.emitBlockInner(bodyNode, "\t\t"))
		}
	}

	fmt.Fprintf(&b, "\t})\n")
	b.WriteString("}\n")

	return b.String()
}

// ---------------------------------------------------------------------------
// table_declaration → Go slice literal of anonymous struct rows
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitTable(n *gotreesitter.Node) string {
	nameNode := t.childByField(n, "name")
	tableName := "cases"
	if nameNode != nil {
		tableName = t.text(nameNode)
	}

	// Collect table rows
	var rows [][]string
	maxCols := 0
	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		if t.nodeType(c) == "table_row" {
			cells := t.extractTableRowCells(c)
			if len(cells) > maxCols {
				maxCols = len(cells)
			}
			rows = append(rows, cells)
		}
	}

	var b strings.Builder

	// Build struct field names
	var fields []string
	for i := 0; i < maxCols; i++ {
		fields = append(fields, fmt.Sprintf("col%d", i))
	}

	// Emit type and slice
	fmt.Fprintf(&b, "type %sRow struct { ", tableName)
	for i, f := range fields {
		if i > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%s interface{}", f)
	}
	b.WriteString(" }\n")
	fmt.Fprintf(&b, "%s := []%sRow{\n", tableName, tableName)
	for _, row := range rows {
		b.WriteString("\t{")
		for i, cell := range row {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(cell)
		}
		b.WriteString("},\n")
	}
	b.WriteString("}\n")
	fmt.Fprintf(&b, "_ = %s\n", tableName)

	return b.String()
}

func (t *dmjTranspiler) extractTableRowCells(row *gotreesitter.Node) []string {
	rowText := strings.TrimSpace(t.text(row))
	if strings.Contains(rowText, "|") {
		var cells []string
		var current strings.Builder
		depthParen := 0
		depthBracket := 0
		depthBrace := 0
		inSingle := false
		inDouble := false
		inBacktick := false
		escaped := false

		flush := func() {
			cell := strings.TrimSpace(current.String())
			if cell != "" {
				cells = append(cells, cell)
			}
			current.Reset()
		}

		for _, r := range rowText {
			if escaped {
				current.WriteRune(r)
				escaped = false
				continue
			}

			if inSingle || inDouble {
				current.WriteRune(r)
				if r == '\\' {
					escaped = true
					continue
				}
				if inSingle && r == '\'' {
					inSingle = false
				}
				if inDouble && r == '"' {
					inDouble = false
				}
				continue
			}

			if inBacktick {
				current.WriteRune(r)
				if r == '`' {
					inBacktick = false
				}
				continue
			}

			switch r {
			case '\'':
				inSingle = true
				current.WriteRune(r)
			case '"':
				inDouble = true
				current.WriteRune(r)
			case '`':
				inBacktick = true
				current.WriteRune(r)
			case '(':
				depthParen++
				current.WriteRune(r)
			case ')':
				if depthParen > 0 {
					depthParen--
				}
				current.WriteRune(r)
			case '[':
				depthBracket++
				current.WriteRune(r)
			case ']':
				if depthBracket > 0 {
					depthBracket--
				}
				current.WriteRune(r)
			case '{':
				depthBrace++
				current.WriteRune(r)
			case '}':
				if depthBrace > 0 {
					depthBrace--
				}
				current.WriteRune(r)
			case '|':
				if depthParen == 0 && depthBracket == 0 && depthBrace == 0 {
					flush()
					continue
				}
				current.WriteRune(r)
			default:
				current.WriteRune(r)
			}
		}

		flush()
		if len(cells) > 0 {
			return cells
		}
	}

	var cells []string
	for j := 0; j < int(row.NamedChildCount()); j++ {
		cell := row.NamedChild(j)
		cells = append(cells, t.emit(cell))
	}
	return cells
}

// ---------------------------------------------------------------------------
// each_row_block → for range iteration over table
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitEachRow(n *gotreesitter.Node) string {
	t.addImport("fmt")
	tableNode := t.childByField(n, "table")
	tableName := "cases"
	if tableNode != nil {
		tableName = t.text(tableNode)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "for _, row := range %s {\n", tableName)
	fmt.Fprintf(&b, "\t%s.Run(fmt.Sprintf(\"row_%%v\", row), func(%s *testing.T) {\n", t.testVar, t.testVar)

	// Find and emit the block body
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if t.nodeType(c) == "block" {
			if len(t.beforeEachHookContext) > 0 || len(t.afterEachHookContext) > 0 {
				b.WriteString(t.emitSubtestBodyWithHooks(c, "\t\t", t.beforeEachHookContext, t.afterEachHookContext))
			} else {
				b.WriteString(t.emitBlockInner(c, "\t\t"))
			}
			break
		}
	}

	fmt.Fprintf(&b, "\t})\n")
	b.WriteString("}\n")

	return b.String()
}
