package danmuji

import (
	"fmt"
	"strconv"
	"strings"
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ---------------------------------------------------------------------------
// no_leaks_directive → goroutine leak detection via t.Cleanup
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitNoLeaks(n *gotreesitter.Node) string {
	t.addImport("runtime")
	t.addImport("time")
	return fmt.Sprintf(`_goroutinesBefore := runtime.NumGoroutine()
%s.Cleanup(func() {
    time.Sleep(100 * time.Millisecond)
    runtime.GC()
    if _delta := runtime.NumGoroutine() - _goroutinesBefore; _delta > 0 {
        %s.Errorf("goroutine leak: %%d new goroutines still running", _delta)
    }
})`, t.testVar, t.testVar)
}

// ---------------------------------------------------------------------------
// fake_clock_directive → Clock interface + fakeClock struct + initialization
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitFakeClock(n *gotreesitter.Node) string {
	t.addImport("time")
	t.addImport("sync")

	startTime := t.childByField(n, "start_time")
	timezone := t.childByField(n, "timezone")

	// The Clock interface and fakeClock struct are collected during the first
	// pass (collectTopLevel) and emitted at package level. Here we only
	// generate the clock variable initialization (inline in the test function).
	var b strings.Builder
	if startTime != nil {
		timeStr := strings.Trim(t.text(startTime), `"`)
		if timezone != nil {
			tz := strings.Trim(t.text(timezone), `"`)
			b.WriteString(fmt.Sprintf("_loc, _ := time.LoadLocation(%s)\n", strconv.Quote(tz)))
			b.WriteString(fmt.Sprintf("_startTime, _ := time.ParseInLocation(time.RFC3339, %s, _loc)\n", strconv.Quote(timeStr)))
			b.WriteString("clock := &fakeClock{current: _startTime, loc: _loc}\n")
		} else {
			b.WriteString(fmt.Sprintf("_startTime, _ := time.Parse(time.RFC3339, %s)\n", strconv.Quote(timeStr)))
			b.WriteString("clock := &fakeClock{current: _startTime}\n")
		}
	} else {
		b.WriteString("clock := &fakeClock{current: time.Now()}\n")
	}

	return b.String()
}

// ---------------------------------------------------------------------------
// snapshot_block → t.Run with golden file comparison
//
// snapshot "valid_response" { body }
//
// The body is executed, and the last expression is captured as the snapshot value.
// The value is compared against a golden file at testdata/snapshots/<name>.golden.
// Set DANMUJI_UPDATE_SNAPSHOTS=1 to update golden files.
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitSnapshot(n *gotreesitter.Node) string {
	t.addImport("path/filepath")
	t.addImport("os")
	t.addImport("fmt")
	t.addImport("github.com/stretchr/testify/assert")

	nameNode := t.childByField(n, "name")
	snapshotName := "snapshot"
	if nameNode != nil {
		snapshotName = strings.Trim(t.text(nameNode), "\"'`")
	}

	// Walk the block children. Separate setup statements from the last expression
	// which becomes the snapshot value.
	var blockNode *gotreesitter.Node
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if t.nodeType(c) == "block" {
			blockNode = c
			break
		}
	}
	if blockNode == nil {
		return t.text(n)
	}

	// Collect all named children from the block's statement_list.
	var stmts []*gotreesitter.Node
	t.walkChildren(blockNode, func(child *gotreesitter.Node) {
		stmts = append(stmts, child)
	})

	// The last statement that looks like an expression is the snapshot value.
	// Everything before it is setup code.
	var setupStmts []*gotreesitter.Node
	snapshotExpr := ""

	if len(stmts) > 0 {
		last := stmts[len(stmts)-1]
		lastType := t.nodeType(last)
		// expression_statement wraps standalone expressions in Go grammar
		if lastType == "expression_statement" || isExpressionNode(lastType) {
			setupStmts = stmts[:len(stmts)-1]
			snapshotExpr = t.emit(last)
		} else {
			// All statements are setup; snapshot the empty string
			setupStmts = stmts
			snapshotExpr = `""`
		}
	}

	var b strings.Builder
	tv := t.testVar
	fmt.Fprintf(&b, "%s.Run(\"snapshot_%s\", func(%s *testing.T) {\n", tv, snapshotName, tv)

	// Emit setup statements
	for _, s := range setupStmts {
		fmt.Fprintf(&b, "\t%s\n", t.emit(s))
	}

	// Capture snapshot value
	fmt.Fprintf(&b, "\t_snapshotValue := fmt.Sprintf(\"%%v\", %s)\n", snapshotExpr)
	fmt.Fprintf(&b, "\n")

	// Golden file path
	fmt.Fprintf(&b, "\t_goldenPath := filepath.Join(\"testdata\", \"snapshots\", %q)\n", snapshotName+".golden")
	fmt.Fprintf(&b, "\n")

	// Update mode
	fmt.Fprintf(&b, "\tif os.Getenv(\"DANMUJI_UPDATE_SNAPSHOTS\") != \"\" {\n")
	fmt.Fprintf(&b, "\t\tos.MkdirAll(filepath.Dir(_goldenPath), 0755)\n")
	fmt.Fprintf(&b, "\t\tos.WriteFile(_goldenPath, []byte(_snapshotValue), 0644)\n")
	fmt.Fprintf(&b, "\t\t%s.Logf(\"snapshot updated: %%s\", _goldenPath)\n", tv)
	fmt.Fprintf(&b, "\t\treturn\n")
	fmt.Fprintf(&b, "\t}\n")
	fmt.Fprintf(&b, "\n")

	// Read and compare
	fmt.Fprintf(&b, "\t_expected, err := os.ReadFile(_goldenPath)\n")
	fmt.Fprintf(&b, "\tif err != nil {\n")
	fmt.Fprintf(&b, "\t\tos.MkdirAll(filepath.Dir(_goldenPath), 0755)\n")
	fmt.Fprintf(&b, "\t\tos.WriteFile(_goldenPath, []byte(_snapshotValue), 0644)\n")
	fmt.Fprintf(&b, "\t\t%s.Logf(\"snapshot created: %%s (run again to verify)\", _goldenPath)\n", tv)
	fmt.Fprintf(&b, "\t\treturn\n")
	fmt.Fprintf(&b, "\t}\n")
	fmt.Fprintf(&b, "\n")

	// Assert equality
	fmt.Fprintf(&b, "\tassert.Equal(%s, string(_expected), _snapshotValue, \"snapshot mismatch for %%s\\nRun with DANMUJI_UPDATE_SNAPSHOTS=1 to update\", %q)\n", tv, snapshotName)

	fmt.Fprintf(&b, "})")

	return b.String()
}

