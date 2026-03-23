# Danmuji

A BDD testing language for Go. Write expressive test specs in `.dmj` files, compile them to standard `go test` code. No runtime library, no reflection, no magic.

Built on [gotreesitter](https://github.com/odvcencio/gotreesitter) — a pure-Go Tree-sitter implementation with grammar composition.

```dmj
package cart_test

import "testing"

unit "ShoppingCart.Add" {
    given "an empty cart" {
        cart := NewCart()

        when "adding an item" {
            cart.Add("widget")

            then "count increases" {
                expect cart.Count() == 1
            }
        }
    }
}
```

This compiles to:

```go
package cart_test

import (
    "testing"
    "github.com/stretchr/testify/assert"
)

func TestShoppingCartAdd(t *testing.T) {
    t.Run("an empty cart", func(t *testing.T) {
        cart := NewCart()
        t.Run("adding an item", func(t *testing.T) {
            cart.Add("widget")
            t.Run("count increases", func(t *testing.T) {
                assert.Equal(t, 1, cart.Count())
            })
        })
    })
}
```

Run with `go test -v`. That's it.

## Install

```bash
go install github.com/odvcencio/danmuji/cmd/danmuji@latest
```

## Usage

```bash
# Transpile a single file
danmuji build ./path/to/user_test.dmj

# Transpile all .dmj files in a directory tree
danmuji build ./mypackage/

# Run the generated tests
go test -v ./mypackage/...
```

Directory builds recurse into subdirectories. Each `.dmj` file produces a `_danmuji_test.go` file in the same directory. Put `.dmj` files next to your Go code, just like `_test.go` files.

### Run tests directly

```bash
# Transpile and run in one step (cleans up generated files after)
danmuji test ./mypackage/

# Forward flags to go test
danmuji test ./mypackage/ -- -v -run TestAuth

# Build without //line directives (for debugging generated code)
danmuji build --debug ./mypackage/
```

When a test fails, errors reference your `.dmj` source file and line number directly.

## Features

### Test categories

```dmj
unit "fast logic" { ... }           // go test ./...
integration "database" { ... }      // go test -tags=integration ./...
e2e "full flow" { ... }             // go test -tags=e2e ./...
```

`integration` and `e2e` blocks emit `//go:build` tags. Plain `go test` only runs unit tests.
For predictable filtering, keep tagged specs in their own `.dmj` files.

### Given / When / Then

```dmj
unit "UserService.Create" {
    given "valid input" {
        svc := NewUserService(repo)

        when "creating a user" {
            user, err := svc.Create("alice")

            then "succeeds" {
                expect err == nil
            }
            then "sets name" {
                expect user.Name == "alice"
            }
        }
    }
}
```

Each block becomes a `t.Run` subtest. Nest them as deep as you want.

### Assertions

```dmj
expect x == 1                  // assert.Equal(t, 1, x)
expect x != 0                  // assert.NotEqual(t, 0, x)
expect err == nil              // require.NoError(t, err)
expect result is_nil           // assert.Nil(t, result)
expect result not_nil          // assert.NotNil(t, result)
expect items contains "apple"  // assert.Contains(t, items, "apple")
expect items unordered_equal []int{3, 2, 1}
expect err is context.DeadlineExceeded
expect err message contains "quota"
reject ok                      // assert.False(t, ok)
```

Backed by [testify](https://github.com/stretchr/testify). `expect err == nil` uses `require` (stops the test on failure). Everything else uses `assert` (logs and continues).

Domain-shaped matchers can be defined as plain Go functions:

```go
func hasRole(user string, role string) bool {
	return user == role
}
```

```dmj
unit "auth" {
	then "is admin" {
		expect "admin" hasRole "admin"
	}
}
```

Partial matching works for structs and maps:

```dmj
then "user payload is right" {
	expect user matches {
		Role: "admin",
		Email: contains "@example.com",
		DeletedAt: is_nil
	}
}
```

### Factories

```dmj
factory User {
	defaults { Name: "alice", Role: "member" }
	trait admin { Role: "admin" }
}

unit "authorization" {
	user := build User with admin { Name: "root" }

	then "applies traits and overrides" {
		expect user.Name == "root"
		expect user.Role == "admin"
	}
}
```

Factories stay DSL-only. `build` emits normal Go composite literals, so the generated tests stay readable.

### Eventual and consistent assertions

```dmj
unit "retries" {
	then "waits for background work" {
		eventually "job has finished" within 5s {
			expect jobDone
		}
		consistently "job sends once" for 2s {
			reject duplicateSend
		}
	}
}
```

Duration shorthand literals are supported: `5s`, `2.5m`, `30ms`, `100us`, `1h`. You can also use full Go expressions like `5 * time.Second`. The `within` and `for` clauses are optional — omit them to use defaults.

These compile into polling loops so you can express temporal behavior without writing custom helper goroutine scaffolding.

When you want a one-shot channel receive with timeout, use `await`:

```dmj
unit "worker completion" {
	await <-jobs within 2s as job

	then "receives the completed job" {
		expect job.Status == "done"
	}
}
```

### Mocks

```dmj
mock UserRepo {
    FindByID(id int) -> User = User{Name: "stub"}
    Save(u User) -> error = nil
}

unit "service" {
    repo := &mockUserRepo{}
    repo.Save(User{Name: "alice"})

    then "save was called" {
        verify repo.Save called 1 times
    }
}
```

Generates a struct with call counters and canned return values. No code generation step, no external tool.

Verification supports:

```dmj
verify repo.Save called 1 times
verify repo.Save called with ("alice")
verify repo.Save not_called
```

### Fakes

```dmj
fake InMemoryStore {
    Get(key string) -> string {
        return "value"
    }
}
```

Like mocks, but with real method bodies. Use when you need working behavior, not just return values.

### Spies

```dmj
spy EventBus {
    Publish(topic string)
    Subscribe(topic string) -> error = nil
}
```

Records all calls and arguments. If `inner` is set, calls delegate to it. If `inner` is unset, methods fall back to their declared default return (or the zero value when no default is declared). Use when you want verification plus optional pass-through to a real implementation.

```go
bus := &spyEventBus{inner: realBus}
```

A spy must declare at least one method. Bare `spy Logger` declarations are rejected during transpilation.

### Lifecycle hooks

```dmj
unit "database tests" {
    before each {
        db := setupTestDB()
    }
    after each {
        db.Close()
    }
    before all {
        initTestEnvironment()
    }
    after all {
        teardownTestEnvironment()
    }
    // ...
}
```

`before each` inlines at the top of each subtest. `after each` becomes `t.Cleanup`. `before all` / `after all` run once for the enclosing test function.

### Tags

```dmj
@slow
@smoke
integration "heavy test" { ... }
```

Danmuji test blocks run in parallel by default. Use `@serial` to opt out when a test must stay sequential. `process`-backed tests also stay sequential automatically.

`@slow` adds `if testing.Short() { t.Skip() }`. `@skip` skips unconditionally. `@parallel` is accepted for compatibility and readability, but is redundant now that parallel is the default. Any `@identifier` is a valid tag.

### Scenario-driven tests

```dmj
unit "AuthMiddleware" {
    each "request scenario" {
        defaults { method: "GET", token: "valid", expect_status: 200 }
        { name: "happy path" }
        { name: "no token",      token: "",       expect_status: 401 }
        { name: "expired token", token: "exp456", expect_status: 401 }
        { name: "wrong role",    token: "guest",  expect_status: 403 }
    } do {
        given scenario.name {
            req := buildRequest(scenario.method, scenario.token)
            rec := httptest.NewRecorder()
            handler.ServeHTTP(rec, req)

            then "correct status" {
                expect rec.Code == scenario.expect_status
            }
        }
    }
}
```

Each entry inherits from `defaults` and only specifies what changes. Generates a scenario struct, slice, and `for...range` with subtests that follow the enclosing test's parallel policy.

### HTTP test helpers

When a `.dmj` file references `danmujiHTTP.`, danmuji injects a small helper set around `net/http/httptest`:

```dmj
unit "users handler" {
    handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusCreated)
    })

    req := danmujiHTTP.POST("/users", map[string]string{"name": "alice"})
    rec := danmujiHTTP.Serve(handler, req)

    then "creates users" {
        expect rec.Code == http.StatusCreated
    }
}
```

Available methods: `Request`, `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, and `Serve`.
String bodies become plain text, `[]byte` bodies stay binary, `io.Reader` passes through, and other body values are JSON-marshaled automatically with `Content-Type: application/json`.

### WebSocket test helpers

When a `.dmj` file references `danmujiWS.`, danmuji injects a small helper set around `github.com/gorilla/websocket`:

```dmj
unit "theatre sync" {
	server := httptest.NewServer(handler)
	defer server.Close()

	ws := danmujiWS.Dial(server, "/api/theatre/ABC123/ws")
	defer ws.Close()

	then "receives heartbeat" {
		msg := ws.ReadBinary(2 * time.Second)
		expect msg[0] == byte(0x01)
	}

	ws.SendBinary(driftBytes)

	then "records the last message" {
		expect ws.LastMessage() not_nil
	}
}
```

Available methods: `Dial`, `Close`, `SendBinary`, `SendText`, `ReadBinary`, `ReadText`, and `LastMessage`.

### Matrix tests

```dmj
unit "API compatibility" {
    matrix "method x auth" {
        method: { "GET", "POST", "PUT", "DELETE" }
        auth: { "none", "basic", "bearer" }
    } do {
        // 4 x 3 = 12 tests, auto-generated
        then "no panic" {
            expect true
        }
    }
}
```

Cartesian product of all dimensions. Each combination runs as a subtest and follows the enclosing test's parallel policy.
Each dimension is also bound as a local alias inside the generated loop, so nested blocks can reference names like `method` or `auth` directly.

### gRPC test helpers

When a `.dmj` file references `danmujiGRPC.`, danmuji injects a small helper set for `grpc/test/bufconn`:

```dmj
unit "worker rpc" {
	conn := danmujiGRPC.Bufconn(func(s *grpc.Server) {
		pb.RegisterWorkerServer(s, fakeWorker)
	})
	defer conn.Close()

	then "invokes unary grpc" {
		err := conn.Conn().Invoke(ctx, "/pkg.Worker/Ping", req, resp)
		expect err == nil
	}
}
```

Available methods: `Bufconn`, `Conn`, and `Close`.

### Property-based specs

```dmj
unit "integer rules" {
	property "addition commutative" for all (a int, b int) up to 500 {
		expect a + b == b + a
	}
}
```

This compiles into `testing/quick.Check` with a predicate-style function, so the body is evaluated over generated values instead of a fixed example table. The optional `up to N` clause overrides the default sample count.

### Data tables

```dmj
unit "addition" {
    table cases {
        | 1 | 2 | 3 |
        | 4 | 5 | 9 |
        | 100 | 200 | 300 |
    }
    each row in cases {
        then "adds correctly" {
            expect row.col0.(int) + row.col1.(int) == row.col2
        }
    }
}
```

Generates a struct per table with `col0`, `col1`, ... `interface{}` fields and iterates with `for...range`. Generated row subtests follow the enclosing test's parallel policy.

### Containers (testcontainers-go)

```dmj
integration "database round-trip" {
    needs tempdir scratch
    needs postgres db {
        password: "test"
        database: "app_test"
    }
    needs http server {
        handler api.NewHandler(repo)
    }

    given "a connected database" {
        expect scratch != ""
        expect dbEndpoint not_nil
        expect server.URL not_nil
    }
}
```

Supported services: `tempdir`, `http`, `postgres`, `redis`, `mysql`, `kafka`, `mongo`, `rabbitmq`, `nats`, `container` (generic). Backed by [testcontainers-go](https://github.com/testcontainers/testcontainers-go) for containerized services.

### Benchmarks

```dmj
benchmark "JSON marshal" {
    setup {
        data := makeLargeStruct()
    }
    measure {
        json.Marshal(data)
    }
    report_allocs
}
```

Generates `func BenchmarkJSONMarshal(b *testing.B)` with `b.ResetTimer()`, `b.N` loop, and `b.ReportAllocs()`. Run with `go test -bench=.`.

For concurrent benchmarks:

```dmj
benchmark "concurrent reads" {
    setup {
        cache := NewCache()
    }
    parallel measure {
        cache.Get("key")
    }
}
```

Generates `b.RunParallel`.

### Load testing (vegeta)

```dmj
load "checkout endpoint" {
    rate 50
    duration 30s
    rampup 5s
    target post "http://localhost:8080/api/checkout"

    then "fast enough" {
        expect true
    }
}
```

Generates [vegeta](https://github.com/tsenart/vegeta) attack code with rate limiting, duration, and metrics collection. Load tests get `//go:build e2e` by default.

Additional load config options: `rampup` and `concurrency`. `duration` and `rampup` accept duration literals such as `30s`, `500ms`, or plain expressions.

### Profiling

```dmj
unit "memory check" {
    profile mem {}
    // ... test code
}

unit "goroutine check" {
    profile routines {}
    // ... test code
}
```

Captures `runtime/pprof` profiles inline with your tests. Supports: `cpu`, `mem`, `allocs`, `routines`, `blockprofile`, `mutexprofile`.

Profile directives:
- `save "path"` writes file-backed profiles to an explicit path.
- `show top N` records the request and logs the profile path, but does not shell out to `go tool pprof` automatically.

`routines` tracks goroutine deltas inline; it does not currently write a `.pprof` file.

### Goroutine leak detection

```dmj
unit "connection pool" {
    no_leaks

    given "a pool" {
        pool := NewPool(10)
        pool.Start()
        pool.Stop()
    }
}
```

One keyword. Captures goroutine count before and after, fails if any leaked.

### Fake clock with timezone support

```dmj
unit "scheduler" {
    fake_clock at "2026-03-17T09:00:00Z" in "America/New_York"

    then "correct timezone" {
        expect clock.Now().Location().String() == "America/New_York"
    }

    then "can advance" {
        clock.Advance(24 * time.Hour)
        expect clock.Now().Day() == 18
    }
}
```

Three forms: `fake_clock at "time" in "tz"`, `fake_clock at "time"`, or bare `fake_clock` for a default zero-value clock.

Generates a `Clock` interface and `fakeClock` struct with `Advance`, `Set`, and `SetLocation` methods.

### Shell commands

```dmj
unit "migrations" {
    exec "run migrations" {
        run "migrate -database $DB_URL up"
        expect exit_code == 0
        expect stdout contains "applied"
    }
}
```

Runs shell commands with assertable `exit_code`, `stdout`, and `stderr`.

### Snapshot testing

```dmj
unit "API response" {
    given "a valid user" {
        user := User{Name: "Alice", Email: "alice@test.com"}

        snapshot "user_json" {
            json.Marshal(user)
        }
    }
}
```

First run creates `testdata/snapshots/user_json.golden`. Subsequent runs compare against it. Update with `DANMUJI_UPDATE_SNAPSHOTS=1 go test ./...`.

### Process testing (opaque box)

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

Start a binary, wait for it to be healthy, run tests against it, then observe shutdown behavior. `process` compiles and runs a Go package by default. Use `process run "./bin/server"` to skip the build step for pre-built binaries.

Readiness modes: `ready http "url"`, `ready tcp "host:port"`, `ready stdout "pattern"`, `ready delay 5s`.

The `stop` block is optional. Without it, the process is killed with SIGTERM after tests complete. With it, you control the signal, timeout, and can assert on exit code, stdout, and stderr during shutdown.

## How it works

Danmuji extends Go's grammar using [gotreesitter](https://github.com/odvcencio/gotreesitter)'s `grammargen` package. The extended grammar parses `.dmj` files into a concrete syntax tree, then a transpiler walks the tree and emits Go code. The grammar adds ~50 new productions on top of Go's base grammar, all defined in pure Go using gotreesitter's composable DSL.

The generated code depends on:
- [testify](https://github.com/stretchr/testify) for assertions
- [testcontainers-go](https://github.com/testcontainers/testcontainers-go) for `needs` blocks
- [vegeta](https://github.com/tsenart/vegeta) for `load` blocks

These are dependencies of your test code, not of danmuji itself. Add them to your project with `go get` as needed.

## File convention

```
myservice/
  user.go                  # implementation
  user_test.go             # existing Go tests (keep these)
  user_test.dmj            # danmuji specs (new)
  user_danmuji_test.go     # generated (gitignore or commit, your call)
```

Danmuji doesn't replace your existing tests. It layers on top.

## Status

Working and tested. The suite covers grammar parsing, transpiler output, highlight queries, self-hosted `.dmj` meta specs, and end-to-end compile-and-run tests that verify the generated Go code actually compiles and executes correctly.

This is an early release. The grammar and transpiler are functional but the generated code patterns may evolve.

## License

MIT
