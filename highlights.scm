;; Auto-generated highlight queries for grammar extension
;; Extension: danmuji (extends go)

;; Keywords
"after" @keyword
"all" @keyword
"allocs" @keyword
"args" @keyword
"at" @keyword
"before" @keyword
"benchmark" @keyword
"blockprofile" @keyword
"called" @keyword
"concurrency" @keyword
"consistently" @keyword
"container" @keyword
"contains" @keyword
"cpu" @keyword
"defaults" @keyword
"delay" @keyword
"delete" @keyword
"do" @keyword
"duration" @keyword
"e2e" @keyword
"each" @keyword
"env" @keyword
"eventually" @keyword
"exec" @keyword
"expect" @keyword
"fake" @keyword
"fake_clock" @keyword
"get" @keyword
"given" @keyword
"http" @keyword
"in" @keyword
"integration" @keyword
"is_nil" @keyword
"kafka" @keyword
"load" @keyword
"matrix" @keyword
"measure" @keyword
"mem" @keyword
"mock" @keyword
"mongo" @keyword
"mutexprofile" @keyword
"mysql" @keyword
"nats" @keyword
"needs" @keyword
"no_leaks" @keyword
"not_called" @keyword
"not_nil" @keyword
"parallel" @keyword
"patch" @keyword
"post" @keyword
"postgres" @keyword
"process" @keyword
"profile" @keyword
"property" @keyword
"put" @keyword
"rabbitmq" @keyword
"rampup" @keyword
"rate" @keyword
"ready" @keyword
"redis" @keyword
"reject" @keyword
"report_allocs" @keyword
"routines" @keyword
"row" @keyword
"run" @keyword
"save" @keyword
"setup" @keyword
"show" @keyword
"signal" @keyword
"snapshot" @keyword
"spy" @keyword
"stdout" @keyword
"stop" @keyword
"table" @keyword
"target" @keyword
"tcp" @keyword
"then" @keyword
"timeout" @keyword
"times" @keyword
"to" @keyword
"top" @keyword
"unit" @keyword
"up" @keyword
"verify" @keyword
"when" @keyword
"with" @keyword
"within" @keyword

;; Operators
"->" @operator

;; test_block
(test_block name: (_) @string)

;; given_block
(given_block description: (_) @string)

;; when_block
(when_block description: (_) @string)

;; then_block
(then_block description: (_) @string)

;; expect_statement
(expect_statement actual: (identifier) @variable)

;; reject_statement
(reject_statement actual: (identifier) @variable)

;; mock_method
(mock_method name: (identifier) @function.method)

;; mock_declaration
(mock_declaration name: (identifier) @type.definition)

;; fake_method
(fake_method name: (identifier) @function.method)

;; fake_declaration
(fake_declaration name: (identifier) @type.definition)

;; spy_declaration
(spy_declaration name: (identifier) @type.definition)

;; table_declaration
(table_declaration name: (identifier) @type.definition)

;; each_row_block

;; needs_block
(needs_block name: (_) @string)

;; setup_block

;; measure_block

;; parallel_measure_block

;; benchmark_block
(benchmark_block name: (_) @string)

;; http_method

;; target_block

;; load_block
(load_block name: (_) @string)

;; exec_block
(exec_block name: (_) @string)

;; profile_block

;; snapshot_block
(snapshot_block name: (_) @string)

;; defaults_block

;; matrix_block
(matrix_block name: (_) @string)

;; property_block
(property_block name: (_) @string)

;; eventually_block
(eventually_block name: (_) @string)

;; consistently_block
(consistently_block name: (_) @string)

;; process_block

;; stop_block

;; Manual danmuji highlight additions
(tag) @attribute
((identifier) @keyword (#any-of? @keyword "exit_code" "stderr"))
(signal_name) @constant
(each_row_block table: (identifier) @variable)
(scenario_field key: (identifier) @property)
(matrix_field key: (identifier) @property)
(process_block path: (_) @string)
(ready_clause target: (_) @string)
