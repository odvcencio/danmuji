package danmuji

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ---------------------------------------------------------------------------
// mock → struct with call counters + methods (package-level)
// ---------------------------------------------------------------------------

type mockMethodInfo struct {
	name       string
	params     string
	returnType string
	defaultVal string
}

func (t *dmjTranspiler) parseMockMethod(n *gotreesitter.Node) mockMethodInfo {
	info := mockMethodInfo{}
	if name := t.childByField(n, "name"); name != nil {
		info.name = t.text(name)
	}
	if params := t.childByField(n, "parameters"); params != nil {
		info.params = t.text(params)
	}
	if ret := t.childByField(n, "return_type"); ret != nil {
		info.returnType = t.text(ret)
	}
	if def := t.childByField(n, "default_value"); def != nil {
		info.defaultVal = t.text(def)
	}
	return info
}

// buildMockDecl generates the Go struct type + methods string for a mock_declaration
// node. The result is meant to be emitted at package level.
func (t *dmjTranspiler) buildMockDecl(n *gotreesitter.Node) string {
	nameNode := t.childByField(n, "name")
	if nameNode == nil {
		return t.text(n)
	}
	mockName := t.text(nameNode)
	structName := "mock" + mockName

	var methods []mockMethodInfo
	// Walk block children looking for mock_method nodes.
	// The block may contain them directly or inside a statement_list.
	t.findMockMethods(n, &methods)

	var b strings.Builder

	// Struct with call counters
	fmt.Fprintf(&b, "type %s struct {\n", structName)
	for _, m := range methods {
		fmt.Fprintf(&b, "\t%sCalls int\n", m.name)
		if m.returnType != "" {
			fmt.Fprintf(&b, "\t%sResult %s\n", m.name, m.returnType)
		}
	}
	fmt.Fprintf(&b, "}\n\n")

	// Methods
	for _, m := range methods {
		fmt.Fprintf(&b, "func (m *%s) %s%s", structName, m.name, m.params)
		if m.returnType != "" {
			fmt.Fprintf(&b, " %s", m.returnType)
		}
		fmt.Fprintf(&b, " {\n")
		fmt.Fprintf(&b, "\tm.%sCalls++\n", m.name)
		if m.defaultVal != "" {
			fmt.Fprintf(&b, "\treturn %s\n", m.defaultVal)
		} else if m.returnType != "" {
			fmt.Fprintf(&b, "\treturn m.%sResult\n", m.name)
		}
		fmt.Fprintf(&b, "}\n\n")
	}

	return b.String()
}

// findMockMethods recursively finds mock_method nodes under n.
func (t *dmjTranspiler) findMockMethods(n *gotreesitter.Node, out *[]mockMethodInfo) {
	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		if t.nodeType(c) == "mock_method" {
			*out = append(*out, t.parseMockMethod(c))
		} else {
			t.findMockMethods(c, out)
		}
	}
}

// ---------------------------------------------------------------------------
// lifecycle hooks
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitLifecycleHook(n *gotreesitter.Node) string {
	nodeText := t.text(n)
	isAfter := strings.HasPrefix(strings.TrimSpace(nodeText), "after")

	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if t.nodeType(c) == "block" {
			blockContent := t.emitTestBody(c)
			if isAfter {
				return fmt.Sprintf("%s.Cleanup(func() %s)", t.testVar, blockContent)
			}
			// before each: inline the block contents (strip outer braces)
			inner := strings.TrimSpace(blockContent)
			if strings.HasPrefix(inner, "{") && strings.HasSuffix(inner, "}") {
				inner = inner[1 : len(inner)-1]
			}
			return inner
		}
	}
	return t.text(n)
}

// ---------------------------------------------------------------------------
// verify → call count assertion
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitVerify(n *gotreesitter.Node) string {
	target := t.childByField(n, "target")
	assertion := t.childByField(n, "assertion")
	if target == nil || assertion == nil {
		return t.text(n)
	}
	targetText := t.emit(target)
	assertText := t.text(assertion)

	if strings.Contains(assertText, "not_called") {
		return fmt.Sprintf("if %sCalls != 0 { %s.Errorf(\"expected %%s not called, got %%d calls\", %q, %sCalls) }",
			targetText, t.testVar, targetText, targetText)
	}
	if strings.Contains(assertText, "called") && strings.Contains(assertText, "times") {
		parts := strings.Fields(assertText)
		count := "0"
		for i, p := range parts {
			if p == "called" && i+1 < len(parts) {
				count = parts[i+1]
				break
			}
		}
		return fmt.Sprintf("if %sCalls != %s { %s.Errorf(\"expected %%d calls to %%s, got %%d\", %s, %q, %sCalls) }",
			targetText, count, t.testVar, count, targetText, targetText)
	}
	return t.text(n)
}

