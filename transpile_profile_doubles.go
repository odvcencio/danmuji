package danmuji

import (
	"fmt"
	"strconv"
	"strings"
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ---------------------------------------------------------------------------
// profile_block → runtime profiling instrumentation
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitProfile(n *gotreesitter.Node) string {
	// Extract profile_type from children.
	profileType := ""
	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		if t.nodeType(c) == "profile_type" {
			profileType = t.text(c)
			break
		}
	}
	dir := t.parseProfileDirective(t.childByField(n, "directive"))
	tv := t.testVar

	var b strings.Builder
	emitProfileOutput := func(prefix, lookup string) {
		pathVar := prefix + "ProfilePath"
		fileVar := prefix + "ProfileFile"
		errVar := prefix + "ProfileErr"
		b.WriteString(pathVar + " := \"\"\n")
		b.WriteString("var " + fileVar + " *os.File\n")
		b.WriteString("var " + errVar + " error\n")
		if dir.mode == "save" {
			b.WriteString(pathVar + " = " + dir.path + "\n")
			b.WriteString(fileVar + ", " + errVar + " = os.Create(" + pathVar + ")\n")
		} else {
			b.WriteString(fileVar + ", " + errVar + " = os.CreateTemp(\"\", \"" + prefix + "_profile_*.pprof\")\n")
			b.WriteString("if " + errVar + " == nil {\n")
			b.WriteString("\t" + pathVar + " = " + fileVar + ".Name()\n")
			b.WriteString("}\n")
		}
		b.WriteString("if " + errVar + " != nil {\n")
		b.WriteString("\t" + tv + ".Fatalf(\"creating " + prefix + " profile failed: %v\", " + errVar + ")\n")
		b.WriteString("}\n")
		b.WriteString(tv + ".Logf(\"" + prefix + " profile written to %s\", " + pathVar + ")\n")
		if dir.mode == "show" {
			b.WriteString(tv + ".Logf(\"show top %d requested for " + prefix + " profile (not executed automatically)\", " + strconv.Itoa(dir.top) + ")\n")
		}
		if lookup != "" {
			b.WriteString("defer func() {\n")
			b.WriteString("\t_ = pprof.Lookup(\"" + lookup + "\").WriteTo(" + fileVar + ", 1)\n")
			b.WriteString("\t_ = " + fileVar + ".Close()\n")
			b.WriteString("}()\n")
		}
	}

	switch profileType {
	case "routines":
		t.addImport("runtime")
		t.addImport("time")
		b.WriteString("_goroutinesBefore := runtime.NumGoroutine()\n")
		if dir.mode == "show" {
			b.WriteString(tv + ".Logf(\"show top %d requested for routines profile (not executed automatically)\", " + strconv.Itoa(dir.top) + ")\n")
		}
		if dir.mode == "save" {
			b.WriteString("// save path for routines is not currently file-backed\n")
		}
		b.WriteString("defer func() {\n")
		b.WriteString("\truntime.GC()\n")
		b.WriteString("\ttime.Sleep(100 * time.Millisecond)\n")
		b.WriteString("\t_goroutinesAfter := runtime.NumGoroutine()\n")
		b.WriteString("\t_goroutineDelta := _goroutinesAfter - _goroutinesBefore\n")
		b.WriteString("\t_ = _goroutineDelta // available for assertions\n")
		b.WriteString("}()\n")
	case "cpu":
		t.addImport("runtime/pprof")
		t.addImport("os")
		b.WriteString("_cpuProfilePath := \"\"\n")
		b.WriteString("var _cpuProfFile *os.File\n")
		b.WriteString("var _cpuProfErr error\n")
		if dir.mode == "save" {
			b.WriteString("_cpuProfilePath = " + dir.path + "\n")
			b.WriteString("_cpuProfFile, _cpuProfErr = os.Create(_cpuProfilePath)\n")
		} else {
			b.WriteString("_cpuProfFile, _cpuProfErr = os.CreateTemp(\"\", \"cpu_profile_*.pprof\")\n")
			b.WriteString("if _cpuProfErr == nil {\n")
			b.WriteString("\t_cpuProfilePath = _cpuProfFile.Name()\n")
			b.WriteString("}\n")
		}
		b.WriteString("if _cpuProfErr != nil {\n")
		b.WriteString("\t" + tv + ".Fatalf(\"creating cpu profile failed: %v\", _cpuProfErr)\n")
		b.WriteString("}\n")
		b.WriteString(tv + ".Logf(\"cpu profile written to %s\", _cpuProfilePath)\n")
		if dir.mode == "show" {
			b.WriteString(tv + ".Logf(\"show top %d requested for cpu profile (not executed automatically)\", " + strconv.Itoa(dir.top) + ")\n")
		}
		b.WriteString("pprof.StartCPUProfile(_cpuProfFile)\n")
		b.WriteString("defer func() {\n")
		b.WriteString("\tpprof.StopCPUProfile()\n")
		b.WriteString("\t_ = _cpuProfFile.Close()\n")
		b.WriteString("}()\n")
	case "mem":
		t.addImport("runtime")
		t.addImport("runtime/pprof")
		t.addImport("os")
		emitProfileOutput("mem", "")
		b.WriteString("defer func() {\n")
		b.WriteString("\truntime.GC()\n")
		b.WriteString("\t_ = pprof.WriteHeapProfile(memProfileFile)\n")
		b.WriteString("\t_ = memProfileFile.Close()\n")
		if dir.mode == "show" {
			b.WriteString("\t" + tv + ".Logf(\"show top %d requested for mem profile (not executed automatically)\", " + strconv.Itoa(dir.top) + ")\n")
		}
		b.WriteString("}()\n")
	case "allocs":
		t.addImport("runtime")
		t.addImport("runtime/pprof")
		t.addImport("os")
		emitProfileOutput("allocs", "")
		b.WriteString("var _memStatsBefore runtime.MemStats\n")
		b.WriteString("runtime.ReadMemStats(&_memStatsBefore)\n")
		b.WriteString("defer func() {\n")
		b.WriteString("\tvar _memStatsAfter runtime.MemStats\n")
		b.WriteString("\truntime.ReadMemStats(&_memStatsAfter)\n")
		b.WriteString("\t_allocsDelta := _memStatsAfter.TotalAlloc - _memStatsBefore.TotalAlloc\n")
		b.WriteString("\t_ = _allocsDelta // available for assertions\n")
		b.WriteString("\tif allocsProfileFile != nil {\n")
		b.WriteString("\t\t_ = pprof.Lookup(\"allocs\").WriteTo(allocsProfileFile, 1)\n")
		b.WriteString("\t\t_ = allocsProfileFile.Close()\n")
		b.WriteString("\t}\n")
		b.WriteString("}()\n")
	case "blockprofile":
		t.addImport("runtime")
		t.addImport("runtime/pprof")
		t.addImport("os")
		emitProfileOutput("block", "block")
		b.WriteString("runtime.SetBlockProfileRate(1)\n")
		b.WriteString("defer runtime.SetBlockProfileRate(0)\n")
	case "mutexprofile":
		t.addImport("runtime")
		t.addImport("runtime/pprof")
		t.addImport("os")
		emitProfileOutput("mutex", "mutex")
		b.WriteString("runtime.SetMutexProfileFraction(1)\n")
		b.WriteString("defer runtime.SetMutexProfileFraction(0)\n")
	default:
		b.WriteString(fmt.Sprintf("// unsupported profile type: %s\n", profileType))
	}
	return b.String()
}