// ---------------------------------------------------------------------------
// process_block → build + exec.Command + Start + readiness + cleanup
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitProcess(n *gotreesitter.Node) string {
	t.addImport("os/exec")
	t.addImport("github.com/stretchr/testify/require")

	// Determine if "run" keyword is present (skip build).
	isRunMode := false
	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		if !c.IsNamed() && string(t.src[c.StartByte():c.EndByte()]) == "run" {
			isRunMode = true
			break
		}
	}

	// Extract path.
	pathNode := t.childByField(n, "path")
	if pathNode == nil {
		return t.text(n)
	}
	rawPath := strings.Trim(t.text(pathNode), "\"'`")

	// Binary name: last segment of path.
	binaryName := rawPath
	if idx := strings.LastIndex(rawPath, "/"); idx >= 0 {
		binaryName = rawPath[idx+1:]
	}

	// Find the body block.
	bodyNode := t.childByField(n, "body")
	if bodyNode == nil {
		return t.text(n)
	}

	// Walk the body for process_args, process_env, ready_clause.
	var argsNode *gotreesitter.Node
	var envNode *gotreesitter.Node
	var readyNode *gotreesitter.Node
	t.walkChildren(bodyNode, func(child *gotreesitter.Node) {
		switch t.nodeType(child) {
		case "process_args":
			argsNode = child
		case "process_env":
			envNode = child
		case "ready_clause":
			readyNode = child
		}
	})

	// Check for a sibling stop_block in the parent.
	hasStopBlock := false
	parent := n.Parent()
	if parent != nil {
		for i := 0; i < int(parent.ChildCount()); i++ {
			sib := parent.Child(i)
			if t.nodeType(sib) == "stop_block" {
				hasStopBlock = true
				break
			}
		}
		// Also check grandparent (in case parent is a block/statement_list wrapper).
		if !hasStopBlock && parent.Parent() != nil {
			gp := parent.Parent()
			for i := 0; i < int(gp.ChildCount()); i++ {
				sib := gp.Child(i)
				if t.nodeType(sib) == "stop_block" {
					hasStopBlock = true
					break
				}
			}
		}
	}

	tv := t.testVar
	var b strings.Builder
	b.WriteString(t.lineDirective(n))
	fmt.Fprintf(&b, "{\n")

	if !isRunMode {
		t.addImport("path/filepath")
		// Build step.
		fmt.Fprintf(&b, "\t_buildCmd := exec.Command(\"go\", \"build\", \"-o\", filepath.Join(%s.TempDir(), %q), %q)\n", tv, binaryName, rawPath)
		fmt.Fprintf(&b, "\t_buildOut, _buildErr := _buildCmd.CombinedOutput()\n")
		fmt.Fprintf(&b, "\trequire.NoError(%s, _buildErr, \"go build failed: %%s\", string(_buildOut))\n", tv)
		fmt.Fprintf(&b, "\n")
		fmt.Fprintf(&b, "\tvar procStdout, procStderr syncBuffer\n")

		// Build the command args.
		cmdArgs := fmt.Sprintf("filepath.Join(%s.TempDir(), %q)", tv, binaryName)
		if argsNode != nil {
			argValNode := t.childByField(argsNode, "value")
			if argValNode != nil {
				argStr := strings.Trim(t.text(argValNode), "\"'`")
				fields := strings.Fields(argStr)
				if len(fields) > 0 {
					var quotedArgs []string
					for _, f := range fields {
						quotedArgs = append(quotedArgs, fmt.Sprintf("%q", f))
					}
					cmdArgs += ", " + strings.Join(quotedArgs, ", ")
				}
			}
		}
		fmt.Fprintf(&b, "\tproc := exec.Command(%s)\n", cmdArgs)
	} else {
		// Run mode — skip build, use path directly.
		fmt.Fprintf(&b, "\tvar procStdout, procStderr syncBuffer\n")

		cmdArgs := fmt.Sprintf("%q", rawPath)
		if argsNode != nil {
			argValNode := t.childByField(argsNode, "value")
			if argValNode != nil {
				argStr := strings.Trim(t.text(argValNode), "\"'`")
				fields := strings.Fields(argStr)
				if len(fields) > 0 {
					var quotedArgs []string
					for _, f := range fields {
						quotedArgs = append(quotedArgs, fmt.Sprintf("%q", f))
					}
					cmdArgs += ", " + strings.Join(quotedArgs, ", ")
				}
			}
		}
		fmt.Fprintf(&b, "\tproc := exec.Command(%s)\n", cmdArgs)
	}

	fmt.Fprintf(&b, "\tproc.Stdout = &procStdout\n")
	fmt.Fprintf(&b, "\tproc.Stderr = &procStderr\n")

	// Env.
	if envNode != nil {
		t.addImport("os")
		var envPairs []string
		for i := 0; i < int(envNode.ChildCount()); i++ {
			child := envNode.Child(i)
			if t.nodeType(child) == "scenario_field" {
				keyNode := t.childByField(child, "key")
				valNode := t.childByField(child, "value")
				if keyNode != nil && valNode != nil {
					key := t.text(keyNode)
					val := strings.Trim(t.text(valNode), "\"'`")
					envPairs = append(envPairs, fmt.Sprintf("%q", key+"="+val))
				}
			}
		}
		if len(envPairs) > 0 {
			fmt.Fprintf(&b, "\tproc.Env = append(os.Environ(), %s)\n", strings.Join(envPairs, ", "))
		}
	}

	fmt.Fprintf(&b, "\trequire.NoError(%s, proc.Start())\n", tv)

	// Readiness polling.
	if readyNode != nil {
		modeNode := t.childByField(readyNode, "mode")
		targetNode := t.childByField(readyNode, "target")
		if modeNode != nil && targetNode != nil {
			mode := t.text(modeNode)
			target := t.text(targetNode)
			b.WriteString(t.emitReady(mode, target, tv))
		}
	}

	// Implicit cleanup if no stop block.
	if !hasStopBlock {
		t.addImport("syscall")
		t.addImport("time")
		fmt.Fprintf(&b, "\t%s.Cleanup(func() {\n", tv)
		fmt.Fprintf(&b, "\t\t_ = proc.Process.Signal(syscall.SIGTERM)\n")
		fmt.Fprintf(&b, "\t\tdone := make(chan error, 1)\n")
		fmt.Fprintf(&b, "\t\tgo func() { done <- proc.Wait() }()\n")
		fmt.Fprintf(&b, "\t\tselect {\n")
		fmt.Fprintf(&b, "\t\tcase <-done:\n")
		fmt.Fprintf(&b, "\t\tcase <-time.After(10 * time.Second):\n")
		fmt.Fprintf(&b, "\t\t\t_ = proc.Process.Kill()\n")
		fmt.Fprintf(&b, "\t\t\t<-done\n")
		fmt.Fprintf(&b, "\t\t}\n")
		fmt.Fprintf(&b, "\t})\n")
	}

	fmt.Fprintf(&b, "}\n")
	return b.String()
}

