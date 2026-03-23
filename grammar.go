package danmuji

// DanmujiGrammar returns a BDD testing DSL grammar that extends Go.
// It adds test blocks, BDD structure (given/when/then), assertions,
// test doubles (mock/fake/spy), lifecycle hooks, data tables, and tags.
func DanmujiGrammar() *Grammar {
	return ExtendGrammar("danmuji", GoGrammar(), func(g *Grammar) {
		// ---------------------------------------------------------------
		// Test categories
		// ---------------------------------------------------------------
		g.Define("test_category",
			Choice(
				Str("unit"),
				Str("integration"),
				Str("e2e"),
			))

		baseIdentifier := g.Rules["identifier"]
		softKeywordIdentifier := func(keyword string) *Rule {
			return Alias(Str(keyword), "identifier", true)
		}

		// Danmuji keywords should remain usable as ordinary Go identifiers when
		// they appear in Go syntax like `exec := ...` or `profile := ...`.
		g.Define("identifier",
			Choice(
				baseIdentifier,
				softKeywordIdentifier("args"),
				softKeywordIdentifier("exec"),
				softKeywordIdentifier("profile"),
			))

		// ---------------------------------------------------------------
		// Tags: @slow, @smoke, @skip, @focus, @flaky, @parallel,
		//       or any @identifier
		// ---------------------------------------------------------------
		g.Define("tag",
			Seq(
				Str("@"),
				ImmToken(Pat(`[a-zA-Z_][a-zA-Z0-9_]*`)),
			))

		g.Define("tag_list",
			Repeat1(Sym("tag")))

		// ---------------------------------------------------------------
		// Test block: [tag_list] test_category "name" { body }
		// ---------------------------------------------------------------
		g.Define("test_block",
			Seq(
				Optional(Field("tags", Sym("tag_list"))),
				Field("category", Sym("test_category")),
				Field("name", Sym("_string_literal")),
				Sym("block"),
			))

		// ---------------------------------------------------------------
		// BDD structure: given/when/then blocks
		// ---------------------------------------------------------------
		g.Define("given_block",
			Seq(
				Str("given"),
				Field("description", Sym("_string_literal")),
				Sym("block"),
			))

		g.Define("when_block",
			Seq(
				Str("when"),
				Field("description", Sym("_string_literal")),
				Sym("block"),
			))

		g.Define("then_block",
			Seq(
				Str("then"),
				Field("description", Sym("_string_literal")),
				Sym("block"),
			))

		// ---------------------------------------------------------------
		// Assertions
		// ---------------------------------------------------------------
		g.Define("match_value",
			Choice(
				Seq(Str("contains"), Field("expected", Sym("_expression"))),
				Str("is_nil"),
				Str("not_nil"),
				Sym("_expression"),
			))

		g.Define("match_field",
			Seq(
				Field("key", Sym("identifier")),
				Str(":"),
				Field("value", Sym("match_value")),
			))

		g.Define("match_block",
			Seq(
				Str("{"),
				Choice(
					CommaSep1(Sym("match_field")),
					Repeat1(Sym("match_field")),
				),
				Str("}"),
			))

		g.Define("expect_statement",
			Seq(
				Str("expect"),
				Field("actual", Sym("_expression")),
				Optional(
					Choice(
						Seq(Str("=="), Field("expected", Sym("_expression"))),
						Seq(Str("!="), Field("expected", Sym("_expression"))),
						Seq(Str("contains"), Field("expected", Sym("_expression"))),
						Seq(Str("matches"), Field("match", Sym("match_block"))),
						Seq(Str("unordered_equal"), Field("expected", Sym("_expression"))),
						Seq(Str("is"), Field("expected", Sym("_expression"))),
						Seq(Str("message"), Str("contains"), Field("expected", Sym("_expression"))),
						Str("is_nil"),
						Str("not_nil"),
						// Domain matcher style: expect user to_have_role "admin"
						PrecDynamic(-1, Seq(
							Field("matcher", Sym("identifier")),
							Optional(Field("expected", Sym("_expression"))),
						)),
					),
				),
			))

		g.Define("reject_statement",
			Seq(
				Str("reject"),
				Field("actual", Sym("_expression")),
			))

		// Optional shorthand durations for polling and time DSL (e.g., 5s, 2.5m, 30ms).
		g.Define("duration_literal",
			ImmToken(Pat(`[0-9]+(?:\.[0-9]+)?(?:ns|us|µs|ms|s|m|h)`)),
		)

		// ---------------------------------------------------------------
		// Test doubles: mock, fake, spy
		// ---------------------------------------------------------------

		// mock_method: identifier parameter_list ["->" type ["=" default_value]]
		g.Define("mock_method",
			Seq(
				Field("name", Sym("identifier")),
				Field("parameters", Sym("parameter_list")),
				Optional(Seq(
					Str("->"),
					Field("return_type", Sym("_simple_type")),
					Optional(Seq(
						Str("="),
						Field("default_value", Sym("_expression")),
					)),
				)),
			))

		// mock_declaration: "mock" identifier block
		// (mock_methods appear as statements inside the block)
		g.Define("mock_declaration",
			Seq(
				Str("mock"),
				Field("name", Sym("identifier")),
				Field("body", Sym("block")),
			))

		// fake_method: identifier parameter_list ["->" type] block
		g.Define("fake_method",
			Seq(
				Field("name", Sym("identifier")),
				Field("parameters", Sym("parameter_list")),
				Optional(Seq(
					Str("->"),
					Field("return_type", Sym("_simple_type")),
				)),
				Field("body", Sym("block")),
			))

		// fake_declaration: "fake" identifier block
		// (fake_methods appear as statements inside the block)
		g.Define("fake_declaration",
			Seq(
				Str("fake"),
				Field("name", Sym("identifier")),
				Field("body", Sym("block")),
			))

		// spy_declaration: "spy" identifier [block]
		// With a body, methods use mock_method syntax inside the block.
		// The spy wraps a real implementation, delegates all calls, and records them.
		g.Define("spy_declaration",
			Seq(
				Str("spy"),
				Field("name", Sym("identifier")),
				Optional(Field("body", Sym("block"))),
			))

		// ---------------------------------------------------------------
		// Verification
		// ---------------------------------------------------------------
		g.Define("verify_assertion",
			Choice(
				Seq(Str("called"), Sym("int_literal"), Str("times")),
				Seq(Str("called"), Str("with"), Str("("), CommaSep(Sym("_expression")), Str(")")),
				Str("not_called"),
			))

		g.Define("verify_statement",
			Seq(
				Str("verify"),
				Field("target", Sym("_expression")),
				Field("assertion", Sym("verify_assertion")),
			))

		// ---------------------------------------------------------------
		// Lifecycle hooks
		// ---------------------------------------------------------------
		g.Define("lifecycle_hook",
			Seq(
				Choice(Str("before"), Str("after")),
				Choice(Str("each"), Str("all")),
				Sym("block"),
			))

		// ---------------------------------------------------------------
		// Data tables
		// ---------------------------------------------------------------
		g.Define("table_row",
			Seq(
				Str("|"),
				Repeat1(Seq(Sym("_expression"), Str("|"))),
			))

		g.Define("table_declaration",
			Seq(
				Str("table"),
				Field("name", Sym("identifier")),
				Str("{"),
				Repeat(Sym("table_row")),
				Str("}"),
			))

		g.Define("each_row_block",
			Seq(
				Str("each"),
				Str("row"),
				Str("in"),
				Field("table", Sym("identifier")),
				Sym("block"),
			))

		// ---------------------------------------------------------------
		// Needs blocks: service dependency declarations
		// ---------------------------------------------------------------
		g.Define("service_type",
			Choice(
				Str("postgres"),
				Str("redis"),
				Str("mysql"),
				Str("kafka"),
				Str("mongo"),
				Str("rabbitmq"),
				Str("nats"),
				Str("tempdir"),
				Str("http"),
				Str("container"),
			))

		g.Define("handler_directive",
			Seq(
				Str("handler"),
				Field("value", Sym("_expression")),
			))

		g.Define("needs_block",
			Seq(
				Str("needs"),
				Field("service", Sym("service_type")),
				Field("name", Sym("identifier")),
				Optional(Field("body", Sym("block"))),
			))

		// ---------------------------------------------------------------
		// Benchmarks
		// ---------------------------------------------------------------
		g.Define("setup_block", Seq(Str("setup"), Sym("block")))

		g.Define("measure_block", Seq(Str("measure"), Sym("block")))

		g.Define("parallel_measure_block", Seq(
			Str("parallel"), Str("measure"), Sym("block"),
		))

		g.Define("report_directive", Str("report_allocs"))

		g.Define("benchmark_block", Seq(
			Optional(Field("tags", Sym("tag_list"))),
			Str("benchmark"),
			Field("name", Sym("_string_literal")),
			Field("body", Sym("block")),
		))

		// ---------------------------------------------------------------
		// Load testing (vegeta)
		// ---------------------------------------------------------------
		g.Define("load_config", Choice(
			Seq(Str("rate"), Sym("_expression")),
			Seq(Str("duration"), Choice(
				Sym("_expression"),
				Sym("duration_literal"),
			)),
			Seq(Str("rampup"), Choice(
				Sym("_expression"),
				Sym("duration_literal"),
			)),
			Seq(Str("concurrency"), Sym("_expression")),
		))

		g.Define("http_method", Choice(
			Str("get"), Str("post"), Str("put"), Str("delete"), Str("patch"),
		))

		g.Define("target_block", Seq(
			Str("target"),
			Field("method", Sym("http_method")),
			Field("url", Sym("_string_literal")),
		))

		g.Define("load_block", Seq(
			Optional(Field("tags", Sym("tag_list"))),
			Str("load"),
			Field("name", Sym("_string_literal")),
			Field("body", Sym("block")),
		))

		// ---------------------------------------------------------------
		// Exec blocks (shell commands)
		// ---------------------------------------------------------------
		g.Define("run_command", Seq(
			Str("run"),
			Field("command", Sym("_string_literal")),
		))

		g.Define("exec_block", Seq(
			Str("exec"),
			Field("name", Sym("_string_literal")),
			Field("body", Sym("block")),
		))

		// ---------------------------------------------------------------
		// Profiling blocks
		// ---------------------------------------------------------------
		g.Define("profile_type", Choice(
			Str("cpu"), Str("mem"), Str("allocs"),
			Str("routines"), Str("blockprofile"), Str("mutexprofile"),
		))

		g.Define("profile_directive", Choice(
			Seq(Str("show"), Str("top"), Sym("int_literal")),
			Seq(Str("save"), Sym("_string_literal")),
		))

		g.Define("profile_block", Seq(
			Str("profile"),
			Field("type", Sym("profile_type")),
			Optional(Field("directive", Sym("profile_directive"))),
			Optional(Sym("block")),
		))

		// ---------------------------------------------------------------
		// no_leaks directive: goroutine leak detection
		// ---------------------------------------------------------------
		g.Define("no_leaks_directive", Str("no_leaks"))

		// ---------------------------------------------------------------
		// fake_clock directive: time abstraction for tests
		// Three forms:
		//   fake_clock at "time" in "tz"
		//   fake_clock at "time"
		//   fake_clock
		// ---------------------------------------------------------------
		g.Define("fake_clock_directive", Choice(
			PrecDynamic(10, Seq(
				Str("fake_clock"),
				Str("at"),
				Field("start_time", Sym("_string_literal")),
				Str("in"),
				Field("timezone", Sym("_string_literal")),
			)),
			PrecDynamic(10, Seq(
				Str("fake_clock"),
				Str("at"),
				Field("start_time", Sym("_string_literal")),
			)),
			PrecDynamic(10, Str("fake_clock")),
		))

		// ---------------------------------------------------------------
		// Snapshot testing: capture output and compare against golden file
		// ---------------------------------------------------------------
		g.Define("snapshot_block",
			Seq(
				Str("snapshot"),
				Field("name", Sym("_string_literal")),
				Sym("block"),
			))

		// ---------------------------------------------------------------
		// Scenario-driven table tests (each ... do)
		// ---------------------------------------------------------------

		// scenario_field: identifier ":" _expression (key-value in a scenario object)
		g.Define("scenario_field", Seq(
			Field("key", Sym("identifier")),
			Str(":"),
			Field("value", Sym("_expression")),
		))

		// scenario_entry: "{" field: val, ... "}"
		g.Define("scenario_entry", PrecDynamic(20, Seq(
			Str("{"),
			CommaSep1(Sym("scenario_field")),
			Str("}"),
		)))

		// defaults_block: "defaults" "{" field: val, ... "}"
		g.Define("defaults_block", Seq(
			Str("defaults"),
			Str("{"),
			CommaSep1(Sym("scenario_field")),
			Str("}"),
		))

		// ---------------------------------------------------------------
		// Factories and build expressions
		// ---------------------------------------------------------------
		g.Define("factory_overrides_block", Seq(
			Str("{"),
			CommaSep1(Sym("scenario_field")),
			Str("}"),
		))

		g.Define("factory_trait_block", Seq(
			Str("trait"),
			Field("name", Sym("identifier")),
			Field("body", Sym("factory_overrides_block")),
		))

		g.Define("factory_declaration", Seq(
			Str("factory"),
			Field("name", Sym("identifier")),
			Field("body", Sym("block")),
		))

		g.Define("trait_list", Seq(
			Field("trait", Sym("identifier")),
			Repeat(Seq(
				Str(","),
				Field("trait", Sym("identifier")),
			)),
		))

		g.Define("build_expression", PrecDynamic(20, Seq(
			Str("build"),
			Field("name", Sym("identifier")),
			Optional(Seq(
				Str("with"),
				Field("traits", Sym("trait_list")),
			)),
			Optional(Field("overrides", Sym("factory_overrides_block"))),
		)))

		// each_do_block: "each" string block "do" block
		// The first block contains defaults_block and scenario_entry statements.
		// Using Sym("block") for the scenario list because Go's grammar requires
		// newline/semicolons between statements, and block handles that naturally.
		g.Define("each_do_block", Seq(
			Str("each"),
			Field("name", Sym("_string_literal")),
			Field("scenarios", Sym("block")),
			Str("do"),
			Field("body", Sym("block")),
		))

		// matrix_field: identifier ":" "{" comma_separated_expressions "}"
		g.Define("matrix_field", Seq(
			Field("key", Sym("identifier")),
			Str(":"),
			Str("{"),
			CommaSep1(Sym("_expression")),
			Str("}"),
		))

		// matrix_block: "matrix" string block "do" block
		// The first block contains matrix_field statements.
		g.Define("matrix_block", Seq(
			Str("matrix"),
			Field("name", Sym("_string_literal")),
			Field("dimensions", Sym("block")),
			Str("do"),
			Field("body", Sym("block")),
		))

		// property_block: "property" string "for" "all" parameter_list
		// ["up" "to" max_count] block
		//
		//   property "commutative sum" for all (a int, b int) up to 200 {
		//     expect a + b == b + a
		//   }
		g.Define("property_block", Seq(
			Str("property"),
			Field("name", Sym("_string_literal")),
			Str("for"),
			Str("all"),
			Field("params", Sym("parameter_list")),
			Optional(Seq(
				Str("up"),
				Str("to"),
				Field("max_count", Sym("_expression")),
			)),
			Field("body", Sym("block")),
		))

		// fuzz_block: "fuzz" string "with" parameter_list block
		//
		//   fuzz "parser handles arbitrary text" with (input string) {
		//     expect len(input) >= 0
		//   }
		g.Define("fuzz_block", Seq(
			Str("fuzz"),
			Field("name", Sym("_string_literal")),
			Str("with"),
			Field("params", Sym("parameter_list")),
			Field("body", Sym("block")),
		))

		// ---------------------------------------------------------------
		// Polling semantics
		//
		// eventually "description" within <duration> { ... }
		// consistently "description" for <duration> { ... }
		// ---------------------------------------------------------------
		g.Define("eventually_block", Seq(
			Str("eventually"),
			Field("name", Sym("_string_literal")),
			Optional(Seq(
				Str("within"),
				Field("duration", Choice(
					Sym("_expression"),
					Sym("duration_literal"),
				)),
			)),
			Field("body", Sym("block")),
		))

		g.Define("consistently_block", Seq(
			Str("consistently"),
			Field("name", Sym("_string_literal")),
			Optional(Seq(
				Str("for"),
				Field("duration", Choice(
					Sym("_expression"),
					Sym("duration_literal"),
				)),
			)),
			Field("body", Sym("block")),
		))

		g.Define("await_statement", Seq(
			Str("await"),
			Field("target", Sym("_expression")),
			Str("within"),
			Field("duration", Choice(
				Sym("duration_literal"),
				Sym("_expression"),
			)),
			Str("as"),
			Field("name", Sym("identifier")),
		))

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

		// ---------------------------------------------------------------
		// Wire into Go: extend _top_level_declaration and _statement
		// ---------------------------------------------------------------
		dslStatement := func(name string) *Rule {
			// Prefer valid Go statements when danmuji keywords are reused as
			// ordinary identifiers, e.g. `exec := ...` or `profile := ...`.
			return PrecDynamic(-1, Sym(name))
		}

		AppendChoice(g, "_top_level_declaration", Choice(
			Sym("factory_declaration"),
			Sym("test_block"),
			Sym("benchmark_block"),
			Sym("load_block"),
		))

		AppendChoice(g, "_expression", Choice(
			Sym("build_expression"),
		))

		AppendChoice(g, "_statement", Choice(
			dslStatement("factory_declaration"),
			dslStatement("given_block"),
			dslStatement("when_block"),
			dslStatement("then_block"),
			dslStatement("expect_statement"),
			dslStatement("reject_statement"),
			dslStatement("mock_declaration"),
			dslStatement("fake_declaration"),
			dslStatement("spy_declaration"),
			dslStatement("verify_statement"),
			dslStatement("lifecycle_hook"),
			dslStatement("table_declaration"),
			dslStatement("each_row_block"),
			Sym("mock_method"),
			Sym("fake_method"),
			dslStatement("needs_block"),
			dslStatement("handler_directive"),
			dslStatement("setup_block"),
			dslStatement("measure_block"),
			dslStatement("parallel_measure_block"),
			dslStatement("report_directive"),
			dslStatement("exec_block"),
			dslStatement("run_command"),
			dslStatement("load_config"),
			dslStatement("target_block"),
			dslStatement("profile_block"),
			dslStatement("no_leaks_directive"),
			dslStatement("fake_clock_directive"),
			dslStatement("snapshot_block"),
			dslStatement("each_do_block"),
			dslStatement("matrix_block"),
			dslStatement("factory_trait_block"),
			dslStatement("property_block"),
			dslStatement("fuzz_block"),
			dslStatement("eventually_block"),
			dslStatement("consistently_block"),
			dslStatement("await_statement"),
			dslStatement("defaults_block"),
			Sym("scenario_entry"),
			Sym("scenario_field"),
			Sym("matrix_field"),
			dslStatement("process_block"),
			dslStatement("process_args"),
			dslStatement("process_env"),
			dslStatement("ready_clause"),
			dslStatement("stop_block"),
			dslStatement("signal_directive"),
			dslStatement("timeout_directive"),
		))

		// ---------------------------------------------------------------
		// GLR conflicts for keyword ambiguities
		// (given/when/then/expect/reject/verify/mock/fake/spy can look
		// like identifiers or call expressions until disambiguation)
		// ---------------------------------------------------------------
		AddConflict(g, "_statement", "given_block")
		AddConflict(g, "_statement", "when_block")
		AddConflict(g, "_statement", "then_block")
		AddConflict(g, "_statement", "expect_statement")
		AddConflict(g, "_statement", "reject_statement")
		AddConflict(g, "_statement", "verify_statement")
		AddConflict(g, "_statement", "factory_declaration")
		AddConflict(g, "_statement", "mock_declaration")
		AddConflict(g, "_statement", "fake_declaration")
		AddConflict(g, "_statement", "spy_declaration")
		AddConflict(g, "_statement", "lifecycle_hook")
		AddConflict(g, "_statement", "mock_method")
		AddConflict(g, "_statement", "fake_method")
		AddConflict(g, "_statement", "needs_block")
		AddConflict(g, "_statement", "handler_directive")
		AddConflict(g, "_statement", "setup_block")
		AddConflict(g, "_statement", "measure_block")
		AddConflict(g, "_statement", "report_directive")
		AddConflict(g, "_statement", "parallel_measure_block")
		AddConflict(g, "_statement", "exec_block")
		AddConflict(g, "_statement", "run_command")
		AddConflict(g, "_statement", "load_config")
		AddConflict(g, "_statement", "target_block")
		AddConflict(g, "_statement", "profile_block")
		AddConflict(g, "_statement", "no_leaks_directive")
		AddConflict(g, "_statement", "fake_clock_directive")
		AddConflict(g, "_statement", "snapshot_block")
		AddConflict(g, "_statement", "each_do_block")
		AddConflict(g, "_statement", "matrix_block")
		AddConflict(g, "_statement", "property_block")
		AddConflict(g, "_statement", "fuzz_block")
		AddConflict(g, "_statement", "defaults_block")
		AddConflict(g, "_statement", "factory_trait_block")
		AddConflict(g, "_statement", "scenario_entry")
		AddConflict(g, "_statement", "scenario_field")
		AddConflict(g, "_statement", "matrix_field")
		AddConflict(g, "_statement", "eventually_block")
		AddConflict(g, "_statement", "consistently_block")
		AddConflict(g, "_statement", "await_statement")
		AddConflict(g, "_statement", "process_block")
		AddConflict(g, "_statement", "process_args")
		AddConflict(g, "_statement", "process_env")
		AddConflict(g, "_statement", "ready_clause")
		AddConflict(g, "_statement", "stop_block")
		AddConflict(g, "_statement", "signal_directive")
		AddConflict(g, "_statement", "timeout_directive")
		AddConflict(g, "_expression", "build_expression")
		AddConflict(g, "scenario_field", "keyed_element")
		AddConflict(g, "match_field", "keyed_element")

		g.EnableLRSplitting = true
	})
}
