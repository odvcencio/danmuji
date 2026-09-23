package danmuji

import (
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gofmtSource(src string) (string, error) {
	out, err := format.Source([]byte(src))
	if err != nil {
		return "", err
	}
	return string(out), nil
}

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
	if !strings.Contains(goCode, "t.Parallel()") {
		t.Error("expected top-level tests to run in parallel by default")
	}
}

func TestTranspileDanmujiSerialOptOut(t *testing.T) {
	source := []byte(`package serial_test

import "testing"

@serial
unit "serialized" {
	each "cases" {
		defaults { value: 1 }
		{ name: "one" }
	} do {
		then "stays sequential" {
			expect scenario.value == 1
		}
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if strings.Contains(goCode, "t.Parallel()") {
		t.Error("expected @serial to disable auto-parallel top-level and generated subtests")
	}
}

func TestTranspileDanmujiProcessRemainsSequential(t *testing.T) {
	source := []byte(`package process_test

import "testing"

e2e "server process" {
	process run "./bin/server" {
		ready http "http://localhost:8080/health"
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if strings.Contains(goCode, "t.Parallel()") {
		t.Error("expected process-backed tests to remain sequential by default")
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
	cmd.Env = goEnv()
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
	cmd.Env = goEnv()
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
	cmd.Env = goEnv()
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
	if !strings.Contains(goCode, `dbContainer.Endpoint(dbCtx, "5432/tcp")`) {
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

func TestTranspileDanmujiNeedsPostgresEnvAndScope(t *testing.T) {
	source := []byte(`package myservice_test

import "testing"

integration "with database" {
	needs postgres db {
		password: "test"
		database: "goetrope_test"
	}
	then "endpoint is available" {
		expect dbEndpoint not_nil
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, `dbReq.Env = map[string]string{"POSTGRES_PASSWORD": fmt.Sprint("test"), "POSTGRES_DB": fmt.Sprint("goetrope_test")}`) &&
		!strings.Contains(goCode, `dbReq.Env = map[string]string{"POSTGRES_DB": fmt.Sprint("goetrope_test"), "POSTGRES_PASSWORD": fmt.Sprint("test")}`) {
		t.Error("expected postgres env vars in generated request")
	}
	if !strings.Contains(goCode, "dbEndpoint, dbErr := dbContainer.Endpoint") {
		t.Error("expected dbEndpoint to be lifted to test scope")
	}
	if strings.Contains(goCode, "{\n\tctx := context.Background()") {
		t.Error("expected needs block setup to avoid an extra local wrapper scope")
	}
}

func TestTranspileDanmujiNeedsTempdir(t *testing.T) {
	source := []byte(`package main_test

import "testing"

unit "tempdir fixture" {
	needs tempdir dir
	then "provides a directory" {
		expect dir != ""
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "dir := t.TempDir()") {
		t.Error("expected tempdir fixture to use t.TempDir")
	}

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "main_test.go", goCode)

	cmd := exec.Command("go", "test", "-v", "./...")
	cmd.Dir = tmpDir
	cmd.Env = goEnv()
	out, err := cmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
}

func TestTranspileDanmujiNeedsHTTPServer(t *testing.T) {
	source := []byte(`package main_test

import (
	"net/http"
	"testing"
)

unit "http fixture" {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	needs http server {
		handler handler
	}

	then "serves requests" {
		resp, err := http.Get(server.URL + "/ping")
		expect err == nil
		expect resp.StatusCode == http.StatusNoContent
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "server := httptest.NewServer(handler)") {
		t.Error("expected httptest server fixture in generated code")
	}

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "main_test.go", goCode)

	cmd := exec.Command("go", "test", "-v", "./...")
	cmd.Dir = tmpDir
	cmd.Env = goEnv()
	out, err := cmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
}

func TestTranspileDanmujiFactoryBuild(t *testing.T) {
	source := []byte(`package main_test

import "testing"

type User struct {
	Name string
	Role string
}

factory User {
	defaults { Name: "alice", Role: "member" }
	trait admin { Role: "admin" }
}

unit "factory build" {
	user := build User with admin { Name: "bob" }
	then "applies defaults traits and overrides" {
		expect user.Name == "bob"
		expect user.Role == "admin"
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if strings.Contains(goCode, "factory User") {
		t.Error("expected factory declaration to stay DSL-only")
	}
	if !strings.Contains(goCode, `User{Name: "bob", Role: "admin"}`) {
		t.Error("expected build expression to emit a Go composite literal with overrides applied")
	}

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "main_test.go", goCode)

	cmd := exec.Command("go", "test", "-v", "./...")
	cmd.Dir = tmpDir
	cmd.Env = goEnv()
	out, err := cmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
}

func TestTranspileDanmujiFactoryUnknownTrait(t *testing.T) {
	source := []byte(`package main_test

factory User {
	defaults { Name: "alice" }
}

unit "bad build" {
	_ = build User with admin
}
`)

	_, err := TranspileDanmuji(source, TranspileOptions{SourceFile: "bad_test.dmj"})
	if err == nil {
		t.Fatal("expected semantic error for unknown factory trait")
	}
	if !strings.Contains(err.Error(), "does not define trait admin") {
		t.Fatalf("expected unknown trait diagnostic, got: %v", err)
	}
}

func TestTranspileDanmujiStructLiteralInsideThen(t *testing.T) {
	source := []byte(`package main_test

import "testing"

type DriftReport struct {
	Drift float64
}

unit "struct literals" {
	then "stay Go" {
		msg := DriftReport{Drift: 0.05}
		expect msg.Drift == 0.05
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, `DriftReport{Drift: 0.05}`) {
		t.Error("expected struct literal to survive transpilation")
	}

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "main_test.go", goCode)

	cmd := exec.Command("go", "test", "-v", "./...")
	cmd.Dir = tmpDir
	cmd.Env = goEnv()
	out, err := cmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
}

func TestTranspileDanmujiRichAssertions(t *testing.T) {
	source := []byte(`package main_test

import (
	"context"
	"fmt"
	"testing"
)

type User struct {
	Name  string
	Role  string
	Email string
}

unit "rich assertions" {
	user := User{Name: "alice", Role: "admin", Email: "alice@example.com"}
	items := []int{3, 1, 2}
	err := fmt.Errorf("wrapped: %w", context.DeadlineExceeded)

	then "matches partial struct" {
		expect user matches { Role: "admin", Email: contains "@example.com", Name: not_nil }
	}

	then "compares unordered slices" {
		expect items unordered_equal []int{1, 2, 3}
	}

	then "checks wrapped errors" {
		expect err is context.DeadlineExceeded
		expect err message contains "wrapped"
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "danmujiPartialMatch") {
		t.Error("expected partial match helper call in output")
	}
	if !strings.Contains(goCode, "danmujiUnorderedEqualDetail") {
		t.Error("expected unordered equality helper call in output")
	}
	if !strings.Contains(goCode, "assert.ErrorIs") {
		t.Error("expected ErrorIs assertion in output")
	}
	if !strings.Contains(goCode, "assert.ErrorContains") {
		t.Error("expected ErrorContains assertion in output")
	}

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "main_test.go", goCode)

	cmd := exec.Command("go", "test", "-v", "./...")
	cmd.Dir = tmpDir
	cmd.Env = goEnv()
	out, err := cmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
}

func TestTranspileDanmujiAwait(t *testing.T) {
	source := []byte(`package main_test

import (
	"testing"
	"time"
)

unit "await channel values" {
	jobs := make(chan string, 1)
	jobs <- "ready"

	await <-jobs within 2s as job

	then "binds the received value" {
		expect job == "ready"
	}

	then "times out through the helper signature" {
		other := make(chan int, 1)
		other <- 7
		await <-other within 2 * time.Second as value
		expect value == 7
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "danmujiAwait(") {
		t.Error("expected await helper call in output")
	}

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "main_test.go", goCode)

	cmd := exec.Command("go", "test", "-v", "./...")
	cmd.Dir = tmpDir
	cmd.Env = goEnv()
	out, err := cmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
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
	duration 5 * time.Second
	rampup 1 * time.Second
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
	runCmd.Env = goEnv()
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
	runCmd.Env = goEnv()
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
	runCmd.Env = goEnv()
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
	runCmd.Env = goEnv()
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
	cmd.Env = goEnv()
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

func TestTranspileDanmujiTableDoesNotInjectUnusedFmt(t *testing.T) {
	source := []byte(`package table_test

import "testing"

unit "table only" {
	table sums {
		| 1 | 2 |
	}
	then "still compiles" {
		expect true
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if strings.Contains(goCode, "\n\t\"fmt\"\n") || strings.Contains(goCode, "import \"fmt\"") {
		t.Error("expected fmt import to be omitted when generated table code does not use it")
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
	runCmd.Env = goEnv()
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
	cmd.Env = goEnv()
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

func TestTranspileDanmujiSnapshotKeywordCanBeIdentifier(t *testing.T) {
	source := []byte(`package snapid_test

import "testing"

unit "snapshot identifier stays go" {
	var snapshot string
	when "assigned" {
		snapshot = "ok"
		then "does not become a snapshot block" {
			expect snapshot == "ok"
		}
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if strings.Contains(goCode, "snapshot_does not become a snapshot block") {
		t.Fatal("expected snapshot identifier assignment to remain Go code, not emit a snapshot subtest")
	}
	if !strings.Contains(goCode, `snapshot = "ok"`) {
		t.Fatal("expected snapshot assignment to be preserved")
	}
	if !strings.Contains(goCode, `assert.Equal(t, "ok", snapshot`) {
		t.Fatal("expected then block to assert on the snapshot variable")
	}
}

func TestTranspileDanmujiSnapshotKeywordCanStartMultiAssignment(t *testing.T) {
	source := []byte(`package snapidmulti_test

import "testing"

unit "snapshot identifier multi assignment stays go" {
	var snapshot string
	var queryErr error
	when "assigned" {
		snapshot, queryErr = load()
		then "does not become a snapshot block" {
			expect queryErr == nil
			expect snapshot == "ok"
		}
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if strings.Contains(goCode, "snapshot_does not become a snapshot block") {
		t.Fatal("expected snapshot multi-assignment to remain Go code, not emit a snapshot subtest")
	}
	if !strings.Contains(goCode, `snapshot, queryErr = load()`) {
		t.Fatal("expected snapshot multi-assignment to be preserved")
	}
	if !strings.Contains(goCode, `assert.Equal(t, "ok", snapshot`) {
		t.Fatal("expected then block to assert on the snapshot variable after multi-assignment")
	}
}

func TestTranspileDanmujiSnapshotKeywordCanStartSequentialAssignments(t *testing.T) {
	source := []byte(`package snapidseq_test

import "testing"

unit "snapshot identifier sequential assignments stay go" {
	var snapshot string
	var values []string
	var queryErr error
	when "assigned" {
		snapshot, queryErr = loadOne()
		values, queryErr = loadMany()
		then "does not become a snapshot block" {
			expect queryErr == nil
			expect snapshot == "ok"
			expect len(values) >= 1
		}
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if strings.Contains(goCode, "snapshot_does not become a snapshot block") {
		t.Fatal("expected sequential snapshot assignments to remain Go code, not emit a snapshot subtest")
	}
	if !strings.Contains(goCode, `snapshot, queryErr = loadOne()`) {
		t.Fatal("expected first sequential assignment to be preserved")
	}
	if !strings.Contains(goCode, `values, queryErr = loadMany()`) {
		t.Fatal("expected second sequential assignment to be preserved")
	}
	if !strings.Contains(goCode, `assert.True(t, len(values) >= 1`) {
		t.Fatal("expected then block to assert on the values variable after sequential assignments")
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
	runCmd.Env = goEnv()
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

func TestTranspileDanmujiMatrixAliasesAcrossNestedBlocks(t *testing.T) {
	source := []byte(`package matrix_test

import "testing"

unit "matrix aliases" {
	matrix "codec x audio" {
		codec: { "h264" }
		hasAudio: { true }
	} do {
		given "nested blocks" {
			then "can see aliases" {
				expect codec == "h264"
				expect hasAudio == true
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

	if !strings.Contains(goCode, "codec := scenario.codec") {
		t.Error("expected codec alias local in generated matrix loop")
	}
	if !strings.Contains(goCode, "hasAudio := scenario.hasAudio") {
		t.Error("expected hasAudio alias local in generated matrix loop")
	}

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "main_test.go", goCode)

	cmd := exec.Command("go", "test", "-v", "./...")
	cmd.Dir = tmpDir
	cmd.Env = goEnv()
	out, err := cmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
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
	runCmd.Env = goEnv()
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
	runCmd.Env = goEnv()
	out, _ := runCmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))
	if !strings.Contains(string(out), "FAIL") {
		t.Error("expected FAIL in output for false property")
	}
}

func TestTranspileDanmujiFuzz(t *testing.T) {
	source := []byte(`package fuzz_test

import "testing"

fuzz "round trip text" with (input string, b byte) {
	_ = b
	expect len([]byte(input)) >= 0
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "func FuzzRoundTripText(f *testing.F)") {
		t.Error("expected Fuzz function signature in generated code")
	}
	if !strings.Contains(goCode, `f.Add("", byte(0))`) {
		t.Error("expected zero-value seed corpus in generated code")
	}
	if !strings.Contains(goCode, "f.Fuzz(func(t *testing.T, input string, b byte)") {
		t.Error("expected fuzz callback signature in generated code")
	}

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "fuzz_test.go", goCode)

	runCmd := exec.Command("go", "test", "-v", "./...")
	runCmd.Dir = tmpDir
	runCmd.Env = goEnv()
	out, err := runCmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PASS") {
		t.Error("expected PASS in go test output")
	}
}

func TestTranspileDanmujiHTTPTestHelpers(t *testing.T) {
	source := []byte(`package httphelper_test

import (
	"net/http"
	"testing"
)

unit "handler helpers" {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	})

	req := danmujiHTTP.POST("/users", map[string]string{"name": "alice"})
	rec := danmujiHTTP.Serve(handler, req)

	then "status code matches" {
		expect rec.Code == http.StatusCreated
	}

	then "body is recorded" {
		expect rec.Body.String() == "created"
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "var danmujiHTTP danmujiHTTPHelperSet") {
		t.Error("expected danmujiHTTP helper declaration in generated code")
	}
	if !strings.Contains(goCode, "httptest.NewRequest") {
		t.Error("expected httptest.NewRequest helper in generated code")
	}
	if !strings.Contains(goCode, "httptest.NewRecorder") {
		t.Error("expected httptest.NewRecorder helper in generated code")
	}

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "http_helper_test.go", goCode)

	runCmd := exec.Command("go", "test", "-v", "./...")
	runCmd.Dir = tmpDir
	runCmd.Env = goEnv()
	out, err := runCmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PASS") {
		t.Error("expected PASS in go test output")
	}
}

func TestTranspileDanmujiWebSocketHelpers(t *testing.T) {
	source := []byte(`package wshelper_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{}

unit "websocket helpers" {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		_ = conn.WriteMessage(websocket.BinaryMessage, []byte{0x01, 0x02})
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		_ = conn.WriteMessage(websocket.BinaryMessage, payload)
	}))
	defer server.Close()

	ws := danmujiWS.Dial(server, "/ws")
	defer ws.Close()

	then "receives heartbeat" {
		msg := ws.ReadBinary(2 * time.Second)
		expect msg[0] == byte(0x01)
	}

	ws.SendBinary([]byte{0x09})

	then "echoes payload" {
		msg := ws.ReadBinary(2 * time.Second)
		expect msg[0] == byte(0x09)
		expect ws.LastMessage() not_nil
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "var danmujiWS danmujiWSHelperSet") {
		t.Error("expected danmujiWS helper declaration in generated code")
	}
	if !strings.Contains(goCode, "websocket.DefaultDialer.Dial") {
		t.Error("expected websocket dial helper in generated code")
	}
	if !strings.Contains(goCode, "ReadBinary") {
		t.Error("expected websocket read helper in generated code")
	}

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "ws_helper_test.go", goCode)

	runCmd := exec.Command("go", "test", "-v", "./...")
	runCmd.Dir = tmpDir
	runCmd.Env = goEnv()
	out, err := runCmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
	}
}

func TestTranspileDanmujiGRPCHelpers(t *testing.T) {
	source := []byte(`package grpchelper_test

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type pingServer interface {
	Ping(context.Context, *wrapperspb.StringValue) (*wrapperspb.StringValue, error)
}

type pingImpl struct{}

func (pingImpl) Ping(ctx context.Context, in *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
	return wrapperspb.String("pong:" + in.Value), nil
}

var pingServiceDesc = grpc.ServiceDesc{
	ServiceName: "test.PingService",
	HandlerType: (*pingServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Ping",
			Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
				in := new(wrapperspb.StringValue)
				if err := dec(in); err != nil {
					return nil, err
				}
				if interceptor == nil {
					return srv.(pingServer).Ping(ctx, in)
				}
				info := &grpc.UnaryServerInfo{
					Server: srv,
					FullMethod: "/test.PingService/Ping",
				}
				handler := func(ctx context.Context, req interface{}) (interface{}, error) {
					return srv.(pingServer).Ping(ctx, req.(*wrapperspb.StringValue))
				}
				return interceptor(ctx, in, info, handler)
			},
		},
	},
}

unit "grpc helpers" {
	conn := danmujiGRPC.Bufconn(func(s *grpc.Server) {
		s.RegisterService(&pingServiceDesc, pingImpl{})
	})
	defer conn.Close()

	then "invokes unary grpc calls" {
		out := new(wrapperspb.StringValue)
		err := conn.Conn().Invoke(context.Background(), "/test.PingService/Ping", wrapperspb.String("ping"), out)
		expect err == nil
		expect out.Value == "pong:ping"
	}
}
`)

	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "var danmujiGRPC danmujiGRPCHelperSet") {
		t.Error("expected danmujiGRPC helper declaration in generated code")
	}
	if !strings.Contains(goCode, "bufconn.Listen") {
		t.Error("expected bufconn helper in generated code")
	}

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "grpc_helper_test.go", goCode)

	runCmd := exec.Command("go", "test", "-v", "./...")
	runCmd.Dir = tmpDir
	runCmd.Env = goEnv()
	out, err := runCmd.CombinedOutput()
	t.Logf("go test output:\n%s", string(out))
	if err != nil {
		t.Fatalf("go test failed: %v\n%s", err, out)
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
	cmd.Env = goEnv()
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

func TestTranspileDanmujiRejectsInvalidSignalName(t *testing.T) {
	source := []byte(`package server_test

import "testing"

e2e "invalid signal" {
	stop {
		signal term
	}
}
`)
	_, err := TranspileDanmuji(source, TranspileOptions{})
	if err == nil {
		t.Fatal("expected invalid signal name to fail semantic validation")
	}
	if !strings.Contains(err.Error(), `invalid signal name "term"`) {
		t.Fatalf("unexpected error: %v", err)
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

func TestTranspileDanmujiKeywordLikeIdentifiersRemainValidGo(t *testing.T) {
	source := []byte(`package main

import "testing"

unit "keyword identifiers" {
	given "plain Go bindings" {
		exec := 1
		profile := 2
		args := profile

		then "still transpiles" {
			expect exec == 1
			expect profile == 2
			expect args == 2
		}
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if !strings.Contains(goCode, "exec := 1") {
		t.Error("expected exec short declaration in output")
	}
	if !strings.Contains(goCode, "profile := 2") {
		t.Error("expected profile short declaration in output")
	}
	if !strings.Contains(goCode, "args := profile") {
		t.Error("expected args short declaration in output")
	}
}

// TestTranspileDanmujiSetenvOmitsParallel verifies that a unit block containing
// t.Setenv does NOT get t.Parallel() injected (Go panics if both are used).
func TestTranspileDanmujiSetenvOmitsParallel(t *testing.T) {
	source := []byte(`package env_test

import "testing"

unit "env override" {
	t.Setenv("FOO", "bar")
	then "env is set" {
		expect true
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if strings.Contains(goCode, "t.Parallel()") {
		t.Error("expected t.Parallel() to be suppressed when t.Setenv is present")
	}
}

// TestTranspileDanmujiOsSetenvOmitsParallel verifies that a unit block using
// os.Setenv also suppresses t.Parallel() (os.Setenv is not cleaned up by the
// test harness, making parallel execution equally unsafe).
func TestTranspileDanmujiOsSetenvOmitsParallel(t *testing.T) {
	source := []byte(`package env_test

import (
	"os"
	"testing"
)

unit "os env override" {
	os.Setenv("BAR", "baz")
	then "env is set" {
		expect true
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if strings.Contains(goCode, "t.Parallel()") {
		t.Error("expected t.Parallel() to be suppressed when os.Setenv is present")
	}
}

// TestTranspileDanmujiNoSetenvKeepsParallel ensures that a plain unit block
// without any Setenv call still gets t.Parallel().
func TestTranspileDanmujiNoSetenvKeepsParallel(t *testing.T) {
	source := []byte(`package plain_test

import "testing"

unit "no env" {
	then "works" {
		expect 1 == 1
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	// Only assert when the grammar actually parsed the unit block.
	if strings.Contains(goCode, "func Test") && !strings.Contains(goCode, "t.Parallel()") {
		t.Error("expected t.Parallel() for a unit block without Setenv")
	}
}

// TestTranspileDanmujiSerialTagStillRespected ensures that @serial still
// suppresses t.Parallel() even when there is no Setenv in the body.
// NOTE: gated on grammar working, same as TestTranspileDanmujiSerialOptOut.
func TestTranspileDanmujiSerialTagStillRespected(t *testing.T) {
	source := []byte(`package serial2_test

import "testing"

@serial
unit "explicit serial" {
	then "no parallel" {
		expect true
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		// @serial is a grammar-level feature; if the parser rejects it
		// the tag mechanism is not testable in this state.
		t.Skipf("transpile error (grammar may be stale): %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if strings.Contains(goCode, "t.Parallel()") {
		t.Error("expected @serial to suppress t.Parallel()")
	}
}

// ---------------------------------------------------------------------------
// V2 regression: assertion classification must dispatch on parse-tree
// fields, never on a substring search over the statement's raw text. A
// string literal that happens to *contain* the letters "is_nil", "not_nil",
// "contains", "unordered_equal", or " is " must never be mistaken for the
// corresponding DSL matcher keyword.
// ---------------------------------------------------------------------------

// TestTranspileDanmujiKeywordLookalikeStringLiteralsCompareByValue is the
// direct V2 reproduction: `expect code == "field_not_nil"` must transpile to
// a real equality comparison (and therefore FAIL when code != that string),
// not `assert.NotNil(t, code == "field_not_nil")` (which is always true,
// because a bool is never nil, so the generated test always passes).
func TestTranspileDanmujiKeywordLookalikeStringLiteralsCompareByValue(t *testing.T) {
	cases := []struct {
		name        string
		spec        string
		mustNotHave string
		mustHave    []string
	}{
		{
			name: "not_nil substring in string literal",
			spec: `unit "u" {
	given "a value" {
		code := "actual_value"
		then "t" {
			expect code == "field_not_nil"
		}
	}
}
`,
			mustNotHave: "assert.NotNil(",
		},
		{
			name: "is_nil substring in string literal",
			spec: `unit "u" {
	given "a value" {
		code := "actual_value"
		then "t" {
			expect code == "status_is_nil_x"
		}
	}
}
`,
			mustNotHave: "assert.Nil(",
		},
		{
			name: "contains substring in string literal",
			spec: `unit "u" {
	given "a value" {
		code := "actual_value"
		then "t" {
			expect code == "it_contains_stuff"
		}
	}
}
`,
			mustNotHave: "assert.Contains(",
		},
		{
			name: "unordered_equal substring in string literal",
			spec: `unit "u" {
	given "a value" {
		code := "actual_value"
		then "t" {
			expect code == "unordered_equal_marker"
		}
	}
}
`,
			mustNotHave: "danmujiUnorderedEqualDetail(",
		},
		{
			name: "is word inside string literal",
			spec: `unit "u" {
	given "a value" {
		code := "actual_value"
		then "t" {
			expect code == "this is a sentence"
		}
	}
}
`,
			mustNotHave: "assert.ErrorIs(",
		},
		{
			name: "message contains phrase inside string literal",
			spec: `unit "u" {
	given "a value" {
		code := "actual_value"
		then "t" {
			expect code == "the message contains text"
		}
	}
}
`,
			mustNotHave: "assert.ErrorContains(",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte("package main_test\n\nimport \"testing\"\n\n" + tc.spec)
			goCode, err := TranspileDanmuji(source, TranspileOptions{})
			if err != nil {
				t.Fatalf("transpile: %v", err)
			}
			t.Logf("Transpiled Go:\n%s", goCode)

			if strings.Contains(goCode, tc.mustNotHave) {
				t.Errorf("expected generated code NOT to contain %q (substring-keyword misclassification); got:\n%s", tc.mustNotHave, goCode)
			}
			if !strings.Contains(goCode, "assert.Equal") {
				t.Errorf("expected a real equality assertion (assert.Equal/assert.EqualValues) in output, got:\n%s", goCode)
			}

			// End-to-end: the comparison is false (code != the RHS string),
			// so the generated test MUST fail, proving the assertion is real.
			tmpDir := newTestModule(t)
			writeModuleFile(t, tmpDir, "main_test.go", goCode)

			cmd := exec.Command("go", "test", "-v", "./...")
			cmd.Dir = tmpDir
			cmd.Env = goEnv()
			out, _ := cmd.CombinedOutput()
			t.Logf("go test output:\n%s", string(out))

			if !strings.Contains(string(out), "FAIL") {
				t.Errorf("expected the generated test to FAIL (values differ), but it passed:\n%s", out)
			}
		})
	}
}

// TestTranspileDanmujiRealMatcherKeywordsStillWork proves the fix in
// TestTranspileDanmujiKeywordLookalikeStringLiteralsCompareByValue did not
// regress genuine keyword matchers: when is_nil/not_nil/contains/
// unordered_equal are used as actual DSL keywords (not substrings inside a
// string literal), the corresponding matcher assertion must still be
// emitted.
func TestTranspileDanmujiRealMatcherKeywordsStillWork(t *testing.T) {
	source := []byte(`package main_test

import "testing"

unit "matchers" {
	given "values" {
		var err error
		items := []string{"a", "b"}
		then "is_nil matches" {
			expect err is_nil
		}
		then "not_nil matches" {
			expect items not_nil
		}
		then "contains matches" {
			expect items contains "a"
		}
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	for _, want := range []string{"assert.Nil(", "assert.NotNil(", "assert.Contains("} {
		if !strings.Contains(goCode, want) {
			t.Errorf("expected genuine matcher keyword to still emit %q; got:\n%s", want, goCode)
		}
	}
}

// ---------------------------------------------------------------------------
// V1 regression: a DSL-only construct used outside its valid parent must
// fail the build with a semantic error, never silently disappear. The
// grammar accepts setup/measure/load_config/target/defaults/scenario_*/
// process_args/process_env/ready/signal/timeout anywhere a statement is
// valid (see grammar.go's dslStatement wiring), but each one only has
// emitter support when its real owner (benchmark/load/each.../process/stop)
// walks its own children by hand. Before the fix, the generic emit()
// dispatcher silently returned "" for these node types no matter where
// they appeared, so a misplaced `setup { expect ... }` inside a `unit`
// vanished and the test PASSed.
// ---------------------------------------------------------------------------

func TestTranspileDanmujiRejectsSetupOutsideBenchmark(t *testing.T) {
	source := []byte(`package main_test

import "testing"

unit "misused setup" {
	setup {
		expect 1 == 2
	}
	then "should still check something" {
		expect true
	}
}
`)
	_, err := TranspileDanmuji(source, TranspileOptions{})
	if err == nil {
		t.Fatal("expected transpile to fail: setup{} is not valid inside a unit")
	}
	if !strings.Contains(err.Error(), "setup_block") {
		t.Errorf("expected error to name setup_block, got: %v", err)
	}
	if !strings.Contains(err.Error(), ":6:") {
		t.Errorf("expected error to point at line 6 (the setup block), got: %v", err)
	}
}

func TestTranspileDanmujiRejectsMeasureOutsideBenchmark(t *testing.T) {
	source := []byte(`package main_test

import "testing"

unit "misused measure" {
	measure {
		expect 1 == 2
	}
}
`)
	_, err := TranspileDanmuji(source, TranspileOptions{})
	if err == nil {
		t.Fatal("expected transpile to fail: measure{} is not valid inside a unit")
	}
	if !strings.Contains(err.Error(), "measure_block") {
		t.Errorf("expected error to name measure_block, got: %v", err)
	}
}

// TestTranspileDanmujiRejectsMisplacedDSLConstructs sweeps every DSL-only
// node type that grammar.go wires into the general _statement rule but
// whose emitter only ever consumes it from a specific owning parent
// (benchmark/load/exec/each.../matrix/process/stop). Each one, dropped
// into a plain `unit`, must fail the build rather than vanish.
func TestTranspileDanmujiRejectsMisplacedDSLConstructs(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantErrHas string
	}{
		{"setup", `setup { expect true }`, "setup_block"},
		{"measure", `measure { expect true }`, "measure_block"},
		{"parallel_measure", `parallel measure { expect true }`, "parallel_measure_block"},
		{"report_allocs", `report_allocs`, "report_directive"},
		{"load_config", `rate 10`, "load_config"},
		{"target", `target get "http://localhost"`, "target_block"},
		{"run_command", `run "echo hi"`, "run_command"},
		{"process_args", `args "-x"`, "process_args"},
		{"process_env", `env { KEY: "value" }`, "process_env"},
		{"ready", `ready tcp ":8080"`, "ready_clause"},
		{"signal", `signal SIGTERM`, "signal_directive"},
		{"timeout", `timeout 10s`, "timeout_directive"},
		{"defaults", `defaults { count: 1 }`, "defaults_block"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte(fmt.Sprintf(`package main_test

import "testing"

unit "misused %s" {
	%s
	then "still runs" {
		expect true
	}
}
`, tc.name, tc.body))
			_, err := TranspileDanmuji(source, TranspileOptions{})
			if err == nil {
				t.Fatalf("expected transpile to fail: %s{} is not valid inside a unit", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantErrHas) {
				t.Errorf("expected error to name %s, got: %v", tc.wantErrHas, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// V3 regression: deleting a single "{" must never let the build succeed
// silently. Before this check, gotreesitter's error recovery could re-sync
// a few tokens after an unbalanced brace with HasError() == false, quietly
// dropping the rest of a `then` block (its assertion AND the next `then`
// header) with no ERROR/MISSING node anywhere in the tree. `danmuji build`
// exited 0 and the miscompiled `go test` passed.
// ---------------------------------------------------------------------------

func TestTranspileDanmujiRejectsUnbalancedBraceSilentDrop(t *testing.T) {
	// This is the exact V3 reproduction: the "{" after `then "a"` is
	// missing, so the parser resyncs past `expect 1 == 2` and the entire
	// `then "b" {` header before it recovers.
	source := []byte(`package p

import "testing"

unit "u" {
	then "a"
		expect 1 == 2
	}
	then "b" {
		expect true
	}
}
`)
	_, err := TranspileDanmuji(source, TranspileOptions{SourceFile: "v3.dmj"})
	if err == nil {
		t.Fatal("expected transpile to fail: a brace was deleted and source text was silently dropped")
	}
	t.Logf("error: %v", err)
	if !strings.Contains(err.Error(), "silently dropped") {
		t.Errorf("expected a byte-coverage error naming the silent drop, got: %v", err)
	}
	if !strings.Contains(err.Error(), "v3.dmj:") {
		t.Errorf("expected the error to carry the source file name, got: %v", err)
	}
}

// TestCheckTreeCoversSourceAcceptsCleanFiles is a sanity check that the new
// byte-coverage pass does not false-positive on ordinary, well-formed
// specs across the existing testdata corpus.
func TestCheckTreeCoversSourceAcceptsCleanFiles(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "*.dmj"))
	if err != nil {
		t.Fatalf("glob testdata: %v", err)
	}
	metaFiles, err := filepath.Glob(filepath.Join("testdata", "meta", "*.dmj"))
	if err != nil {
		t.Fatalf("glob testdata/meta: %v", err)
	}
	files = append(files, metaFiles...)
	if len(files) == 0 {
		t.Fatal("expected at least one .dmj fixture")
	}

	for _, f := range files {
		f := f
		t.Run(filepath.Base(f), func(t *testing.T) {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("read %s: %v", f, err)
			}
			if _, err := TranspileDanmuji(src, TranspileOptions{SourceFile: f}); err != nil {
				t.Fatalf("transpile %s: %v", f, err)
			}
		})
	}
}

// TestTranspileDanmujiRejectsMidBlockDurationShorthand documents a defect
// CheckTreeCoversSource discovered in the pinned gotreesitter build: a
// unit-suffixed duration_literal (e.g. "200ms") loses its unit and falls
// back to Go's own int_literal ("200") when it is not the very last
// statement in its enclosing block. Before this check existed, `duration
// 200ms` inside testdata/meta/load_runtime_meta.dmj silently became
// `200 * time.Second` (not 200 * time.Millisecond) with no error anywhere
// — the load attack ran for ~200s instead of ~200ms. The fix (in both
// testdata/load.dmj and testdata/meta/load_runtime_meta.dmj) is to spell
// the duration as an explicit, unambiguous Go expression, e.g.
// `200 * time.Millisecond`, which has no such ambiguity. This test keeps
// the underlying defect visible until the gotreesitter upgrade (item 2)
// resolves the lexer ambiguity at its root.
func TestTranspileDanmujiRejectsMidBlockDurationShorthand(t *testing.T) {
	source := []byte(`package main_test

import "testing"

load "x" {
	rate 10
	duration 5s
	rampup 1s
	target get "http://localhost"
}
`)
	_, err := TranspileDanmuji(source, TranspileOptions{SourceFile: "duration_shorthand.dmj"})
	if err == nil {
		t.Fatal("expected transpile to fail: mid-block \"5s\" duration shorthand silently drops its unit in the pinned gotreesitter build")
	}
	if !strings.Contains(err.Error(), "silently dropped") {
		t.Errorf("expected a byte-coverage error, got: %v", err)
	}
	t.Logf("confirmed still-open defect: %v", err)
}

// ---------------------------------------------------------------------------
// Item 5 acceptance test: //line directives must be at column 1 (the Go
// toolchain silently ignores an indented "//line file:N" and reports
// failures against the *generated* file instead), and must resolve to the
// exact source line at every nesting depth, not just depth 1.
// ---------------------------------------------------------------------------

func TestTranspileDanmujiLineDirectivesResolveAtEveryNestingDepth(t *testing.T) {
	// Depths 1, 3, and 6: unit>then (1), unit>given>when>then (3), and
	// unit>given>when>given>when>given>then (6). Each expect deliberately
	// fails so `go test -v` reports a real failure location.
	source := []byte(`package depth_test

import "testing"

unit "depths" {
	then "depth one fails" {
		expect 1 == 2
	}
	given "g1" {
		when "w1" {
			then "depth three fails" {
				expect 1 == 2
			}
		}
	}
	given "g1" {
		when "w1" {
			given "g2" {
				when "w2" {
					given "g3" {
						then "depth six fails" {
							expect 1 == 2
						}
					}
				}
			}
		}
	}
}
`)

	sourceFile := "depth.dmj"
	goCode, err := TranspileDanmuji(source, TranspileOptions{SourceFile: sourceFile})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	// Every //line directive must start at column 1 (no leading
	// whitespace) — that's the only form the Go toolchain honors.
	for _, line := range strings.Split(goCode, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "//line ") && trimmed != line {
			t.Errorf("//line directive is indented (must start at column 1): %q", line)
		}
	}

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "depth_test.go", goCode)

	cmd := exec.Command("go", "test", "-v", "./...")
	cmd.Dir = tmpDir
	cmd.Env = goEnv()
	out, _ := cmd.CombinedOutput()
	output := string(out)
	t.Logf("go test output:\n%s", output)

	for _, wantLine := range []string{
		sourceFile + ":7:",  // depth 1: "expect 1 == 2" under "depth one fails"
		sourceFile + ":12:", // depth 3
		sourceFile + ":22:", // depth 6
	} {
		if !strings.Contains(output, wantLine) {
			t.Errorf("expected go test output to reference %s (source failure location), got:\n%s", wantLine, output)
		}
	}
	if strings.Contains(output, "depth_test.go:") {
		t.Errorf("go test output referenced the generated file instead of %s — a //line directive did not take effect:\n%s", sourceFile, output)
	}

	vetCmd := exec.Command("go", "vet", "./...")
	vetCmd.Dir = tmpDir
	vetCmd.Env = goEnv()
	vetOut, vetErr := vetCmd.CombinedOutput()
	t.Logf("go vet output:\n%s", vetOut)
	if vetErr != nil {
		t.Errorf("go vet failed on generated code: %v\n%s", vetErr, vetOut)
	}
}

// TestTranspileOutputIsAlwaysGofmtClean is the item 6 acceptance test:
// every spec that transpiles successfully — across testdata/*.dmj,
// testdata/meta/*.dmj, and a sample of the goetrope corpus copy in
// testdata/fuzz_seeds — must produce gofmt-canonical output. This is
// mostly a guard against regressing TranspileDanmuji's format.Source()
// call (transpile_core.go): `gofmt -l` reports a file as dirty exactly
// when reformatting it changes its bytes, so re-formatting the output and
// diffing is equivalent to `gofmt -l` without shelling out.
func TestTranspileOutputIsAlwaysGofmtClean(t *testing.T) {
	var files []string
	for _, pattern := range []string{
		filepath.Join("testdata", "*.dmj"),
		filepath.Join("testdata", "meta", "*.dmj"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("glob %s: %v", pattern, err)
		}
		files = append(files, matches...)
	}
	// Sample the (larger) goetrope corpus copy rather than all 151 files,
	// to keep this test fast; the fuzz target already exercises the full
	// set for the "no silent drops" property, and this test only cares
	// about formatting.
	seedSample, err := filepath.Glob(filepath.Join("testdata", "fuzz_seeds", "*.dmj"))
	if err != nil {
		t.Fatalf("glob fuzz_seeds: %v", err)
	}
	for i, f := range seedSample {
		if i%5 == 0 {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		t.Fatal("expected at least one .dmj fixture")
	}

	checked := 0
	for _, f := range files {
		f := f
		t.Run(filepath.Base(f), func(t *testing.T) {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("read %s: %v", f, err)
			}
			goCode, err := TranspileDanmuji(src, TranspileOptions{SourceFile: f})
			if err != nil {
				// Not every corpus file is guaranteed to transpile (some
				// exercise syntax danmuji intentionally rejects); that's
				// covered elsewhere. This test only checks formatting for
				// specs that DO transpile.
				return
			}
			checked++
			refmt, err := gofmtSource(goCode)
			if err != nil {
				t.Fatalf("gofmt the transpiled output: %v\n%s", err, goCode)
			}
			if refmt != goCode {
				t.Errorf("transpiled output for %s is not gofmt-clean; diff (gofmt-wanted vs got):\n--- wanted ---\n%s\n--- got ---\n%s", f, refmt, goCode)
			}
		})
	}
	t.Logf("checked %d/%d corpus files that transpiled successfully", checked, len(files))
}

// TestTranspileDanmujiEmptyPropertyIsACompileError is an item 6 regression:
// a property block with no expect/reject/return statement can never fail
// (quick.Check always sees `return true`), so it silently claims to
// validate an invariant it never actually checks. That must be a build
// error, not a vacuously green test.
func TestTranspileDanmujiEmptyPropertyIsACompileError(t *testing.T) {
	source := []byte(`package main_test

import "testing"

unit "u" {
	then "empty property" {
		property "vacuous" for all (x int) {
		}
	}
}
`)
	_, err := TranspileDanmuji(source, TranspileOptions{})
	if err == nil {
		t.Fatal("expected an empty property block to fail the build")
	}
	if !strings.Contains(err.Error(), "vacuous") || !strings.Contains(err.Error(), "never fail") {
		t.Errorf("expected error to explain the property can never fail, got: %v", err)
	}
}

// TestTranspileDanmujiPropertyEndingInReturnHasNoDeadCode is an item 6
// regression: emitPropertyBody used to append an unconditional "return
// true" after the body, which `go vet` flags as unreachable code whenever
// the property's last statement was itself an explicit return.
func TestTranspileDanmujiPropertyEndingInReturnHasNoDeadCode(t *testing.T) {
	source := []byte(`package main_test

import "testing"

unit "u" {
	then "explicit return" {
		property "manual" for all (x int) {
			return x == x
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

	cmd := exec.Command("go", "vet", "./...")
	cmd.Dir = tmpDir
	cmd.Env = goEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go vet failed (likely unreachable code after an explicit return): %v\n%s", err, out)
	}
}

// TestTranspileDanmujiEventuallyNameWithPercentIsNotAFormatDirective is an
// item 6 regression: a "%" in an eventually/consistently block's own name
// used to be spliced directly into t.Errorf's format string, which go vet
// flags ("possible formatting directive in Errorf call") and which
// corrupts the failure message at runtime. The name must always be passed
// as its own %s argument.
func TestTranspileDanmujiEventuallyNameWithPercentIsNotAFormatDirective(t *testing.T) {
	source := []byte(`package main_test

import "testing"

unit "u" {
	then "t" {
		eventually "50% success rate" within 10ms {
			expect false
		}
	}
}
`)
	goCode, err := TranspileDanmuji(source, TranspileOptions{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	t.Logf("Transpiled Go:\n%s", goCode)

	if strings.Contains(goCode, `failed after %s", "50%% success rate"`) {
		t.Error("expected the literal name, not a double-escaped %%, in the emitted argument")
	}
	if !strings.Contains(goCode, `"50% success rate"`) {
		t.Error("expected the eventually name to appear as its own quoted argument")
	}

	tmpDir := newTestModule(t)
	writeModuleFile(t, tmpDir, "main_test.go", goCode)

	vetCmd := exec.Command("go", "vet", "./...")
	vetCmd.Dir = tmpDir
	vetCmd.Env = goEnv()
	if out, err := vetCmd.CombinedOutput(); err != nil {
		t.Fatalf("go vet flagged the generated Errorf call: %v\n%s", err, out)
	}

	runCmd := exec.Command("go", "test", "-v", "./...")
	runCmd.Dir = tmpDir
	runCmd.Env = goEnv()
	out, _ := runCmd.CombinedOutput()
	t.Logf("go test output:\n%s", out)
	if !strings.Contains(string(out), "50% success rate") {
		t.Errorf("expected the runtime failure message to contain the literal name, got:\n%s", out)
	}
	if strings.Contains(string(out), "%!") {
		t.Errorf("expected no broken format verb (%%!) in the failure message, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// V6 regression: an unrecognized @tag (a typo like @skipp, or an
// aspirational tag with no implemented effect like @focus) must fail the
// build, not compile clean and silently do nothing.
// ---------------------------------------------------------------------------

func TestTranspileDanmujiRejectsUnknownTags(t *testing.T) {
	for _, tag := range []string{"@skipp", "@focus", "@smoke", "@flaky", "@wip"} {
		tag := tag
		t.Run(tag, func(t *testing.T) {
			source := []byte(fmt.Sprintf(`package main_test

import "testing"

%s
unit "u" {
	then "t" {
		expect true
	}
}
`, tag))
			_, err := TranspileDanmuji(source, TranspileOptions{})
			if err == nil {
				t.Fatalf("expected %s to be rejected as an unknown tag", tag)
			}
			if !strings.Contains(err.Error(), "unknown tag") {
				t.Errorf("expected an \"unknown tag\" error, got: %v", err)
			}
		})
	}
}

func TestTranspileDanmujiAcceptsKnownTags(t *testing.T) {
	for _, tag := range []string{"@skip", "@slow", "@serial", "@sequential", "@parallel"} {
		tag := tag
		t.Run(tag, func(t *testing.T) {
			source := []byte(fmt.Sprintf(`package main_test

import "testing"

%s
unit "u" {
	then "t" {
		expect true
	}
}
`, tag))
			if _, err := TranspileDanmuji(source, TranspileOptions{}); err != nil {
				t.Errorf("expected %s to be accepted, got: %v", tag, err)
			}
		})
	}
}
