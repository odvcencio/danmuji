# Design: `danmuji test` Command + `process` Blocks

## Summary

Two features that make danmuji a complete opaque-box testing tool:

1. **`danmuji test`** — transpile and run tests in one step, with errors reported against `.dmj` source lines
2. **`process` block** — start a binary, wait for readiness, run tests against it, observe shutdown behavior

## Feature 1: `danmuji test`

### Command interface

```
danmuji test [path] [-- go-test-flags...]
```

- `danmuji test ./mypackage/` — transpile all `.dmj` files, run `go test`
- `danmuji test ./user_test.dmj` — transpile one file, run `go test`
- `danmuji test ./mypackage/ -- -run TestAuth -v` — forward flags to `go test`

### Behavior

1. Discover `.dmj` files at the given path (file or directory)
2. Transpile each file in-place into the same directory as the source (generating `_danmuji_test.go` files, same as `build`)
3. Emit `//line` directives in the generated code so `go test` natively reports `.dmj` filenames and line numbers
4. Run `go test` in the source directory with forwarded flags
5. Stream output directly to the user's terminal
6. Clean up the generated `_danmuji_test.go` files after `go test` completes (regardless of pass/fail)
7. Exit with `go test`'s exit code

This avoids the temp directory / `go.mod` problem entirely. The generated files live in the module tree where `go test` expects them, and cleanup removes them after the run.

### `//line` directives

Go supports `//line filename:line` directives that override the file/line reported by the compiler and test runner. This is the standard mechanism used by protobuf, yacc, and `go generate` tools.

Directives are emitted before each:
- Test block (`func TestXxx`)
- BDD block (`t.Run`)
- `expect` / `reject` statement
- Lifecycle hook
- `exec` block
- `process` / `stop` block
- Any other node where a runtime error or assertion failure could surface

The transpiler already tracks source positions via `lineOf(n)`. Each directive references the original `.dmj` file path (absolute) and source line number.

Example generated output:

```go
//line /home/user/project/user_test.dmj:10
func TestUserService(t *testing.T) {
//line /home/user/project/user_test.dmj:11
    t.Run("valid input", func(t *testing.T) {
//line /home/user/project/user_test.dmj:14
        assert.Equal(t, "alice", user.Name, "danmuji:14: ...")
    })
}
```

### API change: `TranspileDanmuji`

The transpiler needs the source filename to emit `//line` directives. New signature:

```go
type TranspileOptions struct {
    SourceFile string // absolute path to .dmj file, used in //line directives
    Debug      bool   // if true, omit //line directives
}

func TranspileDanmuji(source []byte, opts TranspileOptions) (string, error)
```

The old call sites in `cmd/danmuji/main.go` and all tests pass the source filename. Tests that use inline source strings pass an empty `SourceFile` (directives omitted when empty) or a synthetic filename like `"test.dmj"`.

### `danmuji build` changes

`danmuji build` continues to emit `_danmuji_test.go` files as before, but now also emits `//line` directives by default.

New flag: `danmuji build --debug [path]` omits `//line` directives so the generated Go code has its own real line numbers for stepping through in a debugger.

**Trade-off note:** Default `//line` emission changes the error reporting behavior for existing `danmuji build` users. Errors will now reference `.dmj` files instead of generated `.go` files. This is the correct behavior but is a breaking change for anyone who relies on generated file line numbers.

### Implementation scope

- `cmd/danmuji/main.go` — add `test` subcommand with cleanup logic, `--debug` flag for `build`, pass `TranspileOptions` to `TranspileDanmuji`
- `transpile.go` — change `TranspileDanmuji` signature to accept `TranspileOptions`. Add `sourceFile` and `emitLineDirectives` fields to `dmjTranspiler`. Add `//line` directive emission to `emitTestBlock`, `emitBDDBlock`, `emitExpect`, `emitReject`, and other emitter functions.

## Feature 2: `process` block

### Syntax

Full form:

```dmj
e2e "API server" {
    process "./cmd/server" {
        args "--port=9090 --verbose"
        env { DB_URL: "postgres://localhost/test", LOG_LEVEL: "debug" }
        ready http "http://localhost:9090/health"
    }

    then "create user" {
        resp := post("http://localhost:9090/users", body)
        expect resp.StatusCode == 201
    }

    stop {
        signal SIGTERM
        timeout 30s
        expect exit_code == 0
        expect stderr contains "shutdown complete"
    }
}
```

Minimal form (pre-built binary, no readiness, implicit teardown):

