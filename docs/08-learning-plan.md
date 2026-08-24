# Chapter 8 Learning Plan — Statements and State

Use this plan after reading
[Statements and State](https://craftinginterpreters.com/statements-and-state.html).
It is organized around this repository's Go implementation and its two parser
front ends. The goal is to understand the design well enough to explain it,
trace it by hand, and safely change it—not merely to recognize the finished
code.

## Learning outcomes

By the end, you should be able to:

1. Explain why expressions and statements use separate AST hierarchies.
2. Trace a program from source through scanning, parsing, and execution.
3. Explain why assignment is right-associative and lower precedence than
   equality.
4. Implement `Define`, `Get`, and `Assign` semantics for a chain of lexical
   environments.
5. Predict lookup, shadowing, and assignment behavior in nested blocks.
6. Explain how `defer` protects interpreter state during runtime-error
   unwinding.
7. Compare the recursive-descent and LL(k) implementations of the same grammar.
8. Explain why one interpreter instance must live for an entire REPL session.

## Working method

Plan for seven sessions of 45–90 minutes. For each session:

1. Read only the listed section and files.
2. Predict behavior before running anything.
3. Run the focused tests.
4. Complete the exercise without copying the existing test implementation.
5. Write a two- or three-sentence explanation in your own words.

Use this command prefix in the current environment:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache
```

For example:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache \
  go test ./interpreter -run TestExecuteStatementsVariablesAndScope -v
```

## Session 1 — Build the program model

**Time:** 45–60 minutes

Read:

- [Chapter 8 grammar](grammar.md#chapter-8--statements-and-state)
- [`cmd/genast/main.go`](../cmd/genast/main.go)
- [`ast/expr.go`](../ast/expr.go)
- [`ast/stmt.go`](../ast/stmt.go)
- [`ast/stmt_printer.go`](../ast/stmt_printer.go)

Focus on the separation:

```text
Expr → produces a runtime value
Stmt → performs an action
Program → ordered list of Stmt
```

Exercises:

1. For each node below, classify it as `Expr` or `Stmt` before checking the
   generated files: `Assign`, `Binary`, `Block`, `Print`, `Var`, `Variable`.
2. Predict the AST for this program:

   ```lox
   var a = 1;
   print a = a + 1;
   ```

3. Compare your answer with:

   ```sh
   env GOTOOLCHAIN=go1.26.6 go run . -print=ast examples/statements-and-state.lox
   ```

Checkpoint:

- Why is `Assign` an expression while `Var` is a statement?
- Why does `Var.Initializer` allow a nil Go interface?
- What compile-time failure occurs if a generated node is added without
  updating a visitor?

## Session 2 — Parse complete programs

**Time:** 60–75 minutes

Read:

- [`parser.Algorithm`](../parser/algorithm.go)
- `ParseProgram`, `declaration`, `statement`, and `block` in
  [`parser/parser.go`](../parser/parser.go)
- Program parser tests in [`parser/parser_test.go`](../parser/parser_test.go)

Trace this source by hand, recording the parser method entered at each leading
token:

```lox
var outer = 1;
{
  print outer;
  outer = 2;
}
```

Exercises:

1. Explain why `declaration` sits above `statement` in the grammar.
2. Remove one semicolon in a scratch input and predict where synchronization
   resumes.
3. Run:

   ```sh
   env GOTOOLCHAIN=go1.26.6 go test ./parser \
     -run 'TestParseProgram|TestParseProgramRecoversInsideBlock' -v
   ```

4. Add a temporary table-driven test for an empty block and a three-level
   nested block. Make your prediction before running it.

Checkpoint:

- Why must the block loop check both `RIGHT_BRACE` and EOF?
- Why does parsing return a slice of statements instead of a single root
  statement?
- What guarantees that recovery cannot loop forever on the same token?

## Session 3 — Understand assignment

**Time:** 60–75 minutes

Read:

- `assignment` in [`parser/parser.go`](../parser/parser.go)
- `Assign` and `Variable` in [`ast/expr.go`](../ast/expr.go)
- `VisitAssignExpr` in
  [`interpreter/interpreter.go`](../interpreter/interpreter.go)

Work through these before running them:

```lox
var a;
var b;
print a = b = 3;
print a;
print b;
```

Draw the assignment tree and number the order in which its nodes evaluate.

Exercises:

1. Explain why the right-hand side calls `assignment()` recursively instead of
   using a loop.
2. Predict the parse result of `(a) = 3;` and `a + b = 3;`.
3. Run:

   ```sh
   env GOTOOLCHAIN=go1.26.6 go test ./parser ./interpreter \
     -run 'Assignment|InvalidAssignment' -v
   ```

Checkpoint:

- What is the difference between an l-value and an r-value here?
- Why does the `Assign` node store a name token rather than the original left
  expression?
- Why must assignment return the stored value?

## Session 4 — Compare recursive descent with LL(k)

**Time:** 75–90 minutes

Read:

- [LL(k) parser guide](llk-parser.md)
- Statement and assignment productions in
  [`parser/llk/grammar.go`](../parser/llk/grammar.go)
- `ParseProgram` and `run` in
  [`parser/llk/parser.go`](../parser/llk/parser.go)
- Parser parity tests in [`parser/algorithm_test.go`](../parser/algorithm_test.go)

Compare the two assignment shapes:

```text
Recursive descent:
assignment → equality ( "=" assignment )?

LL(k):
assignment     → equality assignmentTail
assignmentTail → "=" assignment | ε
```

Exercises:

1. Explain why both forms create the same tree.
2. On paper, trace the LL(k) value stack for `a = b = 3` until `mkAssign` runs
   twice.
3. Run parity at every supported lookahead:

   ```sh
   env GOTOOLCHAIN=go1.26.6 go test ./parser \
     -run TestAlgorithmsAgreeOnPrograms -v
   ```

4. Temporarily move `act(mkAssign)` before the recursive assignment in a local
   experiment. Predict whether the failure is a grammar conflict, stack error,
   or wrong tree before running the tests.

Checkpoint:

- Why does factoring assignment preserve LL(1)?
- What is invisible to FIRST/FOLLOW analysis but visible to the value stack?
- Why are semantic-action errors propagated by the driver?

## Session 5 — Environments and global variables

**Time:** 45–60 minutes

Read:

- [`interpreter/environment.go`](../interpreter/environment.go)
- `VisitVariableExpr` and `VisitVarStmt` in
  [`interpreter/interpreter.go`](../interpreter/interpreter.go)
- `TestEnvironmentDistinguishesNilFromMissing` in
  [`interpreter/interpreter_test.go`](../interpreter/interpreter_test.go)

Build this state table by hand:

| Statement | Environment after execution | Output |
|---|---|---|
| `var a;` | ? | — |
| `print a;` | ? | ? |
| `var a = 2;` | ? | — |
| `print a;` | ? | ? |

Exercises:

1. Explain why `map[string]any` needs the comma-ok lookup form.
2. Contrast `Define` with `Assign`: which may create a binding?
3. Predict the token and line attached to `print missing;`.
4. Run:

   ```sh
   env GOTOOLCHAIN=go1.26.6 go test ./interpreter \
     -run 'Environment|UndefinedVariable' -v
   ```

Checkpoint:

- How does the runtime distinguish “defined as Lox nil” from “not defined”?
- Why are environment keys strings while lookup errors retain tokens?
- Why is undefined-variable access a runtime error rather than a parse error?

## Session 6 — Lexical scope, shadowing, and unwinding

**Time:** 60–75 minutes

Read:

- `VisitBlockStmt` and `executeBlock` in
  [`interpreter/interpreter.go`](../interpreter/interpreter.go)
- Recursive `Get` and `Assign` in
  [`interpreter/environment.go`](../interpreter/environment.go)
- `TestRuntimeErrorRestoresBlockEnvironment` in
  [`interpreter/interpreter_test.go`](../interpreter/interpreter_test.go)

Predict every output and final binding before running:

```lox
var a = "global";
var b = "global";
{
  var a = "local";
  b = "changed";
  print a;
  print b;
}
print a;
print b;
```

Exercises:

1. Draw the environment chain while execution is inside the block.
2. Mark which environment handles each `Get` and `Assign`.
3. Explain what would break if `executeBlock` restored `environment` after the
   loop without `defer`.
4. Run:

   ```sh
   env GOTOOLCHAIN=go1.26.6 go test ./interpreter \
     -run 'Scope|RestoresBlockEnvironment' -v
   ```

Checkpoint:

- How is shadowing different from overwriting an outer variable?
- Why does `Define` never walk outward?
- Why do `Get` and `Assign` walk outward?

## Session 7 — CLI, REPL, and the complete pipeline

**Time:** 45–60 minutes

Read:

- `runSource`, `emitProgram`, and `isBareExpression` in [`main.go`](../main.go)
- [`main_test.go`](../main_test.go)
- [`examples/statements-and-state.lox`](../examples/statements-and-state.lox)

Trace these two REPL lines and identify the object that must survive between
them:

```lox
var answer = 42;
print answer;
```

Exercises:

1. Explain why scripts always use `ParseProgram` but the REPL keeps a bare
   expression path.
2. Compare an expression statement with a bare REPL expression: both evaluate,
   but which one prints automatically?
3. Run the same program through both parsers:

   ```sh
   env GOTOOLCHAIN=go1.26.6 go run . examples/statements-and-state.lox
   env GOTOOLCHAIN=go1.26.6 go run . -parser=llk examples/statements-and-state.lox
   ```

4. Inspect both debug representations:

   ```sh
   env GOTOOLCHAIN=go1.26.6 go run . -print=ast examples/statements-and-state.lox
   env GOTOOLCHAIN=go1.26.6 go run . -print=rpn examples/statements-and-state.lox
   ```

Checkpoint:

- Why was creating an interpreter inside every REPL iteration incorrect?
- Which layer owns Lox `print` output, and why is its writer injected?
- Why should a script containing `1 + 2;` produce no output?

## Capstone exercises

Choose one after completing all seven sessions.

### A. Uninitialized-variable challenge

Change the runtime so `var a; print a;` is an error, while `var a; a = nil;
print a;` is valid. You will need a private sentinel distinct from Go nil.

Acceptance criteria:

- Explicit Lox `nil` remains printable and comparable.
- Reading the sentinel reports a runtime error at the variable token.
- Assignment replaces the sentinel.
- Both parser front ends remain unchanged.

### B. Environment-chain visualization

Add a debug-only method or test helper that renders the current chain from the
innermost scope outward. Use it to demonstrate shadowing across three blocks.
Do not expose mutable environment maps to normal callers.

### C. Stronger recovery parity

Add malformed nested-block programs to `TestAlgorithmsAgreeOnPrograms`. Check
that both parsers retain the same valid statements and report the same number
of errors without entering an infinite loop.

## Final self-check

You are ready for Chapter 9 when you can answer these without opening the code:

1. What exact AST represents `a = b = 3`?
2. Which environment changes for an assignment to an outer variable from an
   inner block?
3. Why can a variable initialized to `nil` not be detected with `value == nil`
   alone?
4. What does `defer` protect in block execution?
5. Why do expression statements exist if their values are discarded?
6. How do the recursive-descent and LL(k) assignment rules differ structurally?
7. Why does the REPL reuse one interpreter?

Finish with the complete verification pass:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache \
  go test -count=1 ./...
```
