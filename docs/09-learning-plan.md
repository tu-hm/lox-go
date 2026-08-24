# Chapter 9 Learning Plan — Control Flow

Use this plan after reading
[Control Flow](https://craftinginterpreters.com/control-flow.html). It follows
this repository's Go implementation and its two parser front ends.

## Learning outcomes

By the end, you should be able to:

1. Explain why logical operators need their own AST node.
2. Derive the precedence of `and` and `or` from the parser call graph.
3. Explain how eager `else` parsing resolves the dangling-`else` ambiguity.
4. Trace `if` and `while` execution, including skipped subtrees.
5. Desugar every combination of optional `for` clauses by hand.
6. Explain why a `for` initializer needs an enclosing block.
7. Compare recursive-descent control parsing with the LL(k) orchestration
   layer.
8. Prove short-circuiting using an observable side effect.

Use this command prefix in the current environment:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache
```

## Session 1 — Model control flow in the AST

Read:

- [Chapter 9 grammar](grammar.md#chapter-9--control-flow)
- [`cmd/genast/main.go`](../cmd/genast/main.go)
- [`ast/expr.go`](../ast/expr.go)
- [`ast/stmt.go`](../ast/stmt.go)

Exercises:

1. Classify `Logical`, `If`, and `While` as expressions or statements.
2. Explain why `For` is absent.
3. Draw the tree for `if (a) print b; else print c;`.
4. Run the generator freshness test:

   ```sh
   env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache \
     go test ./cmd/genast ./ast
   ```

Checkpoint: why can `ElseBranch` be nil while `ThenBranch` cannot?

## Session 2 — Parse logical expressions

Read `assignment`, `or`, and `and` in:

- [`parser/parser.go`](../parser/parser.go)
- [`parser/llk/grammar.go`](../parser/llk/grammar.go)

Predict the tree for:

```lox
false or true and false or nil
```

Then run:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache \
  go test ./parser ./parser/llk -run Logical -v
```

Checkpoint: why does assignment call `or`, while `and` calls equality?

## Session 3 — Parse branches and loops

Read `statement`, `ifStatement`, and `whileStatement` in both parser files.

Trace this without running it:

```lox
if (a)
  if (b) print 1;
  else print 2;
```

Exercises:

1. Identify which `if` owns the `else`.
2. Remove each parenthesis in turn and predict the diagnostic.
3. Compare the two parser outputs at every `k`:

   ```sh
   env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache \
     go test ./parser -run TestAlgorithmsAgreeOnPrograms -v
   ```

Checkpoint: why is compound-statement selection outside the LL(k) prediction
table even though logical expressions remain inside it?

## Session 4 — Desugar `for`

Read `forStatement` in both parser implementations. Expand this by hand:

```lox
for (var i = 0; i < 3; i = i + 1) print i;
```

Repeat for:

```lox
for (;;) print 1;
for (; condition;) body;
for (initialize; condition;) body;
```

Check your trees with:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache \
  go test ./parser ./parser/llk -run 'ControlFlow|Desugar' -v
```

Checkpoint: which synthesized block controls initializer scope, and which
ensures the increment runs after the body?

## Session 5 — Execute without evaluating everything

Read the control-flow visitors in
[`interpreter/interpreter.go`](../interpreter/interpreter.go).

Predict output and side effects:

```lox
var changed = false;
print "left" or (changed = true);
print false and missing;
print changed;
```

Run:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache \
  go test ./interpreter -run 'Logical|IfAndWhile|ForDesugaring' -v
```

Checkpoint: why do `and` and `or` return operands instead of booleans?

## Session 6 — Trace the complete pipeline

Run the example through both parsers and both debug printers:

```sh
env GOTOOLCHAIN=go1.26.6 go run . examples/control-flow.lox
env GOTOOLCHAIN=go1.26.6 go run . -print=ast examples/control-flow.lox
env GOTOOLCHAIN=go1.26.6 go run . -parser=llk -k=3 examples/control-flow.lox
```

Trace one `for` statement through scanning, parsing, desugaring, and execution.
Finish with the full suite:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache go test ./...
```

Final checkpoint: describe one observable behavior that would change if a
logical expression eagerly evaluated both operands.