type profileDirective struct {
	mode string // save or show
	path string
	top  int
}

func (t *dmjTranspiler) parseProfileDirective(n *gotreesitter.Node) profileDirective {
	if n == nil {
		return profileDirective{}
	}
	text := strings.TrimSpace(t.text(n))
	if text == "" {
		return profileDirective{}
	}
	lower := strings.ToLower(text)
	if strings.HasPrefix(lower, "save ") {
		return profileDirective{
			mode: "save",
			path: strings.TrimSpace(text[len("save"):]),
		}
	}
	if strings.HasPrefix(lower, "show top") {
		parts := strings.Fields(lower)
		top := 10
		if len(parts) >= 3 {
			if parsed, err := strconv.Atoi(parts[2]); err == nil {
				top = parsed
			}
		}
		return profileDirective{
			mode: "show",
			top:  top,
		}
	}
	return profileDirective{}
}

// ---------------------------------------------------------------------------
// fake_declaration → struct with real method bodies (package-level)
// ---------------------------------------------------------------------------

type fakeMethodInfo struct {
	name       string
	params     string
	returnType string
	bodyText   string
}

func (t *dmjTranspiler) parseFakeMethod(n *gotreesitter.Node) fakeMethodInfo {
	info := fakeMethodInfo{}
	if name := t.childByField(n, "name"); name != nil {
		info.name = t.text(name)
	}
	if params := t.childByField(n, "parameters"); params != nil {
		info.params = t.text(params)
	}
	if ret := t.childByField(n, "return_type"); ret != nil {
		info.returnType = t.text(ret)
	}
	if body := t.childByField(n, "body"); body != nil {
		info.bodyText = t.emitTestBody(body)
	}
	return info
}

func (t *dmjTranspiler) buildFakeDecl(n *gotreesitter.Node) string {
	nameNode := t.childByField(n, "name")
	if nameNode == nil {
		return t.text(n)
	}
	fakeName := t.text(nameNode)
	structName := "fake" + fakeName

	var methods []fakeMethodInfo
	t.findFakeMethods(n, &methods)

	var b strings.Builder

	// Struct definition
	fmt.Fprintf(&b, "type %s struct{}\n\n", structName)

	// Methods with real bodies
	for _, m := range methods {
		fmt.Fprintf(&b, "func (f *%s) %s%s", structName, m.name, m.params)
		if m.returnType != "" {
			fmt.Fprintf(&b, " %s", m.returnType)
		}
		fmt.Fprintf(&b, " %s\n\n", m.bodyText)
	}

	return b.String()
}