// ---------------------------------------------------------------------------
// needs_block → testcontainers setup
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitNeedsBlock(n *gotreesitter.Node) string {
	t.addImport("context")
	t.addImport("github.com/docker/go-connections/nat")
	t.addImport("github.com/stretchr/testify/require")
	t.addImport("github.com/testcontainers/testcontainers-go")
	t.addImport("github.com/testcontainers/testcontainers-go/wait")

	serviceNode := t.childByField(n, "service")
	nameNode := t.childByField(n, "name")
	if serviceNode == nil || nameNode == nil {
		return t.text(n)
	}
	serviceType := strings.TrimSpace(t.text(serviceNode))
	varName := strings.TrimSpace(t.text(nameNode))

	serviceImage := ""
	servicePort := ""
	waitForPort := true

	switch serviceType {
	case "postgres":
		serviceImage = "postgres:15"
		servicePort = "5432/tcp"
	case "redis":
		serviceImage = "redis:7"
		servicePort = "6379/tcp"
	case "mysql":
		serviceImage = "mysql:8"
		servicePort = "3306/tcp"
	case "kafka":
		serviceImage = "confluentinc/confluent-local:7.5.0"
		servicePort = "9092/tcp"
	case "mongo":
		serviceImage = "mongo:7"
		servicePort = "27017/tcp"
	case "rabbitmq":
		serviceImage = "rabbitmq:3-management"
		servicePort = "5672/tcp"
	case "nats":
		serviceImage = "nats:2"
		servicePort = "4222/tcp"
	case "container":
		serviceImage = "alpine:latest"
		waitForPort = false
	default:
		return fmt.Sprintf("// unsupported needs service type: %s", serviceType)
	}

	tv := t.testVar
	var b strings.Builder
	fmt.Fprintf(&b, "{\n")
	fmt.Fprintf(&b, "\tctx := context.Background()\n")
	fmt.Fprintf(&b, "\t%sReq := testcontainers.ContainerRequest{\n", varName)
	fmt.Fprintf(&b, "\t\tImage:  %q,\n", serviceImage)
	fmt.Fprintf(&b, "\t\tExposedPorts: []string{},\n")
	if waitForPort {
		fmt.Fprintf(&b, "\t\tWaitingFor:   wait.ForListeningPort(nat.Port(%q)),\n", servicePort)
	}
	fmt.Fprintf(&b, "\t}\n")
	fmt.Fprintf(&b, "\t%sContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{\n", varName)
	fmt.Fprintf(&b, "\t\tContainerRequest: %sReq,\n", varName)
	fmt.Fprintf(&b, "\t\tStarted:        true,\n")
	fmt.Fprintf(&b, "\t})\n")
	fmt.Fprintf(&b, "\trequire.NoError(%s, err)\n", tv)
	fmt.Fprintf(&b, "\t%s.Cleanup(func() { _ = %sContainer.Terminate(ctx) })\n", tv, varName)
	if waitForPort {
		fmt.Fprintf(&b, "\t%sEndpoint, err := %sContainer.Endpoint(ctx, nat.Port(%q))\n", varName, varName, servicePort)
		fmt.Fprintf(&b, "\trequire.NoError(%s, err)\n", tv)
		fmt.Fprintf(&b, "\t_ = %sEndpoint\n", varName)
	}
	fmt.Fprintf(&b, "}\n")

	return b.String()
}

// ---------------------------------------------------------------------------
// load_block → func TestLoadXxx(t *testing.T) with vegeta attack
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitLoad(n *gotreesitter.Node) string {
	t.addImport("time")
	t.addImport("github.com/tsenart/vegeta/v12/lib")

	// Extract name
	nameNode := t.childByField(n, "name")
	name := "Load"
	if nameNode != nil {
		name = sanitizeTestName(t.text(nameNode))
	}

	// Walk the body block's children for load_config, target_block, then_block
	bodyNode := t.childByField(n, "body")
	if bodyNode == nil {
		return t.text(n)
	}

	rate := "10"
	duration := "5"
	rampup := "0"
	concurrency := "1"
	method := "GET"
	url := `"http://localhost"`
	var thenBlocks []*gotreesitter.Node

	t.walkChildren(bodyNode, func(child *gotreesitter.Node) {
		switch t.nodeType(child) {
		case "load_config":
			configText := t.text(child)
			// Extract the value (everything after the keyword)
			key, val := t.parseLoadConfig(configText)
			switch key {
			case "rate":
				if val != "" {
					rate = val
				}
			case "duration":
				if val != "" {
					duration = val
				}
			case "rampup":
				if val != "" {
					rampup = val
				}
			case "concurrency":
				if val != "" {
					concurrency = val
				}
			}
		case "target_block":
			if m := t.childByField(child, "method"); m != nil {
				method = strings.ToUpper(t.text(m))
			}
			if u := t.childByField(child, "url"); u != nil {
				url = t.text(u)
			}
		case "then_block":
			thenBlocks = append(thenBlocks, child)
		}
	})

	var b strings.Builder

	// Build constraint
	fmt.Fprintf(&b, "//go:build e2e\n\n")

	// Function signature
	fmt.Fprintf(&b, "func TestLoad%s(t *testing.T) {\n", name)
	b.WriteString(t.lineDirective(n))

	// Vegeta setup
	fmt.Fprintf(&b, "\trate := vegeta.Rate{Freq: %s, Per: time.Second}\n", normalizeRateExpression(rate))
	fmt.Fprintf(&b, "\tduration := %s\n", normalizeDurationExpression(duration, "5 * time.Second"))
	fmt.Fprintf(&b, "\trampup := %s\n", normalizeDurationExpression(rampup, "0"))
	fmt.Fprintf(&b, "\tattackDuration := duration + rampup\n")
	fmt.Fprintf(&b, "\tattacker := vegeta.NewAttacker(vegeta.Workers(%s), vegeta.MaxWorkers(%s))\n", normalizeConcurrencyExpression(concurrency), normalizeConcurrencyExpression(concurrency))
	fmt.Fprintf(&b, "\tvar metrics vegeta.Metrics\n")
	fmt.Fprintf(&b, "\ttargeter := vegeta.NewStaticTargeter(vegeta.Target{\n")
	fmt.Fprintf(&b, "\t\tMethod: %q,\n", method)
	fmt.Fprintf(&b, "\t\tURL:    %s,\n", url)
	fmt.Fprintf(&b, "\t})\n")
	fmt.Fprintf(&b, "\tfor res := range attacker.Attack(targeter, rate, attackDuration, %q) {\n", name)
	fmt.Fprintf(&b, "\t\tmetrics.Add(res)\n")
	fmt.Fprintf(&b, "\t}\n")
	fmt.Fprintf(&b, "\tmetrics.Close()\n")

	// Emit then blocks
	for _, tb := range thenBlocks {
		b.WriteString("\t")
		b.WriteString(t.emitBDDBlock(tb, "then"))
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "}\n")

	return b.String()
}