```dmj
e2e "CLI tool" {
    process run "./bin/mytool" {
        args "--check"
    }
    then "exits cleanly" {
        expect true
    }
}
```

### Grammar productions

#### `process_block`

```
process_block: "process" [run] path block
```

- `process "./cmd/server" { ... }` — `go build` the package, then start the binary
- `process run "./bin/server" { ... }` — start the binary directly, skip build

```go
g.Define("process_block", Seq(
    Str("process"),
    Optional(Str("run")),
    Field("path", Sym("_string_literal")),
    Field("body", Sym("block")),
))
```

#### `process_args`

```
process_args: "args" string_literal
```

Single string, shell-split by the transpiler.

```go
g.Define("process_args", Seq(
    Str("args"),
    Field("value", Sym("_string_literal")),
))
```

#### `process_env`

```
process_env: "env" "{" scenario_field, ... "}"
```

Reuses `scenario_field` (key: value pairs). Needs `PrecDynamic(20, ...)` to match the precedence used by `scenario_entry` (grammar.go:367) and avoid conflicts with Go's block parsing.

```go
g.Define("process_env", PrecDynamic(20, Seq(
    Str("env"),
    Str("{"),
    CommaSep1(Sym("scenario_field")),
    Str("}"),
)))
```

#### `ready_clause`

```
ready_clause: "ready" mode target
```

Four modes:

| Mode | Syntax | Transpiles to |
|------|--------|---------------|
| HTTP | `ready http "http://host:port/path"` | Poll HTTP GET until 200 |
| TCP | `ready tcp "host:port"` | Poll `net.Dial("tcp", ...)` until success |
| Stdout | `ready stdout "listening on"` | Scan process stdout buffer for substring |
| Delay | `ready delay 5s` | `time.Sleep` |

```go
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
```

Note: `_string_literal` is a subset of `_expression`, so the `Choice` uses `_expression` and `duration_literal` only (matching the pattern used by `eventually_block` at grammar.go:443).

#### `stop_block`

```
stop_block: "stop" block
```

Contains optional `signal`, `timeout`, and `expect`/`reject` assertions.

```go
g.Define("stop_block", Seq(
    Str("stop"),
    Field("body", Sym("block")),
))
```

#### `signal_directive`

```
signal_directive: "signal" signal_name
```

```go
g.Define("signal_name", Pat(`SIG[A-Z0-9]+`))

g.Define("signal_directive", Seq(
    Str("signal"),
    Field("name", Sym("signal_name")),
))
```

Uses a pattern instead of an exhaustive `Choice` so `SIGUSR1`, `SIGUSR2`, etc. work without grammar changes. The transpiler validates known signals and emits an error comment for unrecognized ones.

#### `timeout_directive`

```
timeout_directive: "timeout" duration
```

```go
g.Define("timeout_directive", Seq(
    Str("timeout"),
    Field("duration", Choice(
        Sym("duration_literal"),
        Sym("_expression"),
    )),
))
```

#### Statement wiring

Add to the existing `AppendChoice(g, "_statement", ...)` call:

```go
Sym("process_block"),
Sym("process_args"),
Sym("process_env"),
Sym("ready_clause"),
Sym("stop_block"),
Sym("signal_directive"),
Sym("timeout_directive"),
```

**Context sensitivity:** These productions are valid anywhere in `_statement` at the grammar level (same as `run_command`, `load_config`, `target_block`). The transpiler handles them contextually — `emitProcess` iterates its body children directly, and the top-level `emit()` dispatcher returns `""` for `process_args`, `process_env`, `ready_clause`, `signal_directive`, and `timeout_directive` (same pattern as `run_command` → "handled by emitExec" at transpile.go:302).

#### Conflicts

Add `AddConflict(g, "_statement", ...)` for each new production:

```go
AddConflict(g, "_statement", "process_block")
AddConflict(g, "_statement", "process_args")
AddConflict(g, "_statement", "process_env")
AddConflict(g, "_statement", "ready_clause")
AddConflict(g, "_statement", "stop_block")
AddConflict(g, "_statement", "signal_directive")
AddConflict(g, "_statement", "timeout_directive")
```

### Transpilation

#### Thread-safe stdout/stderr capture

The process runs as a long-lived subprocess. Its stdout/stderr are written concurrently by the process while the test may read them (e.g., `ready stdout` polling). `bytes.Buffer` is not thread-safe.

The transpiler emits a `syncBuffer` helper at package level (same pattern as `pollingAssertionHelpers`):

```go
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
```

Process startup uses `var procStdout, procStderr syncBuffer` instead of `bytes.Buffer`.