// emitReady generates readiness polling code for a process.
func (t *dmjTranspiler) emitReady(mode, target, tv string) string {
	t.addImport("time")
	var b strings.Builder

	switch mode {
	case "http":
		t.addImport("net/http")
		targetStr := strings.Trim(target, "\"'`")
		fmt.Fprintf(&b, "\t{\n")
		fmt.Fprintf(&b, "\t\t_ready := false\n")
		fmt.Fprintf(&b, "\t\t_deadline := time.Now().Add(30 * time.Second)\n")
		fmt.Fprintf(&b, "\t\tfor time.Now().Before(_deadline) {\n")
		fmt.Fprintf(&b, "\t\t\tresp, err := http.Get(%q)\n", targetStr)
		fmt.Fprintf(&b, "\t\t\tif err == nil && resp.StatusCode == 200 {\n")
		fmt.Fprintf(&b, "\t\t\t\tresp.Body.Close()\n")
		fmt.Fprintf(&b, "\t\t\t\t_ready = true\n")
		fmt.Fprintf(&b, "\t\t\t\tbreak\n")
		fmt.Fprintf(&b, "\t\t\t}\n")
		fmt.Fprintf(&b, "\t\t\tif err == nil {\n")
		fmt.Fprintf(&b, "\t\t\t\tresp.Body.Close()\n")
		fmt.Fprintf(&b, "\t\t\t}\n")
		fmt.Fprintf(&b, "\t\t\ttime.Sleep(100 * time.Millisecond)\n")
		fmt.Fprintf(&b, "\t\t}\n")
		fmt.Fprintf(&b, "\t\trequire.True(%s, _ready, \"process not ready: HTTP endpoint %%s did not return 200\", %q)\n", tv, targetStr)
		fmt.Fprintf(&b, "\t}\n")

	case "tcp":
		t.addImport("net")
		targetStr := strings.Trim(target, "\"'`")
		fmt.Fprintf(&b, "\t{\n")
		fmt.Fprintf(&b, "\t\t_ready := false\n")
		fmt.Fprintf(&b, "\t\t_deadline := time.Now().Add(30 * time.Second)\n")
		fmt.Fprintf(&b, "\t\tfor time.Now().Before(_deadline) {\n")
		fmt.Fprintf(&b, "\t\t\tconn, err := net.Dial(\"tcp\", %q)\n", targetStr)
		fmt.Fprintf(&b, "\t\t\tif err == nil {\n")
		fmt.Fprintf(&b, "\t\t\t\tconn.Close()\n")
		fmt.Fprintf(&b, "\t\t\t\t_ready = true\n")
		fmt.Fprintf(&b, "\t\t\t\tbreak\n")
		fmt.Fprintf(&b, "\t\t\t}\n")
		fmt.Fprintf(&b, "\t\t\ttime.Sleep(100 * time.Millisecond)\n")
		fmt.Fprintf(&b, "\t\t}\n")
		fmt.Fprintf(&b, "\t\trequire.True(%s, _ready, \"process not ready: TCP endpoint %%s not reachable\", %q)\n", tv, targetStr)
		fmt.Fprintf(&b, "\t}\n")

	case "stdout":
		targetStr := strings.Trim(target, "\"'`")
		t.addImport("strings")
		fmt.Fprintf(&b, "\t{\n")
		fmt.Fprintf(&b, "\t\t_ready := false\n")
		fmt.Fprintf(&b, "\t\t_deadline := time.Now().Add(30 * time.Second)\n")
		fmt.Fprintf(&b, "\t\tfor time.Now().Before(_deadline) {\n")
		fmt.Fprintf(&b, "\t\t\tif strings.Contains(procStdout.String(), %q) {\n", targetStr)
		fmt.Fprintf(&b, "\t\t\t\t_ready = true\n")
		fmt.Fprintf(&b, "\t\t\t\tbreak\n")
		fmt.Fprintf(&b, "\t\t\t}\n")
		fmt.Fprintf(&b, "\t\t\ttime.Sleep(100 * time.Millisecond)\n")
		fmt.Fprintf(&b, "\t\t}\n")
		fmt.Fprintf(&b, "\t\trequire.True(%s, _ready, \"process not ready: stdout did not contain %%q\", %q)\n", tv, targetStr)
		fmt.Fprintf(&b, "\t}\n")

	case "delay":
		dur := durationLiteralToGo(target)
		fmt.Fprintf(&b, "\ttime.Sleep(%s)\n", dur)
	}

	return b.String()
}

