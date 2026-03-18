# `danmuji test` + `process` Blocks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `danmuji test` command that transpiles and runs tests in one step with `.dmj`-level error reporting, and a `process` block for opaque-box testing of binaries.

**Architecture:** The transpiler gains a `TranspileOptions` struct for `//line` directive control. The grammar adds ~9 new productions for `process`/`stop`/`ready`. The CLI adds a `test` subcommand that generates, runs, and cleans up. All new transpiler emitters follow the existing contextual-handler pattern (parent iterates children, children return `""` from top-level `emit()`).

**Tech Stack:** Go 1.24, gotreesitter (grammargen DSL), testify, testcontainers-go, vegeta

**Spec:** `docs/superpowers/specs/2026-03-18-test-command-and-process-blocks-design.md`

---

## File Structure

| File | Role | Change |
|------|------|--------|
| `transpile.go` | Core transpiler | Add `TranspileOptions`, `sourceFile`/`emitLineDirectives` fields, `lineDirective()` helper, `syncBuffer` helper, `emitProcess`, `emitStop`, `emitReady`. Add 7 empty-return cases to `emit()`. |
| `grammar.go` | Grammar definition | Add 9 productions, wire into `_statement`, add 7 conflicts. |
| `cmd/danmuji/main.go` | CLI | Add `test` subcommand, `--debug` flag, update call sites. |
| `grammar_test.go` | Grammar tests | Add ~8 parsing tests for new productions. |
| `transpile_test.go` | Transpiler tests | Add ~12 tests for process/stop/ready/line directives. Update existing tests for new API. |
| `highlights.scm` | Syntax highlighting | Add keywords and patterns for new productions. |
| `README.md` | Documentation | Document `danmuji test`, `process`, `stop`, `ready`. |

---

### Task 1: `TranspileOptions` API and `//line` directives

Change `TranspileDanmuji` to accept options and emit `//line` directives before key nodes.

**Files:**
- Modify: `transpile.go:32-60` (TranspileDanmuji function)
- Modify: `transpile.go:62-94` (dmjTranspiler struct)
- Modify: `transpile.go:411-476` (emitTestBlock)
- Modify: `transpile.go:653-679` (emitBDDBlock)
- Modify: `transpile.go:690-695` (emitExpect)
- Modify: `transpile.go:794+` (emitExpectAssertion)
- Test: `transpile_test.go`

- [ ] **Step 1: Write failing test for `TranspileOptions` API**

Add this test to `transpile_test.go`:

```go
func TestTranspileDanmujiLineDirectives(t *testing.T) {
	source := []byte(`package main_test

import "testing"

unit "basic" {
	then "check" {
		expect 1 == 1
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{SourceFile: "/tmp/test.dmj"})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)
	if !strings.Contains(goCode, "//line /tmp/test.dmj:") {
		t.Error("expected //line directive with source file")
	}
}

