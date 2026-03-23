package danmuji

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

type fuzzParamSpec struct {
	Name string
	Type string
}

func (t *dmjTranspiler) emitFuzz(n *gotreesitter.Node) string {
	oldTestVar := t.testVar
	t.testVar = "t"
	defer func() { t.testVar = oldTestVar }()

	nameNode := t.childByField(n, "name")
	name := "Fuzz"
	if nameNode != nil {
		name = sanitizeTestName(t.text(nameNode))
	}

	paramsNode := t.childByField(n, "params")
	paramsText := "()"
	if paramsNode != nil {
		paramsText = strings.TrimSpace(t.text(paramsNode))
	}

	params, err := parseFuzzParamSpecs(paramsText)
	if err != nil || len(params) == 0 {
		return t.text(n)
	}

	seedArgs := make([]string, 0, len(params))
	for _, param := range params {
		seed, ok := fuzzSeedLiteral(param.Type)
		if !ok {
			return t.text(n)
		}
		seedArgs = append(seedArgs, seed)
	}

	bodyNode := t.childByField(n, "body")
	if bodyNode == nil {
		return t.text(n)
	}

	t.addImport("testing")

	var b strings.Builder

	if len(t.mockDecls) > 0 {
		for _, md := range t.mockDecls {
			b.WriteString(md)
		}
		t.mockDecls = nil
	}

	fmt.Fprintf(&b, "func Fuzz%s(f *testing.F) {\n", name)
	b.WriteString(t.lineDirective(n))
	fmt.Fprintf(&b, "\tf.Add(%s)\n", strings.Join(seedArgs, ", "))
	fmt.Fprintf(&b, "\tf.Fuzz(func(t *testing.T, %s) {\n", strings.Trim(paramsText, "()"))
	b.WriteString(t.emitBlockInner(bodyNode, "\t\t"))
	fmt.Fprintf(&b, "\t})\n")
	fmt.Fprintf(&b, "}\n")

	return b.String()
}

func (t *dmjTranspiler) validateFuzzBlock(n *gotreesitter.Node) {
	paramsNode := t.childByField(n, "params")
	paramsText := "()"
	if paramsNode != nil {
		paramsText = strings.TrimSpace(t.text(paramsNode))
	}

	params, err := parseFuzzParamSpecs(paramsText)
	if err != nil {
		t.addSemanticError(n,
			"fuzz blocks require at least one named parameter",
			`fuzz "round trip text" with (input string) { expect len(input) >= 0 }`)
		return
	}
	if len(params) == 0 {
		t.addSemanticError(n,
			"fuzz blocks require at least one named parameter",
			`fuzz "round trip text" with (input string) { expect len(input) >= 0 }`)
		return
	}

	for _, param := range params {
		if _, ok := fuzzSeedLiteral(param.Type); !ok {
			t.addSemanticError(n,
				fmt.Sprintf("fuzz parameter %s uses unsupported type %s", param.Name, param.Type),
				"supported fuzz types: string, []byte, bool, byte, rune, int*, uint*, float32, float64")
			return
		}
	}
}

func parseFuzzParamSpecs(params string) ([]fuzzParamSpec, error) {
	src := "package p\nfunc _" + params + " {}"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, 0)
	if err != nil {
		return nil, err
	}
	if len(file.Decls) == 0 {
		return nil, fmt.Errorf("missing fuzz params")
	}

	fn, ok := file.Decls[0].(*ast.FuncDecl)
	if !ok || fn.Type == nil || fn.Type.Params == nil {
		return nil, fmt.Errorf("missing fuzz params")
	}

	var specs []fuzzParamSpec
	for _, field := range fn.Type.Params.List {
		if len(field.Names) == 0 {
			return nil, fmt.Errorf("fuzz params must be named")
		}
		if _, ok := field.Type.(*ast.Ellipsis); ok {
			return nil, fmt.Errorf("variadic fuzz params unsupported")
		}

		typeText, err := formatGoNode(field.Type)
		if err != nil {
			return nil, err
		}

		for _, name := range field.Names {
			if name == nil || name.Name == "_" {
				return nil, fmt.Errorf("fuzz params must be named")
			}
			specs = append(specs, fuzzParamSpec{
				Name: name.Name,
				Type: typeText,
			})
		}
	}

	return specs, nil
}

func formatGoNode(n ast.Node) (string, error) {
	var b bytes.Buffer
	if err := format.Node(&b, token.NewFileSet(), n); err != nil {
		return "", err
	}
	return b.String(), nil
}

func fuzzSeedLiteral(typeText string) (string, bool) {
	switch strings.TrimSpace(typeText) {
	case "bool":
		return "false", true
	case "string":
		return `""`, true
	case "[]byte":
		return "[]byte{}", true
	case "byte":
		return "byte(0)", true
	case "rune":
		return "rune(0)", true
	case "int":
		return "int(0)", true
	case "int8":
		return "int8(0)", true
	case "int16":
		return "int16(0)", true
	case "int32":
		return "int32(0)", true
	case "int64":
		return "int64(0)", true
	case "uint":
		return "uint(0)", true
	case "uint8":
		return "uint8(0)", true
	case "uint16":
		return "uint16(0)", true
	case "uint32":
		return "uint32(0)", true
	case "uint64":
		return "uint64(0)", true
	case "uintptr":
		return "uintptr(0)", true
	case "float32":
		return "float32(0)", true
	case "float64":
		return "float64(0)", true
	}
	return "", false
}
