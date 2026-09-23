package danmuji

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// FuzzTranspileNeverLosesAssertions is the item-3 mutation fuzzer: it
// mutates real-world .dmj specs (seeded from the goetrope corpus, copied
// read-only into testdata/fuzz_seeds) and checks that whenever a mutant
// still transpiles successfully, the generated Go code contains at least
// as many real assertions as the source has standalone expect/reject
// statements. A mutant that transpiles clean but emits FEWER assertions
// than it should is exactly the V1/V3 failure mode: danmuji silently
// dropped DSL content instead of failing the build.
//
// "Standalone" deliberately excludes expect/reject statements nested
// inside eventually/consistently/property blocks: those compile down to
// boolean conditions feeding a polling loop or quick.Check, not to a
// `danmuji:N:`-tagged assertion call, so they use a different (already
// covered elsewhere) verification path rather than silent disappearance.
//
// Run the full mutation budget with:
//
//	go test -run '^$' -fuzz FuzzTranspileNeverLosesAssertions -fuzztime 5m .
//
// CI runs a short smoke pass (see .github/workflows/ci.yml).
func FuzzTranspileNeverLosesAssertions(f *testing.F) {
	seeds, err := filepath.Glob(filepath.Join("testdata", "fuzz_seeds", "*.dmj"))
	if err != nil {
		f.Fatalf("glob fuzz seeds: %v", err)
	}
	if len(seeds) == 0 {
		f.Fatal("expected at least one seed in testdata/fuzz_seeds")
	}
	for _, s := range seeds {
		data, err := os.ReadFile(s)
		if err != nil {
			f.Fatalf("read seed %s: %v", s, err)
		}
		f.Add(data)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Keep the fuzzer fast and focused: absurdly large mutants are not
		// what this check is after, and would dominate wall-clock budget.
		if len(data) == 0 || len(data) > 32*1024 {
			return
		}

		wanted, ok := countStandaloneExpectRejectStatements(data)
		if !ok || wanted == 0 {
			// Either the mutant doesn't even parse (irrelevant to this
			// check — build-time rejection is fine) or it has nothing to
			// verify.
			return
		}

		goCode, err := TranspileDanmuji(data, TranspileOptions{})
		if err != nil {
			// Rejecting the build is always an acceptable outcome for this
			// check: an explicit error is the opposite of a silent drop.
			return
		}

		got := len(danmujiAssertionMarker.FindAllString(goCode, -1))
		if got < wanted {
			t.Fatalf("silent drop: source has %d standalone expect/reject statement(s) but only %d assertion(s) were emitted\n\nsource:\n%s\n\noutput:\n%s",
				wanted, got, data, goCode)
		}
	})
}

// danmujiAssertionMarker matches the `danmuji:<line>:` context tag that
// every real, top-level expect/reject assertion embeds in its failure
// message (see expectFailureContext in transpile_core.go). Counting these
// is a precise, false-positive-free proxy for "how many assertions did the
// emitted Go code actually contain" — far more reliable than grepping the
// output for "assert."/"require." (which also appear in helper
// definitions, imports, and unrelated boilerplate).
var danmujiAssertionMarker = regexp.MustCompile(`danmuji:\d+:`)

// countStandaloneExpectRejectStatements parses src with the same grammar
// TranspileDanmuji uses and counts expect_statement/reject_statement nodes
// that are NOT nested inside an eventually_block, consistently_block, or
// property_block. ok is false if src doesn't parse at all (HasError, or a
// parser error) — in that case the count is meaningless and the caller
// should skip the mutant entirely, since a non-parsing mutant says nothing
// about silent drops.
func countStandaloneExpectRejectStatements(src []byte) (count int, ok bool) {
	lang, err := getDanmujiLanguage()
	if err != nil {
		return 0, false
	}
	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse(src)
	if err != nil {
		return 0, false
	}
	root := tree.RootNode()
	if root.HasError() {
		return 0, false
	}

	var walk func(n *gotreesitter.Node, inPolling bool)
	walk = func(n *gotreesitter.Node, inPolling bool) {
		switch n.Type(lang) {
		case "eventually_block", "consistently_block", "property_block":
			inPolling = true
		case "expect_statement", "reject_statement":
			if !inPolling {
				count++
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), inPolling)
		}
	}
	walk(root, false)
	return count, true
}