func TestTranspileDanmujiDebugOmitsLineDirectives(t *testing.T) {
	source := []byte(`package main_test

import "testing"

unit "basic" {
	then "check" {
		expect 1 == 1
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{SourceFile: "/tmp/test.dmj", Debug: true})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	if strings.Contains(goCode, "//line") {
		t.Error("expected no //line directives in debug mode")
	}
}

func TestTranspileDanmujiEmptySourceFileOmitsDirectives(t *testing.T) {
	source := []byte(`package main_test

import "testing"

unit "basic" {
	then "check" {
		expect 1 == 1
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	if strings.Contains(goCode, "//line") {
		t.Error("expected no //line directives when SourceFile is empty")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestTranspileDanmujiLineDirectives -v ./...`
Expected: FAIL — `TranspileDanmuji` does not accept a second argument.

- [ ] **Step 3: Add `TranspileOptions` struct and update `TranspileDanmuji` signature**

In `transpile.go`, add the struct and update the function:

```go
// TranspileOptions controls transpiler behavior.
type TranspileOptions struct {
	SourceFile string // absolute path to .dmj file, used in //line directives
	Debug      bool   // if true, omit //line directives
}

// TranspileDanmuji parses a .dmj source file and emits valid Go test code.
func TranspileDanmuji(source []byte, opts TranspileOptions) (string, error) {
	lang, err := getDanmujiLanguage()
	if err != nil {
		return "", fmt.Errorf("generate danmuji language: %w", err)
	}

	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse(source)
	if err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}

	root := tree.RootNode()
	if root.HasError() {
		return "", fmt.Errorf("parse errors:\n%s", root.SExpr(lang))
	}

	emitDirectives := opts.SourceFile != "" && !opts.Debug
	tr := &dmjTranspiler{
		src:                source,
		lang:               lang,
		testVar:            "t",
		sourceFile:         opts.SourceFile,
		emitLineDirectives: emitDirectives,
	}
	tr.collectTopLevel(root)
	output := tr.emit(root)
	output = tr.injectImports(output)

	return output, nil
}
```

Add fields to `dmjTranspiler` struct (after `pollingHelpersEmitted`):

```go
	// Source file path for //line directives.
	sourceFile string
	// Whether to emit //line directives.
	emitLineDirectives bool
```

Add the `lineDirective` helper method:

```go
// lineDirective returns a //line directive string for the given node, or empty if disabled.
func (t *dmjTranspiler) lineDirective(n *gotreesitter.Node) string {
	if !t.emitLineDirectives {
		return ""
	}
	return fmt.Sprintf("//line %s:%d\n", t.sourceFile, t.lineOf(n))
}
```

- [ ] **Step 4: Update all existing call sites**

In `transpile_test.go`, update every call to `TranspileDanmuji(source)` to `TranspileDanmuji(source, TranspileOptions{})`. There are ~25 call sites. Use find-and-replace: `TranspileDanmuji(source)` → `TranspileDanmuji(source, TranspileOptions{})`.

In `cmd/danmuji/main.go:76`, update:

```go
goCode, err := danmuji.TranspileDanmuji(source, danmuji.TranspileOptions{
    SourceFile: absPath,
})
```

Where `absPath` is computed from the input path:

```go
absPath, _ := filepath.Abs(path)
```

- [ ] **Step 5: Insert `//line` directives in emitters**

In `emitTestBlock`, before the function signature line (`fmt.Fprintf(&b, "func Test%s...")`), insert:

```go
b.WriteString(t.lineDirective(n))
```

In `emitBDDBlock`, before the `t.Run(` line (`fmt.Fprintf(&b, "%s.Run(...")`), insert:

```go
b.WriteString(t.lineDirective(n))
```

In `emitExpectAssertion`, at the top of the function (after the `actual == nil` check), insert:

```go
directive := t.lineDirective(n)
```

Then prepend `directive` to each return value. For example, change:
```go
return fmt.Sprintf("assert.True(%s, ...)", ...)
```
to:
```go
return directive + fmt.Sprintf("assert.True(%s, ...)", ...)
```

Do the same for `emitReject` (the non-exec, non-polling path).

In `emitExec`, before the `t.Run(` line, insert `b.WriteString(t.lineDirective(n))`.

In `emitLoad`, before the `func TestLoad` line, insert `b.WriteString(t.lineDirective(n))`.

In `emitBenchmark`, before the `func Benchmark` line, insert `b.WriteString(t.lineDirective(n))`.

- [ ] **Step 6: Run all tests**

Run: `go test -v -count=1 ./...`
Expected: All tests pass, including the 3 new `//line` directive tests.

- [ ] **Step 7: Commit**

```
git add transpile.go transpile_test.go cmd/danmuji/main.go
git commit -m "Add TranspileOptions API and //line directive emission"
```

---

### Task 2: `danmuji test` CLI command

Add the `test` subcommand that transpiles, runs `go test`, and cleans up.

**Files:**
- Modify: `cmd/danmuji/main.go`
- Test: `cmd/danmuji/main_test.go`

- [ ] **Step 1: Write failing test for `test` subcommand**

Read `cmd/danmuji/main_test.go` first to understand the existing test pattern. Then add a test that invokes the `test` subcommand. Since the CLI is a `main` package, the test should verify the `testCmd` function directly. Add to `cmd/danmuji/main_test.go`:

```go
func TestTestCommand(t *testing.T) {
	// Create a temp dir with a simple .dmj file
	tmpDir := t.TempDir()
	dmjSource := []byte(`package main_test

import "testing"

unit "basic" {
	then "it works" {
		expect 1 == 1
	}
}
`)
	os.WriteFile(filepath.Join(tmpDir, "basic_test.dmj"), dmjSource, 0644)
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module testmod\n\ngo 1.21\n"), 0644)

	// Run go get testify
	getCmd := exec.Command("go", "get", "github.com/stretchr/testify@latest")
	getCmd.Dir = tmpDir
	if out, err := getCmd.CombinedOutput(); err != nil {
		t.Fatalf("go get: %v\n%s", err, out)
	}
	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = tmpDir
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}

	// Run danmuji test
	exitCode, err := runTest(filepath.Join(tmpDir, "basic_test.dmj"), nil)
	if err != nil {
		t.Fatalf("runTest: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}

	// Verify cleanup — generated file should not exist
	generated := filepath.Join(tmpDir, "basic_danmuji_test.go")
	if _, err := os.Stat(generated); !os.IsNotExist(err) {
		t.Error("expected generated file to be cleaned up")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestTestCommand -v ./cmd/danmuji/`
Expected: FAIL — `runTest` function does not exist.

- [ ] **Step 3: Implement `test` subcommand**

In `cmd/danmuji/main.go`, update `main()` to handle `test`:

```go
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: danmuji <build|test> <path> [-- go-test-flags...]\n")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "build":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Usage: danmuji build [--debug] <path>\n")
			os.Exit(1)
		}
		debug := false
		pathArg := os.Args[2]
		if os.Args[2] == "--debug" {
			debug = true
			if len(os.Args) < 4 {
				fmt.Fprintf(os.Stderr, "Usage: danmuji build --debug <path>\n")
				os.Exit(1)
			}
			pathArg = os.Args[3]
		}
		if err := build(pathArg, debug); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "test":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Usage: danmuji test <path> [-- go-test-flags...]\n")
			os.Exit(1)
		}
		// Split args at "--" to separate danmuji args from go test flags
		pathArg := os.Args[2]
		var goTestFlags []string
		for i := 3; i < len(os.Args); i++ {
			if os.Args[i] == "--" {
				goTestFlags = os.Args[i+1:]
				break
			}
		}
		exitCode, err := runTest(pathArg, goTestFlags)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(exitCode)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\nUsage: danmuji <build|test> <path>\n", os.Args[1])
		os.Exit(1)
	}
}
```

Update `build` to accept `debug` parameter and pass it through:

```go
func build(path string, debug bool) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return buildDir(path, debug)
	}
	return buildFile(path, debug)
}

func buildDir(dir string, debug bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".dmj") {
			continue
		}
		if err := buildFile(filepath.Join(dir, entry.Name()), debug); err != nil {
			return err
		}
		count++
	}
	fmt.Printf("danmuji: transpiled %d file(s)\n", count)
	return nil
}

func buildFile(path string, debug bool) error {
	source, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	absPath, _ := filepath.Abs(path)
	goCode, err := danmuji.TranspileDanmuji(source, danmuji.TranspileOptions{
		SourceFile: absPath,
		Debug:      debug,
	})
	if err != nil {
		return fmt.Errorf("transpile %s: %w", path, err)
	}

	base := strings.TrimSuffix(filepath.Base(path), ".dmj")
	outName := base + "_danmuji_test.go"
	if strings.HasSuffix(base, "_test") {
		outName = strings.TrimSuffix(base, "_test") + "_danmuji_test.go"
	}
	outPath := filepath.Join(filepath.Dir(path), outName)

	output := generatedHeader + goCode
	if err := os.WriteFile(outPath, []byte(output), 0644); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}

	fmt.Printf("  %s -> %s\n", filepath.Base(path), outName)
	return nil
}
```

Add the `runTest` function:

```go
func runTest(path string, goTestFlags []string) (int, error) {
	// Step 1: Discover .dmj files
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}

	var dmjFiles []string
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return 0, err
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".dmj") {
				dmjFiles = append(dmjFiles, filepath.Join(path, entry.Name()))
			}
		}
	} else {
		dmjFiles = []string{path}
	}

	if len(dmjFiles) == 0 {
		return 0, fmt.Errorf("no .dmj files found at %s", path)
	}

	// Step 2: Transpile each file (generates _danmuji_test.go in same dir)
	var generatedFiles []string
	for _, f := range dmjFiles {
		outPath, err := transpileForTest(f)
		if err != nil {
			// Cleanup already-generated files on error
			for _, gf := range generatedFiles {
				os.Remove(gf)
			}
			return 0, err
		}
		generatedFiles = append(generatedFiles, outPath)
	}

	// Step 3: Cleanup generated files when done (regardless of outcome)
	defer func() {
		for _, gf := range generatedFiles {
			os.Remove(gf)
		}
	}()

	// Step 4: Run go test
	testDir := filepath.Dir(dmjFiles[0])
	args := append([]string{"test"}, goTestFlags...)
	args = append(args, "./...")
	cmd := exec.Command("go", args...)
	cmd.Dir = testDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return 0, err
	}
	return 0, nil
}

func transpileForTest(path string) (string, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}

	absPath, _ := filepath.Abs(path)
	goCode, err := danmuji.TranspileDanmuji(source, danmuji.TranspileOptions{
		SourceFile: absPath,
	})
	if err != nil {
		return "", fmt.Errorf("transpile %s: %w", path, err)
	}

	base := strings.TrimSuffix(filepath.Base(path), ".dmj")
	outName := base + "_danmuji_test.go"
	if strings.HasSuffix(base, "_test") {
		outName = strings.TrimSuffix(base, "_test") + "_danmuji_test.go"
	}
	outPath := filepath.Join(filepath.Dir(path), outName)

	output := generatedHeader + goCode
	if err := os.WriteFile(outPath, []byte(output), 0644); err != nil {
		return "", fmt.Errorf("write %s: %w", outPath, err)
	}

	return outPath, nil
}
```

Add `"os/exec"` to the imports in `main.go`.

- [ ] **Step 4: Run all tests**

Run: `go test -v -count=1 ./...`
Expected: All tests pass including the new TestTestCommand.

- [ ] **Step 5: Commit**

```
git add cmd/danmuji/main.go cmd/danmuji/main_test.go
git commit -m "Add danmuji test command with generate-run-cleanup flow"
```

---

### Task 3: `process` block grammar

Add the grammar productions for `process`, `stop`, `ready`, `signal`, `timeout`.

**Files:**
- Modify: `grammar.go:460-554` (after `consistently_block`, before wiring section)
- Test: `grammar_test.go`

- [ ] **Step 1: Write failing grammar parsing tests**

Add to `grammar_test.go`:

```go
func TestDanmujiProcessBlock(t *testing.T) {
	input := `package main
func f() {
	process "./cmd/server" {
		args "--port=9090"
		env { DB_URL: "postgres://localhost/test", LOG_LEVEL: "debug" }
		ready http "http://localhost:9090/health"
	}
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "process_block") {
		t.Error("expected process_block node")
	}
	if !strings.Contains(sexp, "process_args") {
		t.Error("expected process_args node")
	}
	if !strings.Contains(sexp, "process_env") {
		t.Error("expected process_env node")
	}
	if !strings.Contains(sexp, "ready_clause") {
		t.Error("expected ready_clause node")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}

func TestDanmujiProcessRun(t *testing.T) {
	input := `package main
func f() {
	process run "./bin/server" {
		args "--port=9090"
	}
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "process_block") {
		t.Error("expected process_block node")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}

func TestDanmujiStopBlock(t *testing.T) {
	input := `package main
func f() {
	stop {
		signal SIGTERM
		timeout 30s
		expect exit_code == 0
	}
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "stop_block") {
		t.Error("expected stop_block node")
	}
	if !strings.Contains(sexp, "signal_directive") {
		t.Error("expected signal_directive node")
	}
	if !strings.Contains(sexp, "timeout_directive") {
		t.Error("expected timeout_directive node")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}

func TestDanmujiReadyModes(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"http", `ready http "http://localhost:8080/health"`},
		{"tcp", `ready tcp "localhost:8080"`},
		{"stdout", `ready stdout "listening on"`},
		{"delay", `ready delay 5s`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			full := `package main
func f() {
	` + tt.input + `
}
`
			sexp := parseDanmuji(t, full)
			t.Logf("SExpr: %s", sexp)
			if !strings.Contains(sexp, "ready_clause") {
				t.Error("expected ready_clause node")
			}
			if !strings.Contains(sexp, "ready_mode") {
				t.Error("expected ready_mode node")
			}
			if strings.Contains(sexp, "ERROR") {
				t.Errorf("unexpected ERROR: %s", sexp)
			}
		})
	}
}

func TestDanmujiSignalNames(t *testing.T) {
	for _, sig := range []string{"SIGTERM", "SIGINT", "SIGKILL", "SIGUSR1", "SIGHUP"} {
		t.Run(sig, func(t *testing.T) {
			input := fmt.Sprintf(`package main
func f() {
	signal %s
}
`, sig)
			sexp := parseDanmuji(t, input)
			t.Logf("SExpr: %s", sexp)
			if !strings.Contains(sexp, "signal_directive") {
				t.Error("expected signal_directive node")
			}
			if strings.Contains(sexp, "ERROR") {
				t.Errorf("unexpected ERROR: %s", sexp)
			}
		})
	}
}

func TestDanmujiStopBareAssertions(t *testing.T) {
	input := `package main
func f() {
	stop {
		expect exit_code == 0
		expect stderr contains "done"
	}
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "stop_block") {
		t.Error("expected stop_block node")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}
```

Add `"fmt"` to the `grammar_test.go` import block if not already present.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run "TestDanmujiProcess|TestDanmujiStop|TestDanmujiReady|TestDanmujiSignal" -v ./...`
Expected: FAIL — new node types not found in parse tree.

- [ ] **Step 3: Add grammar productions**

In `grammar.go`, add these productions **before** the "Wire into Go" section (before line 464):

```go
		// ---------------------------------------------------------------
		// Process blocks: opaque-box testing
		// ---------------------------------------------------------------
		g.Define("process_block", Seq(
			Str("process"),
			Optional(Str("run")),
			Field("path", Sym("_string_literal")),
			Field("body", Sym("block")),
		))

		g.Define("process_args", Seq(
			Str("args"),
			Field("value", Sym("_string_literal")),
		))

		g.Define("process_env", PrecDynamic(20, Seq(
			Str("env"),
			Str("{"),
			CommaSep1(Sym("scenario_field")),
			Str("}"),
		)))

		g.Define("ready_mode", Choice(
			Str("http"),
			Str("tcp"),
			Str("stdout"),
			Str("delay"),
		))

		g.Define("ready_clause", Seq(
			Str("ready"),
			Field("mode", Sym("ready_mode")),
			Field("target", Choice(
				Sym("_expression"),
				Sym("duration_literal"),
			)),
		))

		// ---------------------------------------------------------------
		// Stop block: shutdown observation
		// ---------------------------------------------------------------
		g.Define("stop_block", Seq(
			Str("stop"),
			Field("body", Sym("block")),
		))

		g.Define("signal_name", Pat(`SIG[A-Z0-9]+`))

		g.Define("signal_directive", Seq(
			Str("signal"),
			Field("name", Sym("signal_name")),
		))

		g.Define("timeout_directive", Seq(
			Str("timeout"),
			Field("duration", Choice(
				Sym("duration_literal"),
				Sym("_expression"),
			)),
		))
```

Add to the `AppendChoice(g, "_statement", ...)` call (after `Sym("matrix_field")`):

```go
			Sym("process_block"),
			Sym("process_args"),
			Sym("process_env"),
			Sym("ready_clause"),
			Sym("stop_block"),
			Sym("signal_directive"),
			Sym("timeout_directive"),
```

Add conflicts (after the existing `AddConflict` block):

```go
		AddConflict(g, "_statement", "process_block")
		AddConflict(g, "_statement", "process_args")
		AddConflict(g, "_statement", "process_env")
		AddConflict(g, "_statement", "ready_clause")
		AddConflict(g, "_statement", "stop_block")
		AddConflict(g, "_statement", "signal_directive")
		AddConflict(g, "_statement", "timeout_directive")
```

- [ ] **Step 4: Run all tests**

Run: `go test -v -count=1 ./...`
Expected: All tests pass (existing + new grammar tests). Note: the cached `danmujiLang` variable in `grammar_test.go` will be regenerated since the grammar changed.

- [ ] **Step 5: Commit**

```
git add grammar.go grammar_test.go
git commit -m "Add process, stop, ready, signal, timeout grammar productions"
```

---

### Task 4: `syncBuffer` helper and `emitProcess`

Add the thread-safe buffer and the process block transpiler.

**Files:**
- Modify: `transpile.go` (add syncBuffer, emitProcess, emit dispatcher cases)
- Test: `transpile_test.go`

- [ ] **Step 1: Write failing transpiler test for process block**

Add to `transpile_test.go`:

```go
func TestTranspileDanmujiProcess(t *testing.T) {
	source := []byte(`package main_test

import "testing"

e2e "server" {
	process "./cmd/server" {
		args "--port=9090"
		ready http "http://localhost:9090/health"
	}
	then "responds" {
		expect true
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "exec.Command(\"go\", \"build\"") {
		t.Error("expected go build command")
	}
	if !strings.Contains(goCode, "proc.Start()") {
		t.Error("expected proc.Start()")
	}
	if !strings.Contains(goCode, "http.Get(") {
		t.Error("expected http.Get readiness check")
	}
	if !strings.Contains(goCode, "require.True") {
		t.Error("expected require.True readiness assertion")
	}
	if !strings.Contains(goCode, "t.Cleanup(") {
		t.Error("expected t.Cleanup for teardown")
	}
	if !strings.Contains(goCode, "syncBuffer") {
		t.Error("expected syncBuffer for thread-safe stdout/stderr")
	}
}

func TestTranspileDanmujiProcessRun(t *testing.T) {
	source := []byte(`package main_test

import "testing"

e2e "tool" {
	process run "./bin/mytool" {
		args "--check"
	}
	then "works" {
		expect true
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if strings.Contains(goCode, "go build") {
		t.Error("process run should not emit go build")
	}
	if !strings.Contains(goCode, "exec.Command(\"./bin/mytool\"") {
		t.Error("expected direct binary execution")
	}
}

func TestTranspileDanmujiProcessEnv(t *testing.T) {
	source := []byte(`package main_test

import "testing"

e2e "server" {
	process "./cmd/server" {
		env { DB_URL: "postgres://localhost/test" }
		ready delay 1s
	}
	then "ok" {
		expect true
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "os.Environ()") {
		t.Error("expected os.Environ() in env setup")
	}
	if !strings.Contains(goCode, "DB_URL=postgres://localhost/test") {
		t.Error("expected DB_URL env var")
	}
}

func TestTranspileDanmujiReadyModes(t *testing.T) {
	cases := []struct {
		name     string
		ready    string
		expected string
	}{
		{"tcp", `ready tcp "localhost:9090"`, "net.Dial(\"tcp\""},
		{"stdout", `ready stdout "listening"`, "procStdout.String()"},
		{"delay", `ready delay 2s`, "time.Sleep(2 * time.Second)"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			source := []byte(fmt.Sprintf(`package main_test
import "testing"
e2e "svc" {
	process run "./bin/svc" {
		%s
	}
	then "ok" { expect true }
}
`, tt.ready))
			goCode, err := TranspileDanmuji(source, TranspileOptions{})
			if err != nil {
				t.Fatalf("transpile: %v", err)
			}
			t.Logf("Transpiled Go:\n%s", goCode)
			if !strings.Contains(goCode, tt.expected) {
				t.Errorf("expected %q in output", tt.expected)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run "TestTranspileDanmujiProcess" -v ./...`
Expected: FAIL — process block not handled by transpiler.

- [ ] **Step 3: Add `syncBuffer` helper emission**

In `transpile.go`, add a constant for the `syncBuffer` type (similar to `pollingAssertionHelpers`):

```go
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
```

Add a `syncBufferEmitted bool` field to `dmjTranspiler`.

In `collectTopLevel`, add detection for `process_block`:

```go
	if nt == "process_block" && !t.syncBufferEmitted {
		t.syncBufferEmitted = true
		t.addImport("sync")
		t.addImport("bytes")
		t.mockDecls = append(t.mockDecls, syncBufferHelper)
	}
```

- [ ] **Step 4: Add emit dispatcher cases**

In the `emit()` switch, add:

```go
	case "process_block":
		return t.emitProcess(n)
	case "stop_block":
		return t.emitStop(n)
	case "process_args":
		return "" // handled by emitProcess
	case "process_env":
		return "" // handled by emitProcess
	case "ready_clause":
		return "" // handled by emitProcess
	case "signal_directive":
		return "" // handled by emitStop
	case "timeout_directive":
		return "" // handled by emitStop
```

- [ ] **Step 5: Implement `emitProcess`**

Add the `emitProcess` method. This function:
1. Checks for `run` keyword to decide build vs direct execution
2. Extracts `args`, `env`, `ready` from body children
3. Emits build step (if no `run`)
4. Emits `syncBuffer` variables and `exec.Command`
5. Emits readiness polling
6. Checks for sibling `stop_block` to decide implicit vs explicit cleanup

The implementation should follow the pattern of `emitNeedsBlock` (iterating children, building up a `strings.Builder`, adding imports as needed). Use `t.walkChildren(bodyNode, ...)` to find `process_args`, `process_env`, `ready_clause` children. Use `t.childByField` for named fields.

For the readiness polling, implement a `emitReady(mode, target string) string` helper that switches on mode and returns the polling code block.

For the implicit teardown, check the parent node for a `stop_block` sibling. Walk the parent's children looking for `stop_block` — if found, skip implicit cleanup.

Refer to the spec's "Transpilation" section for the exact generated code patterns.

- [ ] **Step 6: Implement `emitReady`**

A helper that takes the ready mode string and target string and returns the generated polling code. Four cases: `http`, `tcp`, `stdout`, `delay`. Each includes `_ready` bool + `require.True` assertion after the loop (except `delay` which is just `time.Sleep`).

- [ ] **Step 7: Run all tests**

Run: `go test -v -count=1 ./...`
Expected: All tests pass.

- [ ] **Step 8: Commit**

```
git add transpile.go transpile_test.go
git commit -m "Add process block transpilation with syncBuffer and readiness polling"
```

---

### Task 5: `stop` block transpilation

Add the `emitStop` function for shutdown observation.

**Files:**
- Modify: `transpile.go`
- Test: `transpile_test.go`

- [ ] **Step 1: Write failing transpiler tests for stop block**

Add to `transpile_test.go`:

```go
func TestTranspileDanmujiStopBlock(t *testing.T) {
	source := []byte(`package main_test

import "testing"

e2e "graceful shutdown" {
	process run "./bin/server" {
		ready delay 1s
	}
	then "serves" {
		expect true
	}
	stop {
		signal SIGTERM
		timeout 30s
		expect exit_code == 0
		expect stderr contains "shutdown complete"
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "syscall.SIGTERM") {
		t.Error("expected syscall.SIGTERM in stop block")
	}
	if !strings.Contains(goCode, "30 * time.Second") || !strings.Contains(goCode, "time.After(") {
		t.Error("expected 30s timeout in stop block")
	}
	if !strings.Contains(goCode, "exitCode") {
		t.Error("expected exitCode variable in stop block")
	}
	if !strings.Contains(goCode, "shutdown complete") {
		t.Error("expected stderr assertion in stop block")
	}
	// Should only have ONE t.Cleanup (not implicit + explicit)
	if strings.Count(goCode, "t.Cleanup(") != 1 {
		t.Errorf("expected exactly 1 t.Cleanup, got %d", strings.Count(goCode, "t.Cleanup("))
	}
}

func TestTranspileDanmujiStopBlockSIGINT(t *testing.T) {
	source := []byte(`package main_test

import "testing"

e2e "interrupt test" {
	process run "./bin/server" {
		ready delay 1s
	}
	stop {
		signal SIGINT
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "syscall.SIGINT") {
		t.Error("expected syscall.SIGINT")
	}
}

func TestTranspileDanmujiImplicitCleanup(t *testing.T) {
	source := []byte(`package main_test

import "testing"

e2e "no stop" {
	process run "./bin/server" {
		ready delay 1s
	}
	then "ok" {
		expect true
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "t.Cleanup(") {
		t.Error("expected implicit t.Cleanup")
	}
	if !strings.Contains(goCode, "syscall.SIGTERM") {
		t.Error("expected SIGTERM in implicit cleanup")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run "TestTranspileDanmujiStop|TestTranspileDanmujiImplicit" -v ./...`
Expected: FAIL — stop block not handled.

- [ ] **Step 3: Implement `emitStop`**

The `emitStop` method:
1. Walks body children for `signal_directive` and `timeout_directive`
2. Defaults: signal = `SIGTERM`, timeout = `10 * time.Second`
3. Emits a `t.Cleanup(func() { ... })` block
4. Sets `inExecBlock = true` for the assertions inside the stop body
5. Emits assertions from the remaining body statements

The generated code pattern matches the spec's "Teardown — with stop block" section. Use `t.addImport("syscall")` and `t.addImport("time")`.

For signal mapping, use the `signal_name` text directly with a `syscall.` prefix (e.g., `"SIGTERM"` → `syscall.SIGTERM`). Validate against known signals in Go's `syscall` package; emit a comment for unknown ones.

For the timeout, parse the `duration_literal` using the same `pollingDuration` helper already used by `eventually`/`consistently`, or extract the raw text and convert `30s` → `30 * time.Second`.

- [ ] **Step 4: Run all tests**

Run: `go test -v -count=1 ./...`
Expected: All tests pass.

- [ ] **Step 5: Commit**

```
git add transpile.go transpile_test.go
git commit -m "Add stop block transpilation with signal, timeout, and assertions"
```

---

### Task 6: Highlights and README

Update syntax highlighting and documentation.

**Files:**
- Modify: `highlights.scm`
- Modify: `README.md`
- Test: `highlights_test.go` (runs automatically — `TestHighlightQueryMatchesGenerated` will fail if highlights.scm is out of sync)

- [ ] **Step 1: Regenerate highlights.scm**

The existing `TestHighlightQueryMatchesGenerated` test compares `highlights.scm` against `GenerateHighlightQueries(base, ext)`. After grammar changes, the generated output will differ. Run the test first to see what the new generated output looks like:

Run: `go test -run TestHighlightQueryMatchesGenerated -v ./...`

The test will fail and print both expected and actual. Use the "expected" (generated) output to update `highlights.scm`. Additionally, manually add semantic patterns for the new productions at the bottom:

```scheme
;; process_block
(process_block path: (_) @string)

;; stop_block

;; ready_clause

;; signal_directive
(signal_directive name: (_) @constant)
```

- [ ] **Step 2: Run highlights tests**

Run: `go test -run "TestHighlight" -v ./...`
Expected: Both `TestHighlightQueryMatchesGenerated` and `TestHighlightQueryCompiles` pass.

- [ ] **Step 3: Update README.md**

Add sections for:
- `danmuji test` command under Usage
- `process` block under Features (after Containers)
- `stop` block under Features (after process)
- `ready` modes documentation

Also update the test count (run `go test -v -count=1 ./... 2>&1 | grep -c "=== RUN"` to get the new count).

- [ ] **Step 4: Run all tests**

Run: `go test -v -count=1 ./...`
Expected: All tests pass.

- [ ] **Step 5: Commit**

```
git add highlights.scm README.md
git commit -m "Update highlights and README for process blocks and danmuji test"
```

---

## Task dependency graph

```
Task 1 (TranspileOptions + //line)
  └── Task 2 (danmuji test CLI)
  └── Task 3 (process grammar)
        └── Task 4 (emitProcess + syncBuffer)
              └── Task 5 (emitStop)
                    └── Task 6 (highlights + README)
```

Tasks 2 and 3 can run in parallel after Task 1. Tasks 4-6 are sequential.
