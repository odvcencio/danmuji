package danmuji

import (
	"os"
	"strings"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

const manualHighlightQueries = `
;; Manual danmuji highlight additions
((identifier) @keyword (#any-of? @keyword "exit_code" "stderr"))
(signal_name) @constant
(each_row_block table: (identifier) @variable)
(scenario_field key: (identifier) @property)
(matrix_field key: (identifier) @property)
(process_block path: (_) @string)
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

func TestHighlightQueryCoversDanmujiDSL(t *testing.T) {
	lang, err := getDanmujiLanguage()
	if err != nil {
		t.Fatalf("getDanmujiLanguage: %v", err)
	}

	source := []byte(`package highlightmeta_test

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

unit "highlight coverage" {
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

benchmark "bench" {
	measure {
		x := 1
		_ = x
	}
	report_allocs
}
`)

	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse(source)
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
	cursor := query.Exec(tree.RootNode(), lang, source)
	for {
		capture, ok := cursor.NextCapture()
		if !ok {
			break
		}
		if captures[capture.Name] == nil {
			captures[capture.Name] = map[string]bool{}
		}
		captures[capture.Name][capture.Text(source)] = true
	}

	assertCaptureTexts(t, captures, "keyword",
		"mock", "fake", "spy", "unit", "fake_clock", "profile", "routines", "no_leaks",
		"table", "each", "row", "defaults", "matrix", "exec", "run", "expect", "contains",
		"snapshot", "property", "up", "then", "eventually", "within", "consistently",
		"integration", "process", "args", "env", "ready", "http", "stop", "signal", "timeout",
		"benchmark", "measure", "report_allocs", "exit_code", "stderr",
	)
	assertCaptureTexts(t, captures, "type.definition", "Repo", "Store", "Bus", "sums")
	assertCaptureTexts(t, captures, "function.method", "Save", "Get", "Publish")
	assertCaptureTexts(t, captures, "variable", "sums")
	assertCaptureTexts(t, captures, "property", "token", "method", "auth")
	assertCaptureTexts(t, captures, "constant", "SIGTERM")
	assertCaptureTexts(t, captures, "string", `"highlight coverage"`, `"process coverage"`, `"./cmd/server"`, `"snap"`, `"bench"`)
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
