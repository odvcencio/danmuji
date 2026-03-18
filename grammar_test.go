package danmuji

import (
	"fmt"
	"strings"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// danmujiLang is a package-level cached language to avoid regenerating for each test.
var danmujiLang *gotreesitter.Language

func getDanmujiLang(t *testing.T) *gotreesitter.Language {
	t.Helper()
	if danmujiLang != nil {
		return danmujiLang
	}
	g := DanmujiGrammar()
	lang, err := GenerateLanguage(g)
	if err != nil {
		t.Fatalf("GenerateLanguage(DanmujiGrammar) failed: %v", err)
	}
	danmujiLang = lang
	return lang
}

func parseDanmuji(t *testing.T, input string) string {
	t.Helper()
	lang := getDanmujiLang(t)
	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if tree == nil {
		t.Fatal("Parse returned nil tree")
	}
	root := tree.RootNode()
	if root == nil {
		t.Fatal("Root node is nil")
	}
	return root.SExpr(lang)
}

// TestDanmujiGoCompat verifies that pure Go code still parses cleanly.
func TestDanmujiGoCompat(t *testing.T) {
	samples := []struct {
		name  string
		input string
	}{
		{
			"hello_world",
			`package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`,
		},
		{
			"variable_decl",
			`package main

var x int = 42
`,
		},
		{
			"if_else",
			`package main

func f() {
	if x > 0 {
		return
	} else {
		x = 0
	}
}
`,
		},
		{
			"for_loop",
			`package main

func f() {
	for i := 0; i < 10; i++ {
		_ = i
	}
}
`,
		},
		{
			"struct",
			`package main

type Point struct {
	X int
	Y int
}
`,
		},
	}

	for _, tt := range samples {
		t.Run(tt.name, func(t *testing.T) {
			sexp := parseDanmuji(t, tt.input)
			t.Logf("SExpr: %s", sexp)
			if strings.Contains(sexp, "ERROR") {
				t.Errorf("pure Go should parse clean, got ERROR in: %s", sexp)
			}
		})
	}
}

// TestDanmujiUnitBlock tests basic unit test block parsing.
func TestDanmujiUnitBlock(t *testing.T) {
	input := `package main
unit "arithmetic" {
	x := 1
	_ = x
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "test_block") {
		t.Error("expected test_block node in parse tree")
	}
	if !strings.Contains(sexp, "test_category") {
		t.Error("expected test_category node in parse tree")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR in parse tree: %s", sexp)
	}
}

// TestDanmujiGivenWhenThen tests BDD given/when/then structure.
func TestDanmujiGivenWhenThen(t *testing.T) {
	input := `package main
func f() {
	given "a user" {
		when "they login" {
			then "they see dashboard" {
				_ = true
			}
		}
	}
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "given_block") {
		t.Error("expected given_block node")
	}
	if !strings.Contains(sexp, "when_block") {
		t.Error("expected when_block node")
	}
	if !strings.Contains(sexp, "then_block") {
		t.Error("expected then_block node")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}

// TestDanmujiExpect tests assertion expressions.
func TestDanmujiExpect(t *testing.T) {
	input := `package main
func f() {
	expect x == 1
	expect err != nil
	expect user to_have_role "admin"
	expect order to_be_paid
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "expect_statement") {
		t.Error("expected expect_statement node")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}

func TestDanmujiEventuallyAndConsistently(t *testing.T) {
	input := `package main
func f() {
	eventually "job completes" within 5s {
		expect jobDone == true
	}
	consistently "no duplicates" for 2s {
		reject hasDuplicates
	}
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "eventually_block") {
		t.Error("expected eventually_block node")
	}
	if !strings.Contains(sexp, "consistently_block") {
		t.Error("expected consistently_block node")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}

// TestDanmujiMock tests mock declaration parsing.
func TestDanmujiMock(t *testing.T) {
	input := `package main
func f() {
	mock Repo {
		Save(u User) -> error = nil
	}
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "mock_declaration") {
		t.Error("expected mock_declaration node")
	}
	if !strings.Contains(sexp, "mock_method") {
		t.Error("expected mock_method node")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}

// TestDanmujiLifecycle tests lifecycle hooks.
func TestDanmujiLifecycle(t *testing.T) {
	input := `package main
func f() {
	before each {
		x := 0
		_ = x
	}
	after each {
		x := 0
		_ = x
	}
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "lifecycle_hook") {
		t.Error("expected lifecycle_hook node")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}

// TestDanmujiTags tests tagged test blocks.
func TestDanmujiTags(t *testing.T) {
	input := `package main
@slow @smoke integration "full suite" {
	_ = true
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "tag") {
		t.Error("expected tag node")
	}
	if !strings.Contains(sexp, "test_block") {
		t.Error("expected test_block node")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}

// TestDanmujiBenchmark tests benchmark block parsing.
func TestDanmujiBenchmark(t *testing.T) {
	input := `package main
benchmark "JSON marshal" {
	setup {
		data := makeLargeStruct()
	}
	measure {
		json.Marshal(data)
	}
	report_allocs
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "benchmark_block") {
		t.Error("expected benchmark_block node")
	}
	if !strings.Contains(sexp, "measure_block") {
		t.Error("expected measure_block node")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}

// TestDanmujiParallelBenchmark tests parallel measure block parsing.
func TestDanmujiParallelBenchmark(t *testing.T) {
	input := `package main
benchmark "concurrent reads" {
	setup {
		cache := NewCache()
	}
	parallel measure {
		cache.Get("key")
	}
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "parallel_measure_block") {
		t.Error("expected parallel_measure_block node")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}

// TestDanmujiNeedsBlock tests needs block parsing.
func TestDanmujiNeedsBlock(t *testing.T) {
	input := `package main
func f() {
	needs postgres db {
		port = 5432
	}
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "needs_block") {
		t.Error("expected needs_block node")
	}
	if !strings.Contains(sexp, "service_type") {
		t.Error("expected service_type node")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}

// TestDanmujiLoadBlock tests load block with rate, duration, target parsing.
func TestDanmujiLoadBlock(t *testing.T) {
	input := `package main
load "checkout" {
	rate 50
	duration 2
	target post "http://localhost:8080/api"
	then "fast" {
		expect true
	}
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "load_block") {
		t.Error("expected load_block node")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}

// TestDanmujiProfileBlock tests profile block parsing.
func TestDanmujiProfileBlock(t *testing.T) {
	input := `package main
func f() {
	profile routines {}
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "profile_block") {
		t.Error("expected profile_block node")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}

// TestDanmujiNoLeaks tests no_leaks directive parsing.
func TestDanmujiNoLeaks(t *testing.T) {
	input := `package main
func f() {
    no_leaks
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "no_leaks_directive") {
		t.Error("expected no_leaks_directive node")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}

// TestDanmujiFakeClock tests fake_clock directive parsing.
func TestDanmujiFakeClock(t *testing.T) {
	input := `package main
func f() {
    fake_clock at "2026-03-17T00:00:00Z"
    fake_clock at "2026-03-17T09:00:00Z" in "America/New_York"
    fake_clock
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "fake_clock_directive") {
		t.Error("expected fake_clock_directive node")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}

// TestDanmujiExecBlock tests exec block with run command parsing.
func TestDanmujiExecBlock(t *testing.T) {
	input := `package main
func f() {
	exec "run migrations" {
		run "echo hello"
		expect exit_code == 0
		expect stdout contains "hello"
	}
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "exec_block") {
		t.Error("expected exec_block node")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}

// TestDanmujiSnapshotBlock tests snapshot block parsing.
func TestDanmujiSnapshotBlock(t *testing.T) {
	input := `package main
func f() {
	snapshot "valid_response" {
		resp := getResponse()
		resp.Body
	}
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "snapshot_block") {
		t.Error("expected snapshot_block node")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}

// TestDanmujiSpyWithBody tests spy declaration with a method body.
func TestDanmujiSpyWithBody(t *testing.T) {
	input := `package main
func f() {
	spy EventBus {
		Publish(topic string)
		Subscribe(topic string) -> error = nil
	}
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "spy_declaration") {
		t.Error("expected spy_declaration node")
	}
	if !strings.Contains(sexp, "mock_method") {
		t.Error("expected mock_method nodes inside spy body")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}

func TestDanmujiEachDo(t *testing.T) {
	// Test basic each...do with scenarios
	input := `package main
func f() {
    each "cases" {
        { name: "first", val: 1 }
        { name: "second", val: 2 }
    } do {
        _ = scenario.val
    }
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "each_do_block") {
		t.Error("expected each_do_block")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}

func TestDanmujiProperty(t *testing.T) {
	input := `package main
func f() {
	property "addition commutes" for all (a int, b int) up to 500 {
		expect a + b == b + a
	}
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "property_block") {
		t.Error("expected property_block")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}

func TestDanmujiEachDoWithDefaults(t *testing.T) {
	input := `package main
func f() {
    each "cases" {
        defaults { method: "GET", status: 200 }
        { name: "ok", status: 200 }
        { name: "post", method: "POST" }
    } do {
        _ = scenario.method
    }
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "defaults_block") {
		t.Error("expected defaults_block")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}

func TestDanmujiMatrix(t *testing.T) {
	input := `package main
func f() {
    matrix "combos" {
        method: { "GET", "POST" }
        auth: { "none", "bearer" }
    } do {
        _ = scenario.method
    }
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "matrix_block") {
		t.Error("expected matrix_block")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}

// TestDanmujiSpyWithoutBody tests that spy without body still parses.
func TestDanmujiSpyWithoutBody(t *testing.T) {
	input := `package main
func f() {
	spy Logger
}
`
	sexp := parseDanmuji(t, input)
	t.Logf("SExpr: %s", sexp)
	if !strings.Contains(sexp, "spy_declaration") {
		t.Error("expected spy_declaration node")
	}
	if strings.Contains(sexp, "ERROR") {
		t.Errorf("unexpected ERROR: %s", sexp)
	}
}

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
		{"http", `ready http "http://localhost:9090/health"`},
		{"tcp", `ready tcp "localhost:9090"`},
		{"stdout", `ready stdout "listening on"`},
		{"delay", `ready delay 5s`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := fmt.Sprintf("package main\nfunc f() {\n\t%s\n}\n", tc.input)
			sexp := parseDanmuji(t, input)
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
	signals := []string{"SIGTERM", "SIGINT", "SIGKILL", "SIGUSR1", "SIGHUP"}
	for _, sig := range signals {
		t.Run(sig, func(t *testing.T) {
			input := fmt.Sprintf("package main\nfunc f() {\n\tsignal %s\n}\n", sig)
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