#### Process startup

`process "./cmd/server"` (default, with build):

```go
{
    // Build
    _buildCmd := exec.Command("go", "build", "-o", filepath.Join(t.TempDir(), "server"), "./cmd/server")
    _buildOut, _buildErr := _buildCmd.CombinedOutput()
    require.NoError(t, _buildErr, "go build failed: %s", string(_buildOut))

    // Start
    var procStdout, procStderr syncBuffer
    proc := exec.Command(filepath.Join(t.TempDir(), "server"), "--port=9090", "--verbose")
    proc.Stdout = &procStdout
    proc.Stderr = &procStderr
    proc.Env = append(os.Environ(), "DB_URL=postgres://localhost/test", "LOG_LEVEL=debug")
    require.NoError(t, proc.Start())
}
```

`process run "./bin/server"` (skip build):

```go
{
    var procStdout, procStderr syncBuffer
    proc := exec.Command("./bin/server", "--port=9090", "--verbose")
    proc.Stdout = &procStdout
    proc.Stderr = &procStderr
    require.NoError(t, proc.Start())
}
```

#### Readiness polling

Reuses the polling loop pattern from `emitEventually`. Default timeout: 30 seconds. Default poll interval: 100ms. All modes assert failure if the deadline is reached without readiness.

`ready http`:

```go
{
    _ready := false
    _readyDeadline := time.Now().Add(30 * time.Second)
    for time.Now().Before(_readyDeadline) {
        resp, err := http.Get("http://localhost:9090/health")
        if err == nil && resp.StatusCode == 200 {
            resp.Body.Close()
            _ready = true
            break
        }
        if resp != nil {
            resp.Body.Close()
        }
        time.Sleep(100 * time.Millisecond)
    }
    require.True(t, _ready, "process not ready after 30s: http://localhost:9090/health")
}
```

`ready tcp`:

```go
{
    _ready := false
    _readyDeadline := time.Now().Add(30 * time.Second)
    for time.Now().Before(_readyDeadline) {
        conn, err := net.Dial("tcp", "localhost:9090")
        if err == nil {
            conn.Close()
            _ready = true
            break
        }
        time.Sleep(100 * time.Millisecond)
    }
    require.True(t, _ready, "process not ready after 30s: tcp localhost:9090")
}
```

`ready stdout`:

```go
{
    _ready := false
    _readyDeadline := time.Now().Add(30 * time.Second)
    for time.Now().Before(_readyDeadline) {
        if strings.Contains(procStdout.String(), "listening on") {
            _ready = true
            break
        }
        time.Sleep(100 * time.Millisecond)
    }
    require.True(t, _ready, "process not ready after 30s: stdout never contained \"listening on\"")
}
```

`ready delay`:

```go
time.Sleep(5 * time.Second)
```

#### Teardown — without `stop` block

When the transpiler does not find a `stop_block` child in the enclosing test block, it emits implicit cleanup immediately after `proc.Start()`:

```go
t.Cleanup(func() {
    _ = proc.Process.Signal(syscall.SIGTERM)
    done := make(chan error, 1)
    go func() { done <- proc.Wait() }()
    select {
    case <-done:
    case <-time.After(10 * time.Second):
        _ = proc.Process.Kill()
        <-done
    }
})
```

#### Teardown — with `stop` block

When the transpiler finds a `stop_block` child, the implicit cleanup is **not emitted**. Instead, the `stop` block transpiles to a single `t.Cleanup` that sends the signal, waits, and runs assertions:

```go
t.Cleanup(func() {
    // Send signal (default SIGTERM)
    _ = proc.Process.Signal(syscall.SIGTERM)

    // Wait with timeout (default 10s)
    var exitCode int
    var stdout, stderr bytes.Buffer
    done := make(chan error, 1)
    go func() { done <- proc.Wait() }()
    select {
    case err := <-done:
        if exitErr, ok := err.(*exec.ExitError); ok {
            exitCode = exitErr.ExitCode()
        }
    case <-time.After(30 * time.Second):
        _ = proc.Process.Kill()
        <-done
        exitCode = -1
    }

    stdout.WriteString(procStdout.String())
    stderr.WriteString(procStderr.String())

    // Assertions from stop block (in inExecBlock mode)
    assert.Equal(t, 0, exitCode, "expected clean exit")
    assert.Contains(t, stderr.String(), "shutdown complete")
})
```

