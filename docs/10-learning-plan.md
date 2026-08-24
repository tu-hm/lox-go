# Chapter 10 learning plan — Functions

This plan follows
[Crafting Interpreters, Chapter 10](https://craftinginterpreters.com/functions.html)
through this repository's Go implementation and its two parser front ends.

## Learning outcomes

By the end, you should be able to:

1. Explain why a call stores its closing parenthesis token.
2. Derive call precedence and the tree for chained calls.
3. Trace callee and argument evaluation order.
4. Explain how arity separates call validation from call implementation.
5. Trace parameter bindings through a recursive invocation.
6. Explain why return uses a private unwinding signal.
7. Draw the environment chain retained by a closure.
8. Identify the lexical-binding problem deferred to Chapter 11.

Use this command prefix in the current environment:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache
```

## Session 1 — Model calls and functions

Read:

- [Chapter 10 grammar](grammar.md#chapter-10--functions)
- [`cmd/genast/main.go`](../cmd/genast/main.go)
- [`ast/expr.go`](../ast/expr.go)
- [`ast/stmt.go`](../ast/stmt.go)

Draw the trees for:

```lox
add(1, 2 * 3);
-factory()(42);
fun identity(value) { return value; }
```

Checkpoint: why does `Call` store `Paren`, while `Function` stores parameter
tokens rather than only parameter strings?

## Session 2 — Parse postfix calls

Read `unary`, `call`, and `finishCall` in [`parser/parser.go`](../parser/parser.go),
then compare the factored rules and argument-list actions in
[`parser/llk/grammar.go`](../parser/llk/grammar.go).

Run:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache \
  go test ./parser ./parser/llk -run 'Functions|Calls|Limit' -v
```

Checkpoint: why must each LL(k) call node be built before recurring into the
next `callTail`?

## Session 3 — Understand the callable boundary

Read [`interpreter/callable.go`](../interpreter/callable.go) and
`VisitCallExpr` in [`interpreter/interpreter.go`](../interpreter/interpreter.go).

Predict which expression runs first and where each failure is reported:

```lox
notAFunction(sideEffect());
oneArgument(first(), second());
```

Checkpoint: why is arity checked once by the interpreter instead of by every
callable implementation?

## Session 4 — Bind and return values

Read [`interpreter/function.go`](../interpreter/function.go) and the function
and return visitors. Trace this call, drawing a new environment for each frame:

```lox
fun count(n) {
  if (n > 1) count(n - 1);
  print n;
}
count(3);
```

Then trace an early return from inside a loop and explain which Go frames the
return signal unwinds.

Checkpoint: what distinguishes a return signal from a runtime-error signal?

## Session 5 — Draw a closure

Run and trace the `makeCounter` program in
[`examples/functions.lox`](../examples/functions.lox). Draw the chain from the
`count` call environment through the captured `makeCounter` environment to the
globals.

Checkpoint: why do two calls to `makeCounter()` retain independent values of
`i`?

## Session 6 — Compare both front ends

Run:

```sh
env GOTOOLCHAIN=go1.26.6 go run . examples/functions.lox
env GOTOOLCHAIN=go1.26.6 go run . -print=ast examples/functions.lox
env GOTOOLCHAIN=go1.26.6 go run . -parser=llk -k=3 examples/functions.lox
env GOTOOLCHAIN=go1.26.6 go run . -parser=llk -k=3 -print=ast examples/functions.lox
```

The evaluated outputs and printed trees must agree. Finish with:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache go test ./...
```

Final checkpoint: explain why closures already work in Chapter 10 but still
need the resolver introduced in Chapter 11.
