package danmuji

import (
	"os"
	"strings"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

const manualHighlightQueries = `
;; Manual danmuji highlight additions
(tag) @attribute
((identifier) @keyword (#any-of? @keyword "exit_code" "stderr"))
(signal_name) @constant
(each_row_block table: (identifier) @variable)
(scenario_field key: (identifier) @property)
(matrix_field key: (identifier) @property)
(process_block path: (_) @string)
(ready_clause target: (_) @string)
`

func expectedHighlightQuery() string {
	base := strings.TrimSpace(GenerateHighlightQueries(GoGrammar(), DanmujiGrammar()))
	manual := strings.TrimSpace(manualHighlightQueries)
	if manual == "" {
		return base
	}
	return strings.TrimSpace(base + "\n\n" + manual)
}

func TestHighlightQueryMatchesGenerated(t *testing.T) {
	generated := expectedHighlightQuery()
	fileBytes, err := os.ReadFile("highlights.scm")
	if err != nil {
		t.Fatalf("read highlights.scm: %v", err)
	}
	file := strings.TrimSpace(string(fileBytes))
	if generated != file {
		t.Fatalf("highlights.scm is out of sync with GenerateHighlightQueries\n\nexpected:\n%s\n\nactual:\n%s", generated, file)
	}
}

func TestHighlightQueryCompiles(t *testing.T) {
	lang, err := getDanmujiLanguage()
	if err != nil {
		t.Fatalf("getDanmujiLanguage: %v", err)
	}
	query, err := os.ReadFile("highlights.scm")
	if err != nil {
		t.Fatalf("read highlights.scm: %v", err)
	}
	if _, err = gotreesitter.NewQuery(string(query), lang); err != nil {
		t.Fatalf("NewQuery(highlights.scm): %v", err)
	}
}

func parseHighlightCaptures(t *testing.T, source string) map[string]map[string]bool {
	t.Helper()

	lang, err := getDanmujiLanguage()
	if err != nil {
		t.Fatalf("getDanmujiLanguage: %v", err)
	}

	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse([]byte(source))
	if err != nil {
		t.Fatalf("parse highlight sample: %v", err)
	}
	if tree.RootNode().HasError() {
		t.Fatalf("highlight sample did not parse cleanly")
	}

	query, err := gotreesitter.NewQuery(expectedHighlightQuery(), lang)
	if err != nil {
		t.Fatalf("compile highlight query: %v", err)
	}

	captures := map[string]map[string]bool{}
	cursor := query.Exec(tree.RootNode(), lang, []byte(source))
	for {
		capture, ok := cursor.NextCapture()
		if !ok {
			break
		}
		if captures[capture.Name] == nil {
			captures[capture.Name] = map[string]bool{}
		}
		captures[capture.Name][capture.Text([]byte(source))] = true
	}

	return captures
}

func TestHighlightQueryCoversDanmujiDSL(t *testing.T) {
	captures := parseHighlightCaptures(t, `package highlightmeta_test

import "testing"

mock Repo {
	Save(name string) -> error = nil
}

fake Store {
	Get(key string) -> string {
		return "cached"
	}
}

spy Bus {
	Publish(topic string)
}

@slow @parallel
unit "highlight coverage" {
	before each {
		repo := &mockRepo{}
		_ = repo
	}

	after each {
		_ = "cleanup"
	}

	before all {
		_ = "setup"
	}

	after all {
		_ = "teardown"
	}

	fake_clock at "2026-01-01T00:00:00Z"
	profile routines {}
	no_leaks

	table sums {
		| 1 | 2 | 3 |
	}

	each row in sums {
		then "table rows" {
			expect true
		}
	}

	each "cases" {
		defaults { token: "valid" }
		{ name: "ok", token: "ok" }
	} do {
		then "scenario fields" {
			expect scenario.token not_nil
		}
	}

	matrix "method x auth" {
		method: { "GET" }
		auth: { "none" }
	} do {
		then "matrix fields" {
			expect scenario.method not_nil
		}
	}

	exec "echo" {
		run "echo hi"
		expect exit_code == 0
		expect stdout contains "hi"
		expect stderr == ""
	}

	verify repo.Save called 0 times

	snapshot "snap" {
		"body"
	}

	property "commutative" for all (a int, b int) up to 10 {
		expect a + b == b + a
	}

	then "polling" {
		eventually "done" within 10ms {
			expect true
		}
		consistently "stable" for 10ms {
			expect true
		}
	}
}

integration "process coverage" {
	needs postgres db {}

	process "./cmd/server" {
		args "--port 1234"
		env { LOG_LEVEL: "debug" }
		ready http "http://127.0.0.1:1234/health"
	}

	stop {
		signal SIGTERM
		timeout 1s
		expect exit_code == 0
	}
}

@skip
unit "skip coverage" {
	then "skipped" {
		expect true
	}
}

load "load coverage" {
	rate 10
	duration 5
	target get "http://localhost:8080/health"

	then "load target parses" {
		expect true
	}
}

benchmark "bench" {
	measure {
		x := 1
		_ = x
	}
	report_allocs
}
`)

	assertCaptureTexts(t, captures, "keyword",
		"mock", "fake", "spy", "unit", "before", "after", "all", "fake_clock", "profile", "routines", "no_leaks",
		"table", "each", "row", "defaults", "matrix", "exec", "run", "expect", "contains",
		"verify", "called", "times", "snapshot", "property", "up", "then", "eventually", "within", "consistently",
		"integration", "needs", "postgres", "process", "args", "env", "ready", "http", "stop", "signal", "timeout",
		"load", "rate", "duration", "target", "get",
		"benchmark", "measure", "report_allocs", "exit_code", "stderr",
	)
	assertCaptureTexts(t, captures, "type.definition", "Repo", "Store", "Bus", "sums")
	assertCaptureTexts(t, captures, "function.method", "Save", "Get", "Publish")
	assertCaptureTexts(t, captures, "variable", "sums")
	assertCaptureTexts(t, captures, "property", "token", "method", "auth")
	assertCaptureTexts(t, captures, "constant", "SIGTERM")
	assertCaptureTexts(t, captures, "attribute", "@slow", "@parallel", "@skip")
	assertCaptureTexts(t, captures, "string", `"highlight coverage"`, `"process coverage"`, `"./cmd/server"`, `"snap"`, `"bench"`)
}

func TestHighlightQueryCoversRemainingDSLLeaves(t *testing.T) {
	captures := parseHighlightCaptures(t, `package highlightleafmeta_test

import "testing"

unit "verify leaves" {
	then "verify variants" {
		verify client.Save called with ("alice")
		verify client.Save not_called
		reject false
	}
}

integration "process leaves" {
	process "./cmd/server" {
		ready tcp "127.0.0.1:1234"
		ready stdout "ready"
	}

	stop {
		signal SIGINT
		timeout 2s
		expect exit_code == 0
	}
}

load "leaf load" {
	rate 25
	rampup 1
	concurrency 4
	target post "http://localhost:8080/api"

	then "load target parses" {
		expect true
	}
}

benchmark "leaf bench" {
	setup {
		_ = 1
	}

	parallel measure {
		_ = 2
	}
}
`)

	assertCaptureTexts(t, captures, "keyword",
		"verify", "called", "with", "not_called", "reject",
		"process", "ready", "tcp", "stdout", "stop", "signal", "timeout", "expect", "exit_code",
		"load", "rate", "rampup", "concurrency", "target", "post",
		"benchmark", "setup", "parallel", "measure",
	)
	assertCaptureTexts(t, captures, "constant", "SIGINT")
	assertCaptureTexts(t, captures, "string",
		`"verify leaves"`, `"process leaves"`, `"./cmd/server"`, `"127.0.0.1:1234"`, `"ready"`, `"leaf load"`, `"leaf bench"`,
	)
}

func assertCaptureTexts(t *testing.T, captures map[string]map[string]bool, captureName string, texts ...string) {
	t.Helper()
	got := captures[captureName]
	for _, text := range texts {
		if got == nil || !got[text] {
			t.Fatalf("missing capture %s for %q", captureName, text)
		}
	}
}
