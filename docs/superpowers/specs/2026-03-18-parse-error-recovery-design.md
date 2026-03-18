# Design: Exhaustive Parse Error Recovery

## Summary

Replace the raw s-expression error dump with human-readable error messages that show file, line, column, source context, and a hint about what was expected. Two-layer system: grammar-derived expectations (automatic, exhaustive) overlaid with hand-written messages for common mistakes.

## Current behavior

When a `.dmj` file has a syntax error, `TranspileDanmuji` returns:

```
parse errors:
(source_file (package_clause ...) (ERROR (identifier) ...) ...)
```

This is the raw Tree-sitter s-expression. Useless to anyone not debugging the grammar.

## Proposed behavior

```
user_test.dmj:12:5: expected string after "given"
   12 |     given valid input {
      |           ^^^^^^^^^^^
   hint: given "description" { ... }
```

Four parts:
1. **Location:** `file:line:col` (1-indexed, converted from Tree-sitter's 0-indexed `Point.Row`/`Point.Column`)
2. **Message:** what went wrong
3. **Source line** with underline at the error span
4. **Hint:** a correct example of the construct

When `SourceFile` is empty (e.g., inline tests), the location omits the filename and shows `line:col` only.

## Architecture

### Layer 1 — Grammar-derived expectations (automatic)

Walk the grammar definition (`DanmujiGrammar()`) to build a description of what each production expects. When an `ERROR` or `MISSING` node is found, look up its parent, determine what was expected at the error position, and generate a message.

The grammar's `Rule` type is a tree with `Kind`, `Value`, and `Children` (see gotreesitter `grammargen/grammar.go`). Productions use combinations of `RuleSeq`, `RuleChoice`, `RuleString`, `RuleSymbol`, `RuleField`, `RuleRepeat`, `RuleToken`, `RulePrec*`, and `RuleBlank`.

Many productions are not flat sequences. `expect_statement` has nested `Optional(Choice(Seq(...), ...))`. `fake_clock_directive` is a top-level `Choice` of three `PrecDynamic`-wrapped alternatives. The expectation model must handle this.

**Approach: enumerate linear expansions.** For each production, recursively flatten the rule tree into all possible linear sequences of expected children. For `Choice`, each alternative is a separate expansion. For `Optional`, generate expansions both with and without. For `Repeat`, include 0 and 1 occurrences.

```go
type LinearExpansion struct {
    Steps []ExpectedStep // ordered sequence of expected tokens/symbols
}

type ExpectedStep struct {
    Type     string // "string_literal", "block", "identifier", etc.
    Keyword  string // if Str("given"), the literal text
    Field    string // if wrapped in Field("name", ...), the field name
    Optional bool   // if this step can be skipped
}

type ProductionExpectations struct {
    NodeType   string
    Expansions []LinearExpansion // all valid sequences
}
```

At error time: given the parent node type and the successfully-parsed children before the error, match against the expansions to determine what should have come next. This handles `Choice` correctly — if the parser got `"expect" expression "=="`, it matched the prefix of the `==` alternative and the next expected step is another `_expression`.

To match, iterate the parent node's non-error children left to right, advancing through each expansion in parallel. Discard expansions that don't match the observed prefix. The remaining expansions tell us what was expected at the error position. If multiple expansions remain, describe the alternatives (e.g., "expected `==`, `!=`, `contains`, or matcher").

The expectation data is built once from `DanmujiGrammar()` and cached alongside the language (see "Caching" below).

### Layer 2 — Human-written overlays (curated)

A static map keyed by `(parent_type, prefix_signature)` where the prefix signature is a compact representation of what was successfully parsed before the error. These replace the mechanical message with a friendlier one for the ~30 most common mistakes.

```go
type ErrorOverlay struct {
    Message string
    Example string
}

var errorOverlays = map[string]ErrorOverlay{
    // key: "parent_type|prefix" where prefix is the matched children
    "given_block|given": {
        Message: `expected string after "given"`,
        Example: `given "description" { ... }`,
    },
    "given_block|given,string": {
        Message: `expected { to open block`,
        Example: `given "description" { ... }`,
    },
    "expect_statement|expect": {
        Message: `expected expression after "expect"`,
        Example: `expect x == 1`,
    },
    "expect_statement|expect,expr,==": {
        Message: `expected value after ==`,
        Example: `expect x == 1`,
    },
    "mock_method|ident,params": {
        Message: `expected -> return_type after parameters`,
        Example: `Save(u User) -> error = nil`,
    },
    // ... ~30 more
}
```

Falls back to Layer 1 when no overlay matches.

### Layer 3 — Keyword-based inference (fallback)

When Tree-sitter's error recovery does NOT create the expected parent node (e.g., the user writes `given valid input {` and the parser wraps everything in an ERROR node under `source_file` instead of creating a `given_block`), Layer 1 and 2 have no parent to work with.

Fallback: scan the ERROR node's text for known danmuji keywords. If the text starts with `given`, `when`, `then`, `expect`, `mock`, `process`, etc., infer the intended construct and use that construct's overlay/expansion. This handles the common case where the parser couldn't even begin to build the expected node.

```go
var keywordToProduction = map[string]string{
    "given": "given_block",
    "when": "when_block",
    "then": "then_block",
    "expect": "expect_statement",
    "reject": "reject_statement",
    "mock": "mock_declaration",
    "fake": "fake_declaration",
    "spy": "spy_declaration",
    "process": "process_block",
    "stop": "stop_block",
    // ... all keyword-initiated productions
}
```

### Go syntax errors

For errors in Go-inherited productions (function declarations, type definitions, imports, etc.), Layer 1 will produce mechanical messages from the Go grammar's rules. Rather than writing overlays for 150+ Go productions, we emit a generic fallback:

```
user_test.dmj:5:10: syntax error in Go code
    5 | func broken( {
      |              ^
```

This is still a massive improvement over the raw s-expression, and Go syntax errors in `.dmj` files are rare since most of the file is danmuji constructs.

## Error discovery

Walk the tree depth-first using gotreesitter's `Walk()` function. Collect all `ERROR` and `MISSING` nodes.

**Single top-level block:** Report only the first error by source position. Cascading errors are noise.

**Multiple top-level blocks:** Report one error per direct child of `source_file` that is a `test_block`, `benchmark_block`, or `load_block` (the three `_top_level_declaration` extensions). This way a file with 3 test blocks where 2 have errors shows 2 useful messages. Errors in Go-level declarations (functions, types) outside test blocks report one error for the whole file section.

**Determining child position within parent:** `Node.childIndex` is unexported in gotreesitter. To find an error node's position, iterate `parent.ChildCount()` and compare `parent.Child(i)` with the error node by start byte. This is O(n) in the number of siblings but n is always small (grammar productions rarely have more than 10 children).

**Edge cases:**
- Empty file or missing `package` clause: report `"expected package clause at start of file"` (special-cased, not grammar-derived).
- File with only a package clause and no test blocks: not an error per se, just nothing to transpile.

## Caching

The expectation data and the grammar are expensive to compute. Cache alongside the language:

```go
var (
    danmujiLangOnce     sync.Once
    danmujiLangCached   *gotreesitter.Language
    danmujiLangErr      error
    danmujiExpectations map[string]*ProductionExpectations // cached
)

func getDanmujiLanguage() (*gotreesitter.Language, error) {
    danmujiLangOnce.Do(func() {
        g := DanmujiGrammar()
        danmujiLangCached, danmujiLangErr = GenerateLanguage(g)
        if danmujiLangErr == nil {
            danmujiExpectations = buildExpectationMap(g)
        }
    })
    return danmujiLangCached, danmujiLangErr
}
```

`FormatParseError` receives the pre-built expectation map rather than the grammar:

```go
func FormatParseError(source []byte, root *gotreesitter.Node, lang *gotreesitter.Language,
    sourceFile string, expectations map[string]*ProductionExpectations) string
```

## File structure

New file: `errors.go` — keeps error formatting separate from transpilation. Contains:

- `FormatParseError(...)` — main entry point
- `buildExpectationMap(g *Grammar) map[string]*ProductionExpectations` — grammar introspection, linear expansion enumeration
- `findErrors(root *Node, lang *Language) []*Node` — error node collection with dedup per top-level block
- `matchPrefix(parent *Node, lang *Language, expectations *ProductionExpectations) []ExpectedStep` — match parsed children against expansions to determine what's next
- `inferFromKeyword(errorNode *Node, source []byte) string` — Layer 3 keyword inference
- `errorOverlays` map — hand-written messages
- `keywordToProduction` map — keyword → intended production mapping
- `formatSourceLine(source []byte, row, startCol, endCol int) string` — source line with caret (0-indexed input, 1-indexed output)
- `formatError(sourceFile string, row, col int, message, sourceLine, hint string) string` — assemble the final error string

New test file: `errors_test.go` — tests for error formatting.

## Integration

In `transpile.go`, update `getDanmujiLanguage` to also build and cache the expectation map (see "Caching" above).

In `TranspileDanmuji`, replace:

```go
if root.HasError() {
    return "", fmt.Errorf("parse errors:\n%s", root.SExpr(lang))
}
```

with:

```go
if root.HasError() {
    return "", fmt.Errorf("%s", FormatParseError(source, root, lang, opts.SourceFile, danmujiExpectations))
}
```

## Testing

### Overlay tests (~30)
One test per hand-written overlay pattern: intentionally break a construct, verify the error message contains the expected message text, correct line number, and hint example.

### Layer 1 fallback tests (~5)
Break constructs that have no overlay, verify the grammar-derived message is reasonable (mentions what was expected and the parent context).

### Layer 3 keyword inference tests (~5)
Break constructs badly enough that the parser doesn't create the expected parent node. Verify the keyword-based fallback still produces a useful message.

### Multi-error tests (~3)
- File with errors in 2 separate test blocks: reports both.
- File with 2 errors in the same block: reports only the first.
- File with error in a test block and error in a Go function: reports both.

### Source line rendering tests (~3)
Verify caret position for: normal ASCII, leading tabs/spaces, error at end of line. Verify 0→1 index conversion.

### Edge case tests (~3)
- Empty file: reports "expected package clause"
- Missing SourceFile: location shows `line:col` without filename
- Error node containing danmuji keyword but no parent: keyword inference fires

### Integration tests (~2)
Verify `TranspileDanmuji` returns the new error format. Verify existing grammar_test.go parse tests still work (they check for ERROR in s-expressions — they use the parser directly, not `TranspileDanmuji`, so they are unaffected).