func (t *dmjTranspiler) parseLoadConfig(text string) (string, string) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return "", ""
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], strings.Join(parts[1:], " ")
}

func normalizeRateExpression(raw string) string {
	return normalizeIntExpression(raw, 10)
}

func normalizeDurationExpression(raw string, fallback string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return fallback
	}
	if matchDurationUnit.MatchString(v) {
		return v
	}
	if matchInt.MatchString(v) {
		return matchInt.ReplaceAllString(v, "$1 * time.Second")
	}
	if strings.Contains(v, "time.") || strings.Contains(v, "*") || strings.Contains(v, "/") || matchIdentifier.MatchString(v) {
		return v
	}
	return fallback
}

func normalizeConcurrencyExpression(raw string) string {
	return normalizeIntExpression(raw, 1)
}

func normalizeIntExpression(raw string, fallback int) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return strconv.Itoa(fallback)
	}
	if matchInt.MatchString(v) {
		return matchInt.FindStringSubmatch(v)[1]
	}
	if matchIdentifier.MatchString(v) {
		return v
	}
	return strconv.Itoa(fallback)
}

var (
	matchInt          = regexp.MustCompile(`^([0-9]+)$`)
	matchIdentifier   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	matchDurationUnit = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)\s*(ns|us|µs|ms|s|m|h)$`)
)

const syncBufferHelper = `
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
`

var pollingAssertionHelpers = `
func danmujiDeepEqual(expected, actual interface{}) bool {
	return reflect.DeepEqual(expected, actual)
}

func danmujiContains(actual, expected interface{}) (found bool) {
	if actual == nil || expected == nil {
		return false
	}
	defer func() {
		if recover() != nil {
			found = false
		}
	}()

	if s, ok := actual.(string); ok {
		needle, ok := expected.(string)
		if !ok {
			return false
		}
		return strings.Contains(s, needle)
	}

	rv := reflect.ValueOf(actual)
	switch rv.Kind() {
	case reflect.Array, reflect.Slice:
		for i := 0; i < rv.Len(); i++ {
			if danmujiDeepEqual(rv.Index(i).Interface(), expected) {
				return true
			}
		}
	case reflect.Map:
		if rv.MapIndex(reflect.ValueOf(expected)).IsValid() {
			return true
		}
	}
	return false
}
`

// ---------------------------------------------------------------------------
// injectImports adds all collected import paths into the existing import block
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) injectImports(code string) string {
	if len(t.neededImports) == 0 {
		return code
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", code, parser.ParseComments)
	if err != nil {
		return code
	}

	// Find existing imports to avoid duplicates.
	existing := map[string]bool{}
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		for _, spec := range gd.Specs {
			is := spec.(*ast.ImportSpec)
			if is.Path != nil {
				existing[strings.Trim(is.Path.Value, "\"")] = true
			}
		}
	}

	var imports []string
	for pkg := range t.neededImports {
		if existing[pkg] {
			continue
		}
		imports = append(imports, pkg)
	}
	if len(imports) == 0 {
		return code
	}
	sort.Strings(imports)

	var importDecl *ast.GenDecl
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		importDecl = gd
		break
	}

	if importDecl == nil {
		importDecl = &ast.GenDecl{
			Tok:   token.IMPORT,
			Specs: []ast.Spec{},
		}
		file.Decls = append([]ast.Decl{importDecl}, file.Decls...)
	}

	for _, path := range imports {
		importDecl.Specs = append(importDecl.Specs, &ast.ImportSpec{Path: &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(path)}})
	}

	var buf strings.Builder
	if err := format.Node(&buf, fset, file); err != nil {
		return code
	}

	return buf.String()
}

