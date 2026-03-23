package danmuji

import (
	"fmt"
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

type factoryField struct {
	key   string
	value *gotreesitter.Node
}

type factoryDefinition struct {
	name     string
	defaults []factoryField
	traits   map[string][]factoryField
}

func (t *dmjTranspiler) collectFactoryDecl(n *gotreesitter.Node) {
	nameNode := t.childByField(n, "name")
	bodyNode := t.childByField(n, "body")
	if nameNode == nil || bodyNode == nil {
		t.addSemanticError(n, "factory must declare a name and body", `factory User { defaults { Name: "alice" } }`)
		return
	}

	name := strings.TrimSpace(t.text(nameNode))
	if t.factories == nil {
		t.factories = make(map[string]*factoryDefinition)
	}
	if _, exists := t.factories[name]; exists {
		t.addSemanticError(n, fmt.Sprintf("factory %s is already declared", name), "")
		return
	}

	def := &factoryDefinition{
		name:   name,
		traits: make(map[string][]factoryField),
	}

	var defaultsSeen bool
	var statementList *gotreesitter.Node
	for i := 0; i < int(bodyNode.NamedChildCount()); i++ {
		child := bodyNode.NamedChild(i)
		if t.nodeType(child) == "statement_list" {
			statementList = child
			break
		}
	}

	if statementList != nil {
		for i := 0; i < int(statementList.NamedChildCount()); i++ {
			child := statementList.NamedChild(i)
			switch t.nodeType(child) {
			case "defaults_block":
				defaultsSeen = true
				def.defaults = t.collectFactoryFields(child)
			case "factory_trait_block":
				traitNameNode := t.childByField(child, "name")
				if traitNameNode == nil {
					t.addSemanticError(child, "trait must declare a name", `trait admin { Role: "admin" }`)
					continue
				}
				traitName := strings.TrimSpace(t.text(traitNameNode))
				if _, exists := def.traits[traitName]; exists {
					t.addSemanticError(child, fmt.Sprintf("factory %s already defines trait %s", name, traitName), "")
					continue
				}
				body := t.childByField(child, "body")
				def.traits[traitName] = t.collectFactoryFields(body)
			}
		}
	}

	if !defaultsSeen {
		t.addSemanticError(n, fmt.Sprintf("factory %s must declare defaults", name), `factory User { defaults { Name: "alice" } }`)
	}

	t.factories[name] = def
}

func (t *dmjTranspiler) collectFactoryFields(n *gotreesitter.Node) []factoryField {
	var fields []factoryField
	if n == nil {
		return fields
	}

	t.extractScenarioFieldNodes(n, func(key string, value *gotreesitter.Node) {
		fields = append(fields, factoryField{key: key, value: value})
	})

	return fields
}

func (t *dmjTranspiler) extractScenarioFieldNodes(n *gotreesitter.Node, fn func(key string, value *gotreesitter.Node)) {
	if n == nil {
		return
	}

	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		switch t.nodeType(c) {
		case "scenario_field":
			keyNode := t.childByField(c, "key")
			valNode := t.childByField(c, "value")
			if keyNode != nil && valNode != nil {
				fn(t.text(keyNode), valNode)
			}
		case "labeled_statement":
			if c.ChildCount() < 2 {
				continue
			}
			label := c.Child(0)
			if label == nil || t.nodeType(label) != "label_name" {
				continue
			}
			for j := 1; j < int(c.ChildCount()); j++ {
				expr := c.Child(j)
				if expr.IsNamed() {
					fn(t.text(label), expr)
					break
				}
			}
		default:
			t.extractScenarioFieldNodes(c, fn)
		}
	}
}

func (t *dmjTranspiler) validateFactoryUsage() {
	for _, buildExpr := range t.pendingBuilds {
		if buildExpr == nil {
			continue
		}
		nameNode := t.childByField(buildExpr, "name")
		if nameNode == nil {
			continue
		}

		name := strings.TrimSpace(t.text(nameNode))
		def, ok := t.factories[name]
		if !ok {
			t.addSemanticError(buildExpr, fmt.Sprintf("build references unknown factory %s", name), fmt.Sprintf("factory %s { defaults { Field: value } }", name))
			continue
		}

		traitsNode := t.childByField(buildExpr, "traits")
		if traitsNode == nil {
			continue
		}
		for _, traitName := range t.traitNames(traitsNode) {
			if _, ok := def.traits[traitName]; !ok {
				t.addSemanticError(buildExpr, fmt.Sprintf("factory %s does not define trait %s", name, traitName), "")
			}
		}
	}
}

func (t *dmjTranspiler) traitNames(n *gotreesitter.Node) []string {
	if n == nil {
		return nil
	}

	var names []string
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		if t.nodeType(child) == "identifier" {
			names = append(names, strings.TrimSpace(t.text(child)))
		}
	}
	return names
}

func (t *dmjTranspiler) emitBuildExpression(n *gotreesitter.Node) string {
	nameNode := t.childByField(n, "name")
	if nameNode == nil {
		return t.text(n)
	}

	name := strings.TrimSpace(t.text(nameNode))
	def, ok := t.factories[name]
	if !ok {
		return fmt.Sprintf("%s{}", name)
	}

	values := make(map[string]string)
	var order []string
	apply := func(fields []factoryField) {
		for _, field := range fields {
			if _, exists := values[field.key]; !exists {
				order = append(order, field.key)
			}
			values[field.key] = t.emit(field.value)
		}
	}

	apply(def.defaults)
	for _, traitName := range t.traitNames(t.childByField(n, "traits")) {
		apply(def.traits[traitName])
	}
	apply(t.collectFactoryFields(t.childByField(n, "overrides")))

	if len(order) == 0 {
		return fmt.Sprintf("%s{}", name)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s{", name)
	for i, key := range order {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s: %s", key, values[key])
	}
	b.WriteString("}")
	return b.String()
}
