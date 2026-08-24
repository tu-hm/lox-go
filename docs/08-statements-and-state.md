# Chapter 8 — Statements and State, in Go

Implementation of [Statements and State](https://craftinginterpreters.com/statements-and-state.html)
for this repository's recursive-descent and table-driven LL(k) front ends.

> Status: implemented. Programs can print, declare/read/assign variables, and
> use nested lexical scopes. Both parsers produce the same statement trees for
> every supported lookahead (`k = 1..3`).

For a guided, exercise-based walkthrough, follow the
[Chapter 8 learning plan](08-learning-plan.md).

## What landed

- `ast.Expr` adds `Assign` and `Variable`.
- A generated `ast.Stmt` hierarchy adds `Block`, `Expression`, `Print`, and
  `Var`.
- `ParseProgram` is the normal script entry point. The old expression entry
  points remain for the expression-friendly REPL and chapter 7 tests.
- `interpreter.Environment` stores bindings and links nested scopes.
- `Interpreter.Execute` runs programs; `Interpreter.Interpret` remains the
  focused expression evaluator.
- Lox `print` writes through an injected `io.Writer`, so output tests do not
  capture process-wide stdout.
- The REPL owns one interpreter for its whole lifetime, so bindings survive
  between lines.

## Go-specific decisions

### Runtime errors still unwind through one private signal

Undefined variables use the same `runtimeSignal` boundary as type errors.
`Environment` returns a `*errors.RuntimeError`; the interpreter turns it into
the private panic used to abandon recursive visitor calls. Public methods still
return ordinary errors and unexpected Go panics are re-thrown.

### Scope restoration uses `defer`

Executing a block swaps the interpreter's current environment, defers restoring
the previous one, and then executes the body. The deferred restoration is
essential: it runs even when a runtime error unwinds out of the block.

### Assignment stays LL(1)

The recursive-descent parser parses equality first and validates the left side
after seeing `=`. The table grammar uses the equivalent factored form:

```
assignment     → equality assignmentTail ;
assignmentTail → "=" assignment | ε ;
```

The action after the recursive right-hand side converts a `Variable` node into
an `Assign` node or reports `Invalid assignment target.`. Recursing on the
right makes `a = b = 3` parse as `a = (b = 3)`.

## Runtime semantics

- An omitted initializer produces Lox `nil`.
- A declaration may replace a binding in the current scope.
- Assignment updates the nearest existing binding and never declares one.
- Lookup and assignment walk from the innermost environment outward.
- A local declaration shadows, but does not delete or mutate, the outer
  binding.
- An assignment expression evaluates to the assigned value.

## Verification

The test suite covers generated-file freshness, parser recovery, parser parity,
right-associative assignment, defined `nil` versus a missing binding, nested
shadowing, undefined-variable errors, output, and environment restoration after
a runtime error.

Run it with the toolchain currently installed for this module:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache go test ./...
```

## Chapter challenges

1. Done: the REPL accepts both full statements and unterminated bare
   expressions, printing only the latter automatically.
2. Open: distinguish an uninitialized variable from one initialized to `nil`.
3. Current Lox behavior for `var a = a + 2;` inside a block is to read the
   enclosing `a` before defining the local one, matching the chapter's runtime
   environment order. Chapter 11's resolver is where this choice becomes a
   static binding rule.