func (t *dmjTranspiler) findFakeMethods(n *gotreesitter.Node, out *[]fakeMethodInfo) {
	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		if t.nodeType(c) == "fake_method" {
			*out = append(*out, t.parseFakeMethod(c))
		} else {
			t.findFakeMethods(c, out)
		}
	}
}

// ---------------------------------------------------------------------------
// spy_declaration → struct wrapping real implementation with call recording
//
// A spy has an `inner` field that delegates to the real implementation.
// Each method records calls and args, then passes through to inner.
// Uses mock_method syntax inside the body (same as mock_declaration).
//
// spy EventBus {
//     Publish(topic string, data interface{})
//     Subscribe(topic string) -> chan interface{}
// }
//
// Generates:
//   type spyEventBus struct {
//       inner          EventBus
//       PublishCalls   int
//       PublishArgs    [][]interface{}
//       SubscribeCalls int
//       SubscribeArgs  [][]interface{}
//   }
//   func (s *spyEventBus) Publish(topic string, data interface{}) { ... }
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitSpy(n *gotreesitter.Node) string {
	// Bodyless spies are rejected during the first pass. If one reaches codegen,
	// emit nothing rather than a placeholder comment.
	bodyNode := t.childByField(n, "body")
	if bodyNode == nil {
		return ""
	}

	// With a body, the struct is emitted at package level via buildSpyDecl.
	// This inline call should not happen if collectTopLevel did its job.
	return ""
}

// buildSpyDecl generates the Go struct type + methods string for a spy_declaration
// with a body. The result is emitted at package level.
func (t *dmjTranspiler) buildSpyDecl(n *gotreesitter.Node) string {
	nameNode := t.childByField(n, "name")
	if nameNode == nil {
		return ""
	}
	spyName := t.text(nameNode)
	structName := "spy" + spyName

	bodyNode := t.childByField(n, "body")
	if bodyNode == nil {
		// No body — emit a TODO placeholder. Not collected at package level.
		return ""
	}

	// Reuse findMockMethods to parse method signatures from the body.
	var methods []mockMethodInfo
	t.findMockMethods(n, &methods)

	var b strings.Builder

	// Struct with inner field + call counters + args slices
	fmt.Fprintf(&b, "type %s struct {\n", structName)
	fmt.Fprintf(&b, "\tinner %s\n", spyName)
	for _, m := range methods {
		fmt.Fprintf(&b, "\t%sCalls int\n", m.name)
		fmt.Fprintf(&b, "\t%sArgs [][]interface{}\n", m.name)
	}
	fmt.Fprintf(&b, "}\n\n")

	// Methods: record call, record args, delegate to inner
	for _, m := range methods {
		// Parse parameter names from the params string, e.g. "(topic string, data interface{})"
		paramNames := extractParamNames(m.params)

		fmt.Fprintf(&b, "func (s *%s) %s%s", structName, m.name, m.params)
		if m.returnType != "" {
			fmt.Fprintf(&b, " %s", m.returnType)
		}
		fmt.Fprintf(&b, " {\n")

		// Record call count
		fmt.Fprintf(&b, "\ts.%sCalls++\n", m.name)

		// Record args
		argsSlice := "[]interface{}{"
		for i, pn := range paramNames {
			if i > 0 {
				argsSlice += ", "
			}
			argsSlice += pn
		}
		argsSlice += "}"
		fmt.Fprintf(&b, "\ts.%sArgs = append(s.%sArgs, %s)\n", m.name, m.name, argsSlice)

		// Delegate to inner
		callArgs := strings.Join(paramNames, ", ")
		if m.returnType != "" {
			fmt.Fprintf(&b, "\treturn s.inner.%s(%s)\n", m.name, callArgs)
		} else {
			fmt.Fprintf(&b, "\ts.inner.%s(%s)\n", m.name, callArgs)
		}

		fmt.Fprintf(&b, "}\n\n")
	}

	return b.String()
}

// extractParamNames parses a Go parameter list string like "(topic string, data interface{})"
// and returns the parameter names: ["topic", "data"].
func extractParamNames(params string) []string {
	// Strip outer parens
	inner := strings.TrimSpace(params)
	if strings.HasPrefix(inner, "(") {
		inner = inner[1:]
	}
	if strings.HasSuffix(inner, ")") {
		inner = inner[:len(inner)-1]
	}
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return nil
	}

	// Split on commas, but respect nested braces/parens for interface{} etc.
	var names []string
	depth := 0
	start := 0
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '(', '{':
			depth++
		case ')', '}':
			depth--
		case ',':
			if depth == 0 {
				names = append(names, extractSingleParamName(inner[start:i]))
				start = i + 1
			}
		}
	}
	names = append(names, extractSingleParamName(inner[start:]))

	return names
}

// extractSingleParamName extracts the parameter name from a single param like "topic string".
func extractSingleParamName(param string) string {
	param = strings.TrimSpace(param)
	// Variadic: "args ...interface{}" → "args..."
	if idx := strings.Index(param, "..."); idx > 0 {
		name := strings.TrimSpace(param[:idx])
		return name + "..."
	}
	// Normal: "topic string" → "topic"
	parts := strings.Fields(param)
	if len(parts) >= 1 {
		return parts[0]
	}
	return param
}
