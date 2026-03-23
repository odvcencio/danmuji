package danmuji

import (
	"fmt"
	gotreesitter "github.com/odvcencio/gotreesitter"
	"regexp"
	"sort"
	"strconv"
	"strings"
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
		fmt.Fprintf(&b, "\t%sArgs [][]interface{}\n", m.name)
		if m.returnType != "" {
			fmt.Fprintf(&b, "\t%sResult %s\n", m.name, m.returnType)
		}
	}
	fmt.Fprintf(&b, "}\n\n")

	// Methods
	for _, m := range methods {
		paramNames := extractParamNames(m.params)

		fmt.Fprintf(&b, "func (m *%s) %s%s", structName, m.name, m.params)
		if m.returnType != "" {
			fmt.Fprintf(&b, " %s", m.returnType)
		}
		fmt.Fprintf(&b, " {\n")
		fmt.Fprintf(&b, "\tm.%sCalls++\n", m.name)
		argsSlice := "[]interface{}{"
		for i, pn := range paramNames {
			if i > 0 {
				argsSlice += ", "
			}
			argsSlice += pn
		}
		argsSlice += "}"
		fmt.Fprintf(&b, "\tm.%sArgs = append(m.%sArgs, %s)\n", m.name, m.name, argsSlice)
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
	if strings.Contains(assertText, "called") && strings.Contains(assertText, "with") {
		argsExpr := ""
		if open := strings.Index(assertText, "("); open >= 0 {
			if close := strings.LastIndex(assertText, ")"); close > open {
				argsExpr = strings.TrimSpace(assertText[open+1 : close])
			}
		}
		expectedArgs := "[]interface{}{}"
		if argsExpr != "" {
			expectedArgs = fmt.Sprintf("[]interface{}{%s}", argsExpr)
		}
		return fmt.Sprintf(`{
	_expectedArgs := %s
	_matched := false
	for _, _callArgs := range %sArgs {
		if danmujiDeepEqual(_expectedArgs, _callArgs) {
			_matched = true
			break
		}
	}
	if !_matched {
		%s.Errorf("expected call to %%s with %%v, got %%v", %q, _expectedArgs, %sArgs)
	}
}`,
			expectedArgs, targetText, t.testVar, targetText, targetText)
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
	serviceNode := t.childByField(n, "service")
	nameNode := t.childByField(n, "name")
	if serviceNode == nil || nameNode == nil {
		return t.text(n)
	}
	serviceType := strings.TrimSpace(t.text(serviceNode))
	varName := strings.TrimSpace(t.text(nameNode))
	tv := t.testVar

	if serviceType == "tempdir" {
		return fmt.Sprintf("%s := %s.TempDir()\n_ = %s\n", varName, tv, varName)
	}

	if serviceType == "http" {
		bodyNode := t.childByField(n, "body")
		handlerExpr := ""
		if bodyNode != nil {
			t.walkChildren(bodyNode, func(child *gotreesitter.Node) {
				if handlerExpr != "" || t.nodeType(child) != "handler_directive" {
					return
				}
				if valueNode := t.childByField(child, "value"); valueNode != nil {
					handlerExpr = t.emit(valueNode)
				}
			})
		}
		if handlerExpr == "" {
			return "// needs http requires a handler directive\n"
		}
		t.addImport("net/http/httptest")
		var b strings.Builder
		fmt.Fprintf(&b, "%s := httptest.NewServer(%s)\n", varName, handlerExpr)
		fmt.Fprintf(&b, "%s.Cleanup(%s.Close)\n", tv, varName)
		fmt.Fprintf(&b, "_ = %s\n", varName)
		return b.String()
	}

	t.addImport("context")
	t.addImport("github.com/docker/go-connections/nat")
	t.addImport("github.com/stretchr/testify/require")
	t.addImport("github.com/testcontainers/testcontainers-go")
	t.addImport("github.com/testcontainers/testcontainers-go/wait")

	serviceImage := ""
	servicePort := ""
	waitForPort := true
	config := map[string]string{}
	if bodyNode := t.childByField(n, "body"); bodyNode != nil {
		t.extractScenarioFields(bodyNode, func(key, val string) {
			config[key] = val
		})
	}

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

	var b strings.Builder
	ctxName := varName + "Ctx"
	reqName := varName + "Req"
	errName := varName + "Err"
	containerName := varName + "Container"
	endpointName := varName + "Endpoint"
	fmt.Fprintf(&b, "%s := context.Background()\n", ctxName)
	fmt.Fprintf(&b, "%s := testcontainers.ContainerRequest{\n", reqName)
	fmt.Fprintf(&b, "\tImage: %q,\n", serviceImage)
	if waitForPort {
		fmt.Fprintf(&b, "\tExposedPorts: []string{%q},\n", servicePort)
	} else {
		fmt.Fprintf(&b, "\tExposedPorts: []string{},\n")
	}
	if waitForPort {
		fmt.Fprintf(&b, "\tWaitingFor: wait.ForListeningPort(nat.Port(%q)),\n", servicePort)
	}
	fmt.Fprintf(&b, "}\n")
	if serviceType == "postgres" {
		var envEntries []string
		if password, ok := config["password"]; ok {
			envEntries = append(envEntries, fmt.Sprintf(`"POSTGRES_PASSWORD": fmt.Sprint(%s)`, password))
		}
		if database, ok := config["database"]; ok {
			envEntries = append(envEntries, fmt.Sprintf(`"POSTGRES_DB": fmt.Sprint(%s)`, database))
		}
		if user, ok := config["user"]; ok {
			envEntries = append(envEntries, fmt.Sprintf(`"POSTGRES_USER": fmt.Sprint(%s)`, user))
		}
		if len(envEntries) > 0 {
			t.addImport("fmt")
			fmt.Fprintf(&b, "%s.Env = map[string]string{%s}\n", reqName, strings.Join(envEntries, ", "))
		}
	}
	fmt.Fprintf(&b, "%s, %s := testcontainers.GenericContainer(%s, testcontainers.GenericContainerRequest{\n", containerName, errName, ctxName)
	fmt.Fprintf(&b, "\tContainerRequest: %s,\n", reqName)
	fmt.Fprintf(&b, "\tStarted: true,\n")
	fmt.Fprintf(&b, "})\n")
	fmt.Fprintf(&b, "require.NoError(%s, %s)\n", tv, errName)
	fmt.Fprintf(&b, "%s.Cleanup(func() { _ = %s.Terminate(%s) })\n", tv, containerName, ctxName)
	fmt.Fprintf(&b, "_ = %s\n", containerName)
	if waitForPort {
		fmt.Fprintf(&b, "%s, %s := %s.Endpoint(%s, %q)\n", endpointName, errName, containerName, ctxName, servicePort)
		fmt.Fprintf(&b, "require.NoError(%s, %s)\n", tv, errName)
		fmt.Fprintf(&b, "_ = %s\n", endpointName)
	}

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
		return durationLiteralToGo(v)
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

const httpTestHelpers = `
type danmujiHTTPHelperSet struct{}

var danmujiHTTP danmujiHTTPHelperSet

func (danmujiHTTPHelperSet) Request(method, target string, body interface{}) *http.Request {
	reader, contentType := danmujiHTTPBody(body)
	req := httptest.NewRequest(method, target, reader)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req
}

func (h danmujiHTTPHelperSet) GET(target string) *http.Request {
	return h.Request(http.MethodGet, target, nil)
}

func (h danmujiHTTPHelperSet) POST(target string, body interface{}) *http.Request {
	return h.Request(http.MethodPost, target, body)
}

func (h danmujiHTTPHelperSet) PUT(target string, body interface{}) *http.Request {
	return h.Request(http.MethodPut, target, body)
}

func (h danmujiHTTPHelperSet) PATCH(target string, body interface{}) *http.Request {
	return h.Request(http.MethodPatch, target, body)
}

func (h danmujiHTTPHelperSet) DELETE(target string, body interface{}) *http.Request {
	return h.Request(http.MethodDelete, target, body)
}

func (danmujiHTTPHelperSet) Serve(handler http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func danmujiHTTPBody(body interface{}) (io.Reader, string) {
	switch v := body.(type) {
	case nil:
		return nil, ""
	case string:
		return strings.NewReader(v), "text/plain; charset=utf-8"
	case []byte:
		return bytes.NewReader(v), "application/octet-stream"
	case io.Reader:
		return v, ""
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			panic(err)
		}
		return bytes.NewReader(encoded), "application/json"
	}
}
`

const wsTestHelpers = `
type danmujiWSHelperSet struct{}

var danmujiWS danmujiWSHelperSet

type danmujiWSConn struct {
	conn        *websocket.Conn
	lastMessage []byte
}

func (danmujiWSHelperSet) Dial(target interface{}, path string) *danmujiWSConn {
	url := danmujiWSURL(target, path)
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		panic(fmt.Sprintf("danmujiWS.Dial(%s): %v", url, err))
	}
	return &danmujiWSConn{conn: conn}
}

func (c *danmujiWSConn) Close() {
	if c == nil || c.conn == nil {
		return
	}
	_ = c.conn.Close()
}

func (c *danmujiWSConn) SendBinary(payload []byte) {
	if err := c.conn.WriteMessage(websocket.BinaryMessage, payload); err != nil {
		panic(fmt.Sprintf("danmujiWS.SendBinary: %v", err))
	}
}

func (c *danmujiWSConn) SendText(payload string) {
	if err := c.conn.WriteMessage(websocket.TextMessage, []byte(payload)); err != nil {
		panic(fmt.Sprintf("danmujiWS.SendText: %v", err))
	}
}

func (c *danmujiWSConn) ReadBinary(timeout time.Duration) []byte {
	return c.readMessage(websocket.BinaryMessage, timeout)
}

func (c *danmujiWSConn) ReadText(timeout time.Duration) string {
	return string(c.readMessage(websocket.TextMessage, timeout))
}

func (c *danmujiWSConn) LastMessage() []byte {
	if c == nil || len(c.lastMessage) == 0 {
		return nil
	}
	return append([]byte(nil), c.lastMessage...)
}

func (c *danmujiWSConn) readMessage(expectedType int, timeout time.Duration) []byte {
	if c == nil || c.conn == nil {
		panic("danmujiWS connection is nil")
	}
	if err := c.conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		panic(fmt.Sprintf("danmujiWS.SetReadDeadline: %v", err))
	}
	messageType, payload, err := c.conn.ReadMessage()
	if err != nil {
		panic(fmt.Sprintf("danmujiWS.ReadMessage: %v", err))
	}
	if messageType != expectedType {
		panic(fmt.Sprintf("danmujiWS expected message type %d, got %d", expectedType, messageType))
	}
	c.lastMessage = append([]byte(nil), payload...)
	return append([]byte(nil), payload...)
}

func danmujiWSURL(target interface{}, path string) string {
	var base string
	switch v := target.(type) {
	case string:
		base = v
	case *httptest.Server:
		base = v.URL
	default:
		panic(fmt.Sprintf("danmujiWS target %T is unsupported", target))
	}

	if strings.HasPrefix(base, "http://") {
		base = "ws://" + strings.TrimPrefix(base, "http://")
	} else if strings.HasPrefix(base, "https://") {
		base = "wss://" + strings.TrimPrefix(base, "https://")
	}

	if path == "" {
		return base
	}
	if strings.HasPrefix(path, "ws://") || strings.HasPrefix(path, "wss://") {
		return path
	}
	if strings.HasSuffix(base, "/") && strings.HasPrefix(path, "/") {
		return base + path[1:]
	}
	if !strings.HasSuffix(base, "/") && !strings.HasPrefix(path, "/") {
		return base + "/" + path
	}
	return base + path
}
`

const awaitHelpers = `
func danmujiAwait[T any](ch <-chan T, timeout time.Duration, t testing.TB) T {
	select {
	case value := <-ch:
		return value
	case <-time.After(timeout):
		var zero T
		t.Fatalf("await timed out after %s", timeout)
		return zero
	}
}
`

const grpcTestHelpers = `
type danmujiGRPCHelperSet struct{}

var danmujiGRPC danmujiGRPCHelperSet

type danmujiGRPCConn struct {
	conn     *grpc.ClientConn
	server   *grpc.Server
	listener *bufconn.Listener
}

func (danmujiGRPCHelperSet) Bufconn(register func(*grpc.Server)) *danmujiGRPCConn {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	register(server)

	go func() {
		if err := server.Serve(listener); err != nil {
			// Serve returns a non-nil error on normal shutdown.
		}
	}()

	conn, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		panic(fmt.Sprintf("danmujiGRPC.Bufconn: %v", err))
	}

	return &danmujiGRPCConn{
		conn:     conn,
		server:   server,
		listener: listener,
	}
}

func (c *danmujiGRPCConn) Conn() *grpc.ClientConn {
	return c.conn
}

func (c *danmujiGRPCConn) Close() {
	if c == nil {
		return
	}
	if c.conn != nil {
		_ = c.conn.Close()
	}
	if c.server != nil {
		c.server.Stop()
	}
	if c.listener != nil {
		_ = c.listener.Close()
	}
}
`

var pollingAssertionHelpers = `
func danmujiDeepEqual(expected, actual interface{}) bool {
	if reflect.DeepEqual(expected, actual) {
		return true
	}

	expectedValue := reflect.ValueOf(expected)
	actualValue := reflect.ValueOf(actual)
	if !expectedValue.IsValid() || !actualValue.IsValid() {
		return false
	}
	if !danmujiIsNumericKind(expectedValue.Kind()) || !danmujiIsNumericKind(actualValue.Kind()) {
		return false
	}

	return danmujiNumericEqual(expectedValue, actualValue)
}

func danmujiNumericEqual(expected, actual reflect.Value) bool {
	switch {
	case danmujiIsSignedKind(expected.Kind()):
		expectedInt := expected.Int()
		switch {
		case danmujiIsSignedKind(actual.Kind()):
			return expectedInt == actual.Int()
		case danmujiIsUnsignedKind(actual.Kind()):
			return expectedInt >= 0 && uint64(expectedInt) == actual.Uint()
		case danmujiIsFloatKind(actual.Kind()):
			return float64(expectedInt) == actual.Float()
		}
	case danmujiIsUnsignedKind(expected.Kind()):
		expectedUint := expected.Uint()
		switch {
		case danmujiIsSignedKind(actual.Kind()):
			actualInt := actual.Int()
			return actualInt >= 0 && expectedUint == uint64(actualInt)
		case danmujiIsUnsignedKind(actual.Kind()):
			return expectedUint == actual.Uint()
		case danmujiIsFloatKind(actual.Kind()):
			return float64(expectedUint) == actual.Float()
		}
	case danmujiIsFloatKind(expected.Kind()):
		expectedFloat := expected.Float()
		switch {
		case danmujiIsSignedKind(actual.Kind()):
			return expectedFloat == float64(actual.Int())
		case danmujiIsUnsignedKind(actual.Kind()):
			return expectedFloat == float64(actual.Uint())
		case danmujiIsFloatKind(actual.Kind()):
			return expectedFloat == actual.Float()
		}
	}

	return false
}

func danmujiIsNumericKind(kind reflect.Kind) bool {
	return danmujiIsSignedKind(kind) || danmujiIsUnsignedKind(kind) || danmujiIsFloatKind(kind)
}

func danmujiIsSignedKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return true
	}
	return false
}

func danmujiIsUnsignedKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return true
	}
	return false
}

func danmujiIsFloatKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Float32, reflect.Float64:
		return true
	}
	return false
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

type danmujiMatcher struct {
	kind     string
	expected interface{}
}

func danmujiMatchContains(expected interface{}) danmujiMatcher {
	return danmujiMatcher{kind: "contains", expected: expected}
}

func danmujiMatchNil() danmujiMatcher {
	return danmujiMatcher{kind: "nil"}
}

func danmujiMatchNotNil() danmujiMatcher {
	return danmujiMatcher{kind: "not_nil"}
}

func danmujiMatches(expected map[string]interface{}, actual interface{}) bool {
	ok, _ := danmujiPartialMatch(expected, actual)
	return ok
}

func danmujiPartialMatch(expected map[string]interface{}, actual interface{}) (bool, string) {
	actualValue := reflect.ValueOf(actual)
	if !actualValue.IsValid() {
		return false, "actual value is nil"
	}
	for actualValue.Kind() == reflect.Pointer {
		if actualValue.IsNil() {
			return false, "actual value is nil"
		}
		actualValue = actualValue.Elem()
	}

	keys := make([]string, 0, len(expected))
	for key := range expected {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		fieldValue, ok := danmujiLookupMatchField(actualValue, key)
		if !ok {
			return false, fmt.Sprintf("missing field %q", key)
		}
		if ok, detail := danmujiMatchValue(expected[key], fieldValue.Interface()); !ok {
			return false, fmt.Sprintf("%s: %s", key, detail)
		}
	}

	return true, ""
}

func danmujiLookupMatchField(actual reflect.Value, key string) (reflect.Value, bool) {
	switch actual.Kind() {
	case reflect.Struct:
		field := actual.FieldByName(key)
		if !field.IsValid() {
			return reflect.Value{}, false
		}
		return field, true
	case reflect.Map:
		if actual.Type().Key().Kind() != reflect.String {
			return reflect.Value{}, false
		}
		field := actual.MapIndex(reflect.ValueOf(key))
		if !field.IsValid() {
			return reflect.Value{}, false
		}
		return field, true
	}
	return reflect.Value{}, false
}

func danmujiMatchValue(expected, actual interface{}) (bool, string) {
	switch spec := expected.(type) {
	case danmujiMatcher:
		switch spec.kind {
		case "contains":
			if danmujiContains(actual, spec.expected) {
				return true, ""
			}
			return false, fmt.Sprintf("expected %v to contain %v", actual, spec.expected)
		case "nil":
			if danmujiIsNilish(actual) {
				return true, ""
			}
			return false, fmt.Sprintf("expected nil, got %v", actual)
		case "not_nil":
			if !danmujiIsNilish(actual) {
				return true, ""
			}
			return false, "expected non-nil value"
		}
	}

	if danmujiDeepEqual(expected, actual) {
		return true, ""
	}
	return false, fmt.Sprintf("expected %v, got %v", expected, actual)
}

func danmujiIsNilish(value interface{}) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	}
	return false
}

func danmujiUnorderedEqual(expected, actual interface{}) bool {
	ok, _ := danmujiUnorderedEqualDetail(expected, actual)
	return ok
}

func danmujiUnorderedEqualDetail(expected, actual interface{}) (bool, string) {
	expectedValue := reflect.ValueOf(expected)
	actualValue := reflect.ValueOf(actual)
	if !expectedValue.IsValid() || !actualValue.IsValid() {
		if !expectedValue.IsValid() && !actualValue.IsValid() {
			return true, ""
		}
		return false, "one side is nil"
	}

	if (expectedValue.Kind() != reflect.Slice && expectedValue.Kind() != reflect.Array) ||
		(actualValue.Kind() != reflect.Slice && actualValue.Kind() != reflect.Array) {
		return false, "unordered_equal requires slices or arrays"
	}

	if expectedValue.Len() != actualValue.Len() {
		return false, fmt.Sprintf("length mismatch: expected %d, got %d", expectedValue.Len(), actualValue.Len())
	}

	used := make([]bool, actualValue.Len())
	for i := 0; i < expectedValue.Len(); i++ {
		expectedElem := expectedValue.Index(i).Interface()
		found := false
		for j := 0; j < actualValue.Len(); j++ {
			if used[j] {
				continue
			}
			if danmujiDeepEqual(expectedElem, actualValue.Index(j).Interface()) {
				used[j] = true
				found = true
				break
			}
		}
		if !found {
			return false, fmt.Sprintf("missing element %v in actual value %v", expectedElem, actual)
		}
	}

	return true, ""
}
`

// ---------------------------------------------------------------------------
// injectImports adds all collected import paths into the existing import block
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) injectImports(code string) string {
	if len(t.neededImports) == 0 {
		return code
	}

	existing := map[string]bool{}
	blockRe := regexp.MustCompile(`(?ms)^import\s*\(\n(.*?)\n\)`)
	blockMatch := blockRe.FindStringSubmatchIndex(code)
	if blockMatch != nil {
		importPathRe := regexp.MustCompile(`"([^"]+)"`)
		for _, match := range importPathRe.FindAllStringSubmatch(code[blockMatch[2]:blockMatch[3]], -1) {
			existing[match[1]] = true
		}
	}

	singleRe := regexp.MustCompile(`(?m)^import\s+"([^"]+)"\s*$`)
	singleMatch := singleRe.FindStringSubmatchIndex(code)
	if singleMatch != nil {
		existing[code[singleMatch[2]:singleMatch[3]]] = true
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

	buildImportBlock := func(paths []string) string {
		var b strings.Builder
		b.WriteString("import (\n")
		for _, path := range paths {
			fmt.Fprintf(&b, "\t%q\n", path)
		}
		b.WriteString(")")
		return b.String()
	}

	if blockMatch != nil {
		all := make([]string, 0, len(existing)+len(imports))
		for path := range existing {
			all = append(all, path)
		}
		all = append(all, imports...)
		sort.Strings(all)
		return code[:blockMatch[0]] + buildImportBlock(all) + code[blockMatch[1]:]
	}

	if singleMatch != nil {
		all := []string{code[singleMatch[2]:singleMatch[3]]}
		all = append(all, imports...)
		sort.Strings(all)
		return code[:singleMatch[0]] + buildImportBlock(all) + code[singleMatch[1]:]
	}

	packageRe := regexp.MustCompile(`(?m)^package\s+\w+\s*$`)
	packageMatch := packageRe.FindStringIndex(code)
	if packageMatch == nil {
		return code
	}

	return code[:packageMatch[1]] + "\n\n" + buildImportBlock(imports) + code[packageMatch[1]:]
}

func (t *dmjTranspiler) injectBuildConstraints(code string) string {
	if len(t.fileCategories) == 0 || t.fileCategories["unit"] {
		return code
	}

	var tags []string
	for _, category := range []string{"integration", "e2e"} {
		if t.fileCategories[category] {
			tags = append(tags, category)
		}
	}
	if len(tags) == 0 {
		return code
	}

	return fmt.Sprintf("//go:build %s\n\n%s", strings.Join(tags, " || "), code)
}
