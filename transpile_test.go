package danmuji

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestTranspileDanmujiSimple(t *testing.T) {
	source := []byte(`package myservice_test

import "testing"

unit "arithmetic" {
	given "two numbers" {
		a := 2
		b := 3
		when "added" {
			result := a + b
			then "equals their sum" {
				expect result == 5
			}
		}
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	// Structural checks
	if !strings.Contains(goCode, "func TestArithmetic(t *testing.T)") {
		t.Error("expected TestArithmetic function")
	}
	if !strings.Contains(goCode, "t.Run(") {
		t.Error("expected t.Run calls for given/when/then")
	}
	if !strings.Contains(goCode, "assert.Equal") {
		t.Error("expected testify assert.Equal assertion")
	}
	if !strings.Contains(goCode, `"github.com/stretchr/testify/assert"`) {
		t.Error("expected testify assert import")
	}
}

func TestTranspileDanmujiCompileAndRun(t *testing.T) {
	source := []byte(`package main_test

import "testing"

unit "basic" {
	given "a value" {
		x := 42
		then "it equals 42" {
			expect x == 42
		}
		then "it is not zero" {
			expect x != 0
		}
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "main_test.go", goCode)

	// Run go test
	cmd := exec.Command("go", "test", "-v", "./...")
	cmd.Dir = tmpDir
	out, err := cmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}

	if !strings.Contains(string(out), "PASS") {
		t.Error("expected PASS in test output")
	}
}

func TestTranspileDanmujiFailingTest(t *testing.T) {
	source := []byte(`package main_test

import "testing"

unit "failing" {
	then "should fail" {
		expect 1 == 2
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "main_test.go", goCode)

	cmd := exec.Command("go", "test", "-v", "./...")
	cmd.Dir = tmpDir
	out, _ := cmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))

	// This test SHOULD fail — that proves our assertions work
	if !strings.Contains(string(out), "FAIL") {
		t.Error("expected FAIL in output — the danmuji test asserts 1==2")
	}
}

func TestTranspileDanmujiMock(t *testing.T) {
	source := []byte(`package main_test

import "testing"

unit "with mock" {
	mock Repo {
		Save(name string) -> error = nil
	}
	repo := &mockRepo{}
	_ = repo.Save("alice")
	then "save was called" {
		expect repo.SaveCalls == 1
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "main_test.go", goCode)

	cmd := exec.Command("go", "test", "-v", "./...")
	cmd.Dir = tmpDir
	out, err := cmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
}

func TestTranspileDanmujiNeeds(t *testing.T) {
	source := []byte(`package myservice_test

import "testing"

integration "with database" {
	needs postgres db {
		port = 5432
	}
	then "database is ready" {
		expect db != nil
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	// Structural checks
	if !strings.Contains(goCode, "GenericContainer") {
		t.Error("expected testcontainers.GenericContainer in generated code")
	}
	if !strings.Contains(goCode, `ExposedPorts: []string{"5432/tcp"}`) {
		t.Error("expected service port to be exposed")
	}
	if !strings.Contains(goCode, `dbContainer.Endpoint(ctx, "5432/tcp")`) {
		t.Error("expected Endpoint to use a string port argument")
	}
	if !strings.Contains(goCode, "require.NoError") {
		t.Error("expected require.NoError in generated code")
	}
	if !strings.Contains(goCode, "t.Cleanup") {
		t.Error("expected t.Cleanup for container teardown")
	}
	if !strings.Contains(goCode, `"github.com/stretchr/testify/require"`) {
		t.Error("expected testify require import")
	}
}

func TestTranspileDanmujiExec(t *testing.T) {
	source := []byte(`package exec_test

import "testing"

unit "shell commands" {
	exec "echo test" {
		run "echo hello"
		expect exit_code == 0
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "exec.Command") {
		t.Error("expected exec.Command in output")
	}
}

func TestTranspileDanmujiDomainMatcher(t *testing.T) {
	source := []byte(`package matcher_test

import "testing"

func isAdmin(role string) bool { return role == "admin" }

unit "auth" {
	then "admin role check" {
		expect "admin" isAdmin
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "assert.True") {
		t.Error("expected assert.True in output")
	}
	if !strings.Contains(goCode, "isAdmin(") {
		t.Error("expected matcher function call in output")
	}
}

func TestTranspileDanmujiLoad(t *testing.T) {
	source := []byte(`package load_test

import "testing"

load "api throughput" {
	rate 10
	duration 5s
	rampup 1s
	target get "http://localhost:8080/health"
	then "healthy" {
		expect true
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "vegeta.Rate") {
		t.Error("expected vegeta.Rate in output")
	}
	if !strings.Contains(goCode, "vegeta.NewAttacker") {
		t.Error("expected vegeta.NewAttacker in output")
	}
	if !strings.HasPrefix(goCode, "//go:build e2e\n\npackage load_test") {
		t.Error("expected load build tag at the top of the generated file")
	}
	if !strings.Contains(goCode, "duration := 5 * time.Second") {
		t.Error("expected duration literal normalization in output")
	}
	if !strings.Contains(goCode, "rampup := 1 * time.Second") {
		t.Error("expected rampup literal normalization in output")
	}
	if !strings.Contains(goCode, "func TestLoadApiThroughput") {
		t.Error("expected TestLoadApiThroughput function")
	}
}

func TestTranspileDanmujiMixedNumericComparisonCompile(t *testing.T) {
	source := []byte(`package numeric_test

import "testing"

unit "mixed numeric comparison" {
	then "int64 compares against literal" {
		var count int64 = 1
		expect count > 0
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}

	if !strings.Contains(goCode, "assert.True(t, count > 0") {
		t.Error("expected relational comparisons to emit boolean assertions")
	}

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "numeric_test.go", goCode)

	runCmd := exec.Command("go", "test", "-v", "./...")
	runCmd.Dir = tmpDir
	out, err := runCmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
}

func TestTranspileDanmujiMixedNumericEqualityUsesValueAssertions(t *testing.T) {
	source := []byte(`package numeric_test

import "testing"

unit "mixed numeric equality" {
	then "int64 literal compares by value" {
		var count int64 = 1000
		expect count == 1000
		expect 1000 == count
	}

	then "uint32 hex literal compares by value" {
		var code uint32 = 0x47
		expect code == 0x47
	}

	then "uint8 zero literal compares by value" {
		var b uint8 = 0x00
		expect b == 0x00
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}

	if !strings.Contains(goCode, "assert.EqualValues") {
		t.Fatal("expected numeric literal equality to emit assert.EqualValues")
	}

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "numeric_values_test.go", goCode)

	runCmd := exec.Command("go", "test", "-v", "./...")
	runCmd.Dir = tmpDir
	out, err := runCmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
}

func TestTranspileDanmujiMixedNumericInequalityUsesValueAssertions(t *testing.T) {
	source := []byte(`package numeric_test

import "testing"

unit "mixed numeric inequality" {
	then "int64 inequality fails when values match" {
		var count int64 = 1000
		expect count != 1000
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}

	if !strings.Contains(goCode, "assert.NotEqualValues") {
		t.Fatal("expected numeric literal inequality to emit assert.NotEqualValues")
	}

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "numeric_not_values_test.go", goCode)

	runCmd := exec.Command("go", "test", "-v", "./...")
	runCmd.Dir = tmpDir
	out, _ := runCmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))

	if !strings.Contains(string(out), "FAIL") {
		t.Fatal("expected go test to fail when typed numeric values are equal")
	}
}

func TestTranspileDanmujiExecCompile(t *testing.T) {
	source := []byte(`package exec_test

import "testing"

unit "echo" {
	exec "hello" {
		run "echo hello"
		expect exit_code == 0
		expect stdout contains "hello"
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "exec_test.go", goCode)

	runCmd := exec.Command("go", "test", "-v", "./...")
	runCmd.Dir = tmpDir
	out, err := runCmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
}

func TestTranspileDanmujiBenchmark(t *testing.T) {
	source, err := os.ReadFile("testdata/benchmark.dmj")
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "func BenchmarkStringConcat(b *testing.B)") {
		t.Error("expected BenchmarkStringConcat function")
	}
	if !strings.Contains(goCode, "b.ReportAllocs()") {
		t.Error("expected b.ReportAllocs()")
	}
	if !strings.Contains(goCode, "b.ResetTimer()") {
		t.Error("expected b.ResetTimer()")
	}
	if !strings.Contains(goCode, "for i := 0; i < b.N; i++") {
		t.Error("expected b.N loop")
	}
}

func TestTranspileDanmujiBenchmarkCompile(t *testing.T) {
	source := []byte(`package bench_test

import "testing"

benchmark "addition" {
	measure {
		x := 1 + 2
		_ = x
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "bench_test.go", goCode)

	cmd := exec.Command("go", "test", "-bench=.", "-benchtime=1x", "-v", "./...")
	cmd.Dir = tmpDir
	out, err := cmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "BenchmarkAddition") {
		t.Error("expected BenchmarkAddition in output")
	}
}

func TestTranspileDanmujiProfile(t *testing.T) {
	source := []byte(`package prof_test

import "testing"

unit "goroutine check" {
	profile routines {}
	then "no leaks" {
		expect true
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "runtime.NumGoroutine") {
		t.Error("expected runtime.NumGoroutine in output")
	}
	if !strings.Contains(goCode, "_goroutinesBefore") {
		t.Error("expected _goroutinesBefore variable")
	}
	if !strings.Contains(goCode, "defer func()") {
		t.Error("expected deferred goroutine check")
	}
}

func TestTranspileDanmujiFake(t *testing.T) {
	source := []byte(`package fake_test

import "testing"

unit "with fake" {
	fake Store {
		Get(key string) -> string {
			return "cached"
		}
	}
	s := &fakeStore{}
	then "returns cached" {
		expect s.Get("x") == "cached"
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "fakeStore") {
		t.Error("expected fakeStore struct in output")
	}
	if !strings.Contains(goCode, "type fakeStore struct{}") {
		t.Error("expected fakeStore struct definition")
	}
}

func TestTranspileDanmujiTable(t *testing.T) {
	source := []byte(`package table_test

import "testing"

unit "table driven" {
	table sums {
		| 1 | 2 | 3 |
		| 4 | 5 | 9 |
	}
	each row in sums {
		expect true
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "range sums") {
		t.Error("expected range iteration over table")
	}
	if !strings.Contains(goCode, "sumsRow") {
		t.Error("expected sumsRow struct type")
	}
}

func TestTranspileDanmujiNoLeaks(t *testing.T) {
	source := []byte(`package noleak_test

import "testing"

unit "no leaks" {
	no_leaks
	then "clean exit" {
		expect true
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "runtime.NumGoroutine") {
		t.Error("expected runtime.NumGoroutine in output")
	}
	if !strings.Contains(goCode, "goroutine leak") {
		t.Error("expected goroutine leak message in output")
	}
}

func TestTranspileDanmujiFakeClock(t *testing.T) {
	source := []byte(`package clock_test

import "testing"

unit "scheduler" {
	fake_clock at "2026-03-17T09:00:00Z" in "America/New_York"
	then "correct timezone" {
		expect clock.Now().Location().String() == "America/New_York"
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "fakeClock") {
		t.Error("expected fakeClock in output")
	}
	if !strings.Contains(goCode, "time.LoadLocation") {
		t.Error("expected time.LoadLocation in output")
	}
	if !strings.Contains(goCode, "America/New_York") {
		t.Error("expected America/New_York in output")
	}
	if !strings.Contains(goCode, "time.ParseInLocation") {
		t.Error("expected time.ParseInLocation in output")
	}
}

func TestTranspileDanmujiFakeClockCompile(t *testing.T) {
	source := []byte(`package main_test

import (
	"testing"
	"time"
)

unit "time travel" {
	fake_clock at "2026-01-01T00:00:00Z"
	then "starts at midnight" {
		expect clock.Now().Year() == 2026
		expect clock.Now().Month() == time.January
	}
	then "can advance" {
		clock.Advance(24 * time.Hour)
		expect clock.Now().Day() == 2
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "main_test.go", goCode)

	runCmd := exec.Command("go", "test", "-v", "./...")
	runCmd.Dir = tmpDir
	out, err := runCmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}

	if !strings.Contains(string(out), "PASS") {
		t.Error("expected PASS in test output")
	}
}

func TestTranspileDanmujiFullStack(t *testing.T) {
	source, err := os.ReadFile("testdata/full_stack.dmj")
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	// Structural checks
	if !strings.Contains(goCode, "func TestUserService(t *testing.T)") {
		t.Error("expected TestUserService function")
	}
	if !strings.Contains(goCode, "func BenchmarkStringOperations(b *testing.B)") {
		t.Error("expected BenchmarkStringOperations function")
	}
	if !strings.Contains(goCode, "type mockUserRepo struct") {
		t.Error("expected mockUserRepo struct")
	}
	if !strings.Contains(goCode, "assert.Equal") {
		t.Error("expected assert.Equal call")
	}
	if !strings.Contains(goCode, "t.Run(") {
		t.Error("expected t.Run calls for BDD nesting")
	}
	if !strings.Contains(goCode, "t.Cleanup(") {
		t.Error("expected t.Cleanup from after each hook")
	}
	if !strings.Contains(goCode, "b.ReportAllocs()") {
		t.Error("expected b.ReportAllocs() in benchmark")
	}

	// Compile and run
	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "full_test.go", goCode)

	cmd := exec.Command("go", "test", "-v", "-bench=.", "-benchtime=1x", "./...")
	cmd.Dir = tmpDir
	out, err := cmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}

	if !strings.Contains(string(out), "PASS") {
		t.Error("expected PASS in test output")
	}
	if !strings.Contains(string(out), "BenchmarkStringOperations") {
		t.Error("expected BenchmarkStringOperations in test output")
	}
}

func TestTranspileDanmujiSnapshot(t *testing.T) {
	source := []byte(`package snap_test

import "testing"

unit "API output" {
	snapshot "valid_response" {
		resp := getResponse()
		resp.Body
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	// Structural checks for snapshot testing pattern
	if !strings.Contains(goCode, "testdata") || !strings.Contains(goCode, "snapshots") {
		t.Error("expected testdata/snapshots path in output")
	}
	if !strings.Contains(goCode, "DANMUJI_UPDATE_SNAPSHOTS") {
		t.Error("expected DANMUJI_UPDATE_SNAPSHOTS env var check")
	}
	if !strings.Contains(goCode, "assert.Equal") {
		t.Error("expected assert.Equal for snapshot comparison")
	}
	if !strings.Contains(goCode, "_snapshotValue") {
		t.Error("expected _snapshotValue variable")
	}
	if !strings.Contains(goCode, "_goldenPath") {
		t.Error("expected _goldenPath variable")
	}
	if !strings.Contains(goCode, "snapshot_valid_response") {
		t.Error("expected snapshot_valid_response subtest name")
	}
	if !strings.Contains(goCode, `"path/filepath"`) {
		t.Error("expected path/filepath import")
	}
	if !strings.Contains(goCode, `"os"`) {
		t.Error("expected os import")
	}
}

func TestTranspileDanmujiSpyReal(t *testing.T) {
	source := []byte(`package spy_test

import "testing"

unit "with spy" {
	spy EventBus {
		Publish(topic string)
		Subscribe(topic string) -> error = nil
	}
	bus := &spyEventBus{}
	bus.Publish("events")
	then "publish was called" {
		expect bus.PublishCalls == 1
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	// Structural checks for spy
	if !strings.Contains(goCode, "spyEventBus struct") {
		t.Error("expected spyEventBus struct definition")
	}
	if !strings.Contains(goCode, "inner") {
		t.Error("expected inner field for delegation")
	}
	if !strings.Contains(goCode, "PublishCalls") {
		t.Error("expected PublishCalls counter")
	}
	if !strings.Contains(goCode, "PublishArgs") {
		t.Error("expected PublishArgs recording")
	}
	if !strings.Contains(goCode, "SubscribeCalls") {
		t.Error("expected SubscribeCalls counter")
	}
	if !strings.Contains(goCode, "reflect.ValueOf(s.inner)") {
		t.Error("expected zero-value inner guard")
	}
	if !strings.Contains(goCode, ".inner.") {
		t.Error("expected .inner. delegation call")
	}
	if !strings.Contains(goCode, "return nil") {
		t.Error("expected default return fallback for nil inner")
	}
}

func TestTranspileDanmujiSpyNoBody(t *testing.T) {
	source := []byte(`package spy_test

import "testing"

unit "spy placeholder" {
	spy Logger
	then "has placeholder" {
		expect true
	}
}
`)

	_, err := TranspileDanmuji(source, TranspileOptions{})
	if err == nil {
		t.Fatal("expected error for bodyless spy")
	}
	if !strings.Contains(err.Error(), "spy Logger must declare at least one method") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTranspileDanmujiTableCompile(t *testing.T) {
	source := []byte(`package table_test

import "testing"

unit "addition" {
	table cases {
		| 1 | 2 | 3 |
		| 0 | 0 | 0 |
		| 100 | 200 | 300 |
	}
	each row in cases {
		then "adds correctly" {
			expect true
		}
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	// Compile and run
	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "table_test.go", goCode)

	runCmd := exec.Command("go", "test", "-v", "./...")
	runCmd.Dir = tmpDir
	out, err := runCmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PASS") {
		t.Error("expected PASS in test output")
	}
}

func TestTranspileDanmujiEachDo(t *testing.T) {
	source := []byte(`package each_test
import "testing"
unit "scenarios" {
    each "auth checks" {
        defaults { token: "valid", code: 200 }
        { name: "valid request", code: 200 }
        { name: "no token", token: "", code: 401 }
    } do {
        then "check" {
            expect true
        }
    }
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled:\n%s", goCode)

	if !strings.Contains(goCode, "Scenario") {
		t.Error("expected Scenario struct")
	}
	if !strings.Contains(goCode, "for _, scenario := range") {
		t.Error("expected scenario iteration")
	}
	if !strings.Contains(goCode, `"valid"`) {
		t.Error("expected default value 'valid'")
	}
}

func TestTranspileDanmujiMatrix(t *testing.T) {
	source := []byte(`package matrix_test
import "testing"
unit "combinations" {
    matrix "method x auth" {
        method: { "GET", "POST" }
        auth: { "none", "bearer" }
    } do {
        then "no panic" {
            expect true
        }
    }
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled:\n%s", goCode)

	if !strings.Contains(goCode, "Scenario") {
		t.Error("expected Scenario struct")
	}
	// 2 methods x 2 auth = 4 entries
	if strings.Count(goCode, "\"GET\"")+strings.Count(goCode, "\"POST\"") < 4 {
		t.Error("expected 4 cartesian product entries")
	}
}

func TestTranspileDanmujiProperty(t *testing.T) {
	source := []byte(`package property_test

import "testing"

unit "integer invariants" {
	property "sum commutative" for all (a int, b int) up to 200 {
		expect a + b == b + a
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "quick.Check") {
		t.Error("expected quick.Check call in generated code")
	}
	if !strings.Contains(goCode, "testing/quick") {
		t.Error("expected quick import path in generated code")
	}
	if !strings.Contains(goCode, "func(a int, b int) bool") {
		t.Error("expected property function signature in generated code")
	}

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "property_test.go", goCode)

	// Quick is from the standard library.
	runCmd := exec.Command("go", "test", "-v", "./...")
	runCmd.Dir = tmpDir
	out, err := runCmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PASS") {
		t.Error("expected PASS in go test output")
	}
}

func TestTranspileDanmujiPropertyFailing(t *testing.T) {
	source := []byte(`package property_test

import "testing"

unit "invalid invariants" {
	property "always non-negative" for all (x int) {
		expect x >= 0
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "property_test.go", goCode)

	// This property should fail; expect tests to fail and report the property failure.
	runCmd := exec.Command("go", "test", "-v", "./...")
	runCmd.Dir = tmpDir
	out, _ := runCmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))
	if !strings.Contains(string(out), "FAIL") {
		t.Error("expected FAIL in output for false property")
	}
}

func TestTranspileDanmujiEachDoCompile(t *testing.T) {
	source := []byte(`package main_test
import "testing"
unit "math scenarios" {
    each "additions" {
        defaults { b: 0, expected: 0 }
        { name: "identity", a: 5, expected: 5 }
        { name: "both set", a: 2, b: 3, expected: 5 }
    } do {
        then "check addition" {
            result := scenario.a.(int) + scenario.b.(int)
            expect result == scenario.expected
        }
    }
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled:\n%s", goCode)

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "main_test.go", goCode)

	cmd := exec.Command("go", "test", "-v", "./...")
	cmd.Dir = tmpDir
	out, err := cmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
}

func TestTranspileDanmujiLineDirectives(t *testing.T) {
	source := []byte(`package myservice_test

import "testing"

unit "arithmetic" {
	then "adds" {
		expect 1 + 1 == 2
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{SourceFile: "/tmp/test.dmj"})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "//line /tmp/test.dmj:") {
		t.Error("expected //line /tmp/test.dmj: directive in output when SourceFile is set")
	}
}

func TestTranspileDanmujiDebugOmitsLineDirectives(t *testing.T) {
	source := []byte(`package myservice_test

import "testing"

unit "arithmetic" {
	then "adds" {
		expect 1 + 1 == 2
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{SourceFile: "/tmp/test.dmj", Debug: true})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if strings.Contains(goCode, "//line") {
		t.Error("expected no //line directives when Debug is true")
	}
}

func TestTranspileDanmujiEmptySourceFileOmitsDirectives(t *testing.T) {
	source := []byte(`package myservice_test

import "testing"

unit "arithmetic" {
	then "adds" {
		expect 1 + 1 == 2
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if strings.Contains(goCode, "//line") {
		t.Error("expected no //line directives when SourceFile is empty")
	}
}

func TestTranspileDanmujiProcess(t *testing.T) {
	source := []byte(`package server_test

import "testing"

e2e "server lifecycle" {
	process "./cmd/server" {
		args "--port 8080"
		ready http "http://localhost:8080/health"
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "go build") || !strings.Contains(goCode, `exec.Command("go", "build"`) {
		t.Error("expected go build command in output")
	}
	if !strings.Contains(goCode, "_danmujiProc.Start()") {
		t.Error("expected proc.Start() in output")
	}
	if !strings.Contains(goCode, "http.Get") {
		t.Error("expected http.Get for readiness polling")
	}
	if !strings.Contains(goCode, "require.True") {
		t.Error("expected require.True for readiness assertion")
	}
	if !strings.Contains(goCode, "t.Cleanup") {
		t.Error("expected t.Cleanup for implicit cleanup")
	}
	if !strings.Contains(goCode, "syncBuffer") {
		t.Error("expected syncBuffer type in output")
	}
}

func TestTranspileDanmujiProcessRun(t *testing.T) {
	source := []byte(`package tool_test

import "testing"

e2e "run binary" {
	process run "./bin/mytool" {
		args "--verbose"
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if strings.Contains(goCode, "go build") || strings.Contains(goCode, `exec.Command("go", "build"`) {
		t.Error("expected NO go build command in run mode")
	}
	if !strings.Contains(goCode, `exec.Command("./bin/mytool"`) {
		t.Error("expected direct exec.Command with path")
	}
}

func TestTranspileDanmujiProcessQuotedArgs(t *testing.T) {
	source := []byte(`package tool_test

import "testing"

e2e "quoted args" {
	process run "./bin/mytool" {
		args "--verbose --message 'hello world'"
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}

	if !strings.Contains(goCode, `"--verbose", "--message", "hello world"`) {
		t.Fatalf("expected quoted process args to be preserved, got:\n%s", goCode)
	}
}

func TestTranspileDanmujiProcessEnv(t *testing.T) {
	source := []byte(`package env_test

import "testing"

e2e "env vars" {
	process "./cmd/server" {
		env { DB_URL: "postgres://localhost/test" }
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "os.Environ()") {
		t.Error("expected os.Environ() in output")
	}
	if !strings.Contains(goCode, "DB_URL=postgres://localhost/test") {
		t.Error("expected env var DB_URL in output")
	}
}

func TestTranspileDanmujiReadyModes(t *testing.T) {
	tests := []struct {
		name   string
		ready  string
		expect string
	}{
		{
			name:   "tcp",
			ready:  `ready tcp "localhost:5432"`,
			expect: `net.Dial("tcp"`,
		},
		{
			name:   "stdout",
			ready:  `ready stdout "server started"`,
			expect: "_danmujiProcStdout.String()",
		},
		{
			name:   "delay",
			ready:  `ready delay 5s`,
			expect: "time.Sleep(5 * time.Second)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := []byte(`package ready_test

import "testing"

e2e "ready mode" {
	process "./cmd/server" {
		` + tt.ready + `
	}
}
`)
			goCode, err := TranspileDanmuji(source, TranspileOptions{})
			if err != nil {
				t.Fatalf("transpile: %v", err)
			}
			t.Logf("Transpiled Go:\n%s", goCode)

			if !strings.Contains(goCode, tt.expect) {
				t.Errorf("expected %q in output", tt.expect)
			}
		})
	}
}

func TestTranspileDanmujiStopBlock(t *testing.T) {
	source := []byte(`package server_test

import "testing"

e2e "server shutdown" {
	process run "./bin/server" {
		ready http "http://localhost:8080/health"
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
		t.Error("expected syscall.SIGTERM in output")
	}
	if !strings.Contains(goCode, "30 * time.Second") {
		t.Error("expected 30 * time.Second in output")
	}
	if !strings.Contains(goCode, "exitCode") {
		t.Error("expected exitCode variable in output")
	}
	if !strings.Contains(goCode, "shutdown complete") {
		t.Error("expected 'shutdown complete' in output")
	}
	// Exactly 1 t.Cleanup (from stop block, not implicit).
	count := strings.Count(goCode, "t.Cleanup(")
	if count != 1 {
		t.Errorf("expected exactly 1 t.Cleanup, got %d", count)
	}
}

func TestTranspileDanmujiStopBlockSIGINT(t *testing.T) {
	source := []byte(`package server_test

import "testing"

e2e "server sigint" {
	process run "./bin/server" {
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
		t.Error("expected syscall.SIGINT in output")
	}
}

func TestTranspileDanmujiImplicitCleanup(t *testing.T) {
	source := []byte(`package server_test

import "testing"

e2e "server implicit" {
	process run "./bin/server" {
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "t.Cleanup") {
		t.Error("expected t.Cleanup for implicit cleanup")
	}
	if !strings.Contains(goCode, "syscall.SIGTERM") {
		t.Error("expected syscall.SIGTERM in implicit cleanup")
	}
}
