# Danmuji

A BDD testing language for Go. Write expressive test specs in `.dmj` files, compile them to standard `go test` code. No runtime library, no reflection, no magic.

```
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

# Transpile all .dmj files in a directory
danmuji build ./mypackage/

# Run the generated tests
go test -v ./mypackage/...
```

Each `.dmj` file produces a `_danmuji_test.go` file in the same directory. Put `.dmj` files next to your Go code, just like `_test.go` files.

## Features

### Test categories

```
unit "fast logic" { ... }           // go test ./...
integration "database" { ... }      // go test -tags=integration ./...
e2e "full flow" { ... }             // go test -tags=e2e ./...
```

`integration` and `e2e` blocks emit `//go:build` tags. Plain `go test` only runs unit tests.

### Given / When / Then

```
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

```
expect x == 1                  // assert.Equal(t, 1, x)
expect x != 0                  // assert.NotEqual(t, 0, x)
expect err == nil              // require.NoError(t, err)
expect result not_nil          // assert.NotNil(t, result)
expect items contains "apple"  // assert.Contains(t, items, "apple")
reject ok                      // assert.False(t, ok)
```

Backed by [testify](https://github.com/stretchr/testify). `expect err == nil` uses `require` (stops the test on failure). Everything else uses `assert` (logs and continues).

### Mocks

```
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

### Fakes

```
fake InMemoryStore {
    Get(key string) -> string {
        return "value"
    }
}
```

Like mocks, but with real method bodies. Use when you need working behavior, not just return values.

### Spies

```
spy EventBus {
    Publish(topic string, data interface{})
    Subscribe(topic string) -> chan interface{}
}
```

Wraps a real implementation. Records all calls and arguments, then delegates to `inner`. Use when you need real side effects but also want to verify they happened.

### Lifecycle hooks

```
unit "database tests" {
    before each {
        db := setupTestDB()
    }
    after each {
        db.Close()
    }
    // ...
}
```

`before each` inlines at the top of each subtest. `after each` becomes `t.Cleanup`.

### Tags

```
@slow
@smoke
integration "heavy test" { ... }
```

`@slow` adds `if testing.Short() { t.Skip() }`. `@skip` skips unconditionally. `@parallel` adds `t.Parallel()`.

### Scenario-driven tests

```
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

Each entry inherits from `defaults` and only specifies what changes. Generates a typed struct, slice, and `for...range` with parallel subtests.

### Matrix tests

```
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

Cartesian product of all dimensions. Each combination runs as a subtest.

### Containers (testcontainers-go)

```
integration "database round-trip" {
    needs postgres "db" {}
    needs redis "cache" {}

    given "a connected database" {
        // db and cache containers are running
    }
}
```

Supported services: `postgres`, `redis`, `mysql`, `kafka`, `mongo`, `rabbitmq`, `nats`, `container` (generic). Backed by [testcontainers-go](https://github.com/testcontainers/testcontainers-go).

### Benchmarks

```
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

```
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

```
load "checkout endpoint" {
    rate 50
    duration 30
    target post "http://localhost:8080/api/checkout" {}

    then "fast enough" {
        expect true
    }
}
```

Generates [vegeta](https://github.com/tsenart/vegeta) attack code with rate limiting, duration, and metrics collection. Load tests get `//go:build e2e` by default.

### Profiling

```
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

### Goroutine leak detection

```
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

```
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

Generates a `Clock` interface and `fakeClock` struct with `Advance`, `Set`, and `SetLocation` methods.

### Shell commands

```
unit "migrations" {
    exec "run migrations" {
        run "migrate -database $DB_URL up"
        expect exit_code == 0
    }
}
```

Runs shell commands with assertable `exit_code`, `stdout`, and `stderr`.

### Snapshot testing

```
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

## How it works

Danmuji extends Go's grammar using [gotreesitter](https://github.com/odvcencio/gotreesitter)'s `grammargen` package. The extended grammar parses `.dmj` files into a concrete syntax tree, then a transpiler walks the tree and emits Go code.

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

Working and tested. 48 tests pass including end-to-end compile-and-run tests that verify the generated Go code actually works.

This is an early release. The grammar and transpiler are functional but the generated code patterns may evolve.

## License

MIT