// durationLiteralToGo converts a duration literal like "5s" or "100ms" to
// a Go time expression like "5 * time.Second" or "100 * time.Millisecond".
func durationLiteralToGo(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "1 * time.Second"
	}

	unitMap := map[string]string{
		"ns": "time.Nanosecond",
		"us": "time.Microsecond",
		"µs": "time.Microsecond",
		"ms": "time.Millisecond",
		"s":  "time.Second",
		"m":  "time.Minute",
		"h":  "time.Hour",
	}

	if matchDurationUnit.MatchString(raw) {
		parts := matchDurationUnit.FindStringSubmatch(raw)
		num := parts[1]
		unit := parts[2]
		if goUnit, ok := unitMap[unit]; ok {
			return num + " * " + goUnit
		}
	}

	// If it already contains time., pass through.
	if strings.Contains(raw, "time.") {
		return raw
	}

	return raw
}

// ---------------------------------------------------------------------------
// stop_block → explicit shutdown observation
// ---------------------------------------------------------------------------

func (t *dmjTranspiler) emitStop(n *gotreesitter.Node) string {
	t.addImport("syscall")
	t.addImport("time")
	t.addImport("bytes")
	t.addImport("os/exec")

	bodyNode := t.childByField(n, "body")
	if bodyNode == nil {
		return t.text(n)
	}

	// Defaults.
	signalName := "SIGTERM"
	timeoutExpr := "10 * time.Second"

	// Collect directives and assertion statements.
	var assertionNodes []*gotreesitter.Node
	t.walkChildren(bodyNode, func(child *gotreesitter.Node) {
		switch t.nodeType(child) {
		case "signal_directive":
			nameNode := t.childByField(child, "name")
			if nameNode != nil {
				signalName = t.text(nameNode)
			}
		case "timeout_directive":
			durNode := t.childByField(child, "duration")
			if durNode != nil {
				if t.nodeType(durNode) == "duration_literal" {
					timeoutExpr = durationLiteralToGo(t.text(durNode))
				} else {
					timeoutExpr = t.text(durNode)
				}
			}
		case "expect_statement", "reject_statement":
			assertionNodes = append(assertionNodes, child)
		}
	})

	tv := t.testVar
	var b strings.Builder
	b.WriteString(t.lineDirective(n))
	fmt.Fprintf(&b, "%s.Cleanup(func() {\n", tv)
	fmt.Fprintf(&b, "\t_ = proc.Process.Signal(syscall.%s)\n", signalName)
	fmt.Fprintf(&b, "\tvar exitCode int\n")
	fmt.Fprintf(&b, "\tdone := make(chan error, 1)\n")
	fmt.Fprintf(&b, "\tgo func() { done <- proc.Wait() }()\n")
	fmt.Fprintf(&b, "\tselect {\n")
	fmt.Fprintf(&b, "\tcase err := <-done:\n")
	fmt.Fprintf(&b, "\t\tif exitErr, ok := err.(*exec.ExitError); ok {\n")
	fmt.Fprintf(&b, "\t\t\texitCode = exitErr.ExitCode()\n")
	fmt.Fprintf(&b, "\t\t}\n")
	fmt.Fprintf(&b, "\tcase <-time.After(%s):\n", timeoutExpr)
	fmt.Fprintf(&b, "\t\t_ = proc.Process.Kill()\n")
	fmt.Fprintf(&b, "\t\t<-done\n")
	fmt.Fprintf(&b, "\t\texitCode = -1\n")
	fmt.Fprintf(&b, "\t}\n")
	fmt.Fprintf(&b, "\tvar stdout, stderr bytes.Buffer\n")
	fmt.Fprintf(&b, "\tstdout.WriteString(procStdout.String())\n")
	fmt.Fprintf(&b, "\tstderr.WriteString(procStderr.String())\n")
	fmt.Fprintf(&b, "\t_ = exitCode\n")

	// Emit assertion statements in exec mode.
	oldInExec := t.inExecBlock
	t.inExecBlock = true
	for _, an := range assertionNodes {
		code := t.emit(an)
		if code != "" {
			t.appendIndented(&b, code, "\t")
		}
	}
	t.inExecBlock = oldInExec

	fmt.Fprintf(&b, "})\n")
	return b.String()
}

// isExpressionNode returns true if the node type is an expression-like node.
func isExpressionNode(nodeType string) bool {
	switch nodeType {
	case "call_expression", "selector_expression", "index_expression",
		"unary_expression", "binary_expression", "identifier",
		"int_literal", "float_literal", "interpreted_string_literal",
		"raw_string_literal", "rune_literal", "composite_literal",
		"func_literal", "parenthesized_expression", "true", "false", "nil":
		return true
	}
	return false
}