**Important:** Only one cleanup is ever emitted per process. The transpiler checks for a `stop_block` sibling before deciding which path to take. This avoids the double-cleanup problem where `t.Cleanup` callbacks stack (LIFO) and the second one signals/waits on an already-exited process.

The `stop` block reuses the `inExecBlock` mode from `exec` blocks so `expect exit_code`, `expect stdout`, and `expect stderr` work identically. The local variables (`exitCode`, `stdout`, `stderr`) match the names used by `translateExecIdent` (transpile.go:1867), so exec-mode assertion translation works without changes.

#### Emit dispatcher additions

The `emit()` switch in `transpile.go` gains these cases:

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

### Imports

The transpiler adds imports based on what the `process` block uses:

| Feature | Imports |
|---------|---------|
| Always | `os/exec`, `sync`, `syscall` |
| `ready http` | `net/http`, `time` |
| `ready tcp` | `net`, `time` |
| `ready stdout` | `strings`, `time` |
| `ready delay` | `time` |
| `env` | `os` |
| Build step | `path/filepath` |
| `stop` with assertions | `github.com/stretchr/testify/assert` |
| Readiness assertions | `github.com/stretchr/testify/require` |

## File changes

| File | Change |
|------|--------|
| `grammar.go` | Add ~9 new productions: `process_block`, `process_args`, `process_env`, `ready_clause`, `ready_mode`, `stop_block`, `signal_directive`, `signal_name`, `timeout_directive`. Wire into `_statement`. Add 7 conflicts. |
| `transpile.go` | Change `TranspileDanmuji` signature to accept `TranspileOptions`. Add `syncBuffer` helper emission. Add `emitProcess`, `emitStop`, `emitReady` functions. Add 7 empty-return cases to `emit()` dispatcher. Add `//line` directive emission across all existing emitters. Add `sourceFile` and `emitLineDirectives` fields to `dmjTranspiler`. |
| `cmd/danmuji/main.go` | Add `test` subcommand with generate-run-cleanup flow. Add `--debug` flag to `build`. Update `TranspileDanmuji` call sites to pass `TranspileOptions`. |
| `grammar_test.go` | Add parsing tests for `process`, `stop`, `ready`, `signal`, `timeout` productions. |
| `transpile_test.go` | Add transpiler tests for process block code generation, readiness patterns, stop block assertions, `//line` directive emission. Update existing tests for new `TranspileDanmuji` signature. |
| `highlights.scm` | Add highlight entries for: `process`, `stop`, `signal`, `timeout`, `ready`, `http`, `tcp`, `stdout` (ready mode), `delay`, and `SIG*` pattern. |
| `README.md` | Document `danmuji test`, `process` block, `stop` block, `ready` modes. |

## Test plan

### Grammar tests
- `process` block with build path parses without errors
- `process run` with pre-built binary path parses without errors
- `process_args`, `process_env`, `ready_clause` parse inside process body
- All four `ready` modes parse: `http`, `tcp`, `stdout`, `delay`
- `stop` block with `signal`, `timeout`, and assertions parses
- `stop` block without `signal`/`timeout` (bare assertions) parses
- `signal` with SIGTERM, SIGINT, SIGKILL, SIGUSR1 parses
- `timeout` with duration literal and expression forms parses

### Transpiler tests
- Process block emits `go build` + `exec.Command` + `proc.Start`
- Process run block skips `go build`
- `args` string is split and passed to `exec.Command`
- `env` fields transpile to `proc.Env = append(os.Environ(), ...)`
- Each `ready` mode emits the correct polling pattern with failure assertion
- Missing `stop` block emits implicit SIGTERM + 10s cleanup
- `stop` block with `signal SIGINT` emits `syscall.SIGINT`
- `stop` block with `timeout 30s` uses 30s deadline
- `stop` block assertions reuse exec-mode expect/reject
- Only one `t.Cleanup` emitted when `stop` block is present (no double cleanup)
- `syncBuffer` helper emitted at package level when process block is present
- `//line` directives appear before test blocks, BDD blocks, and assertions
- `//line` directives omitted when `Debug: true`
- `//line` directives omitted when `SourceFile` is empty
- Existing tests updated for new `TranspileDanmuji(source, opts)` signature

### End-to-end tests
- `danmuji test` on a simple `.dmj` file runs and reports PASS
- `danmuji test` on a failing `.dmj` file reports the `.dmj` filename and line number in the error
- `danmuji test` forwards `-- -v -run TestX` flags to `go test`
- `danmuji test` cleans up generated files after run
- Process that crashes during startup fails with a clear readiness timeout message
- Multiple `process` blocks in a single `e2e` test work independently
