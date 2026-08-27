# Chapter 11 learning plan — Resolving and binding

This plan follows
[Crafting Interpreters, Chapter 11](https://craftinginterpreters.com/resolving-and-binding.html)
through this repository's Go implementation.

## Learning outcomes

By the end, you should be able to:

1. Explain why a closure could observe a later declaration before this chapter.
2. Distinguish a static error from a syntax error and from a runtime error.
3. Explain what the declared-but-undefined state in a scope is for.
4. Count the scope distance for a variable use by reading the source.
5. Pair every resolver scope with the runtime environment it predicts.
6. Explain why the global scope is not on the resolver's scope stack.
7. Explain why the locals table is keyed by node identity, and what that costs.
8. Say what the pass does and does not make faster, and measure it.
9. Explain why an unused local is a warning here and a redeclared one is an error.

Use this command prefix in the current environment:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache
```

## Session 1 — Reproduce the bug the chapter exists to fix

Read the opening of the chapter, then check out the previous commit and run:

```lox
var a = "global";
{
  fun showA() { print a; }
  showA();
  var a = "block";
  showA();
}
```

Draw the environment `showA` captured, and the moment `var a = "block"` changes
what it reads. Then run the same program on this commit.

Checkpoint: the two `showA()` calls are textually identical and the captured
environment is the same object. What, precisely, differed between them before?

## Session 2 — Separate the three kinds of error

Read [`pkg/errors/errors.go`](../pkg/errors/errors.go) and compare `ParseError`,
`ResolveError`, and `RuntimeError`.

Run each of these as a script and note the message and the exit code:

```lox
var a = ;                    // syntax
{ var a = 1; var a = 2; }    // static
print undefined_name;        // runtime
```

Checkpoint: the resolver collects its errors and keeps walking, while the parser
unwinds to a statement boundary. Why is that difference not an inconsistency?

## Session 3 — Trace the scope stack

Read `declare`, `define`, `resolveLocal`, and `VisitVariableExpr` in
[`resolver/resolver.go`](../resolver/resolver.go).

Predict the recorded distance for every variable use, then check your answers:

```lox
var value = "global";
fun outer() {
  var value = "outer";
  fun middle() {
    fun inner() { return value; }
    return inner;
  }
  return middle();
}
```

Also predict which of these are errors, and which rule catches them:

```lox
{ var a = a; }
var a = a;
{ var a = 1; { var a = a; } }
fun f(a, a) {}
```

Checkpoint: why does the initializer rule only ever look at the innermost scope?

## Session 4 — Pair the scopes with the environments

Read `VisitBlockStmt` and `resolveFunction` in the resolver, then
`VisitBlockStmt` and `executeBlock` in
[`interpreter/interpreter.go`](../interpreter/interpreter.go) and `Call` in
[`interpreter/function.go`](../interpreter/function.go).

For this program, list every resolver scope and the runtime environment it
predicts, in order:

```lox
fun f(n) {
  var double = n * 2;
  if (n > 0) { var inner = double; return f(n - 1); }
  return double;
}
```

Then break the invariant on purpose: make `resolveFunction` resolve the body as
a `Block` instead of resolving its statements, run the tests, and read the
failures.

Checkpoint: what would a program have to look like for that mistake to produce
wrong output rather than a crash?

## Session 5 — Read the resolved variable

Read `Resolve`, `lookUpVariable`, and `VisitAssignExpr` in the interpreter, then
`ancestor`, `GetAt`, and `AssignAt` in
[`interpreter/environment.go`](../interpreter/environment.go).

Run [`examples/resolving-and-binding.lox`](../examples/resolving-and-binding.lox)
and account for the `3` at the end: the loop closure and the loop variable.

Then execute a program without resolving it — `TestUnresolvedLocalFallsBackToGlobals`
in [`interpreter/interpreter_test.go`](../interpreter/interpreter_test.go) does
exactly that — and explain the failure.

Checkpoint: `GetAt` has no not-found branch and `Get` does. Which one is the
special case, and why?

## Session 6 — Read the two extensions

Both code challenges are implemented, so this session reads rather than writes.

Read `binding`, `scope`, `endScope`, and `resolveLocal` in
[`resolver/resolver.go`](../resolver/resolver.go), then `Define`, `GetAt`, and
`AssignAt` in [`interpreter/environment.go`](../interpreter/environment.go).

Predict the diagnostics, then check:

```lox
{ var written = 0; written = 1; }
fun f(ignored) { return 1; } print f(1);
{ fun helper() { return 1; } }
{ var a = 1; fun show() { print a; } show(); }
```

Then work out the slot number of every local in this program, and the
`(distance, index)` pair for every use:

```lox
{
  var first = "1";
  var second = "2";
  fun swap() { var carried = first; first = second; second = carried; }
  swap();
}
```

Run the benchmarks, and account for the memory number being the one that moved
most:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache \
  go test ./interpreter -run '^$' -bench . -benchmem -count 10
```

Checkpoint: `Define` appends in a local environment. What property of Lox
declarations makes that safe, and which single language feature would break it?

## Session 7 — Confirm both front ends and the REPL

```sh
env GOTOOLCHAIN=go1.26.6 go run . examples/resolving-and-binding.lox
env GOTOOLCHAIN=go1.26.6 go run . -print=ast examples/resolving-and-binding.lox
env GOTOOLCHAIN=go1.26.6 go run . -parser=llk -k=3 examples/resolving-and-binding.lox
```

The evaluated outputs must agree. Then use the REPL: define a block-scoped
variable on one line, trigger a static error on the next, and keep going.

Finish with:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache go test ./...
```

Final checkpoint: the resolver needed no change for either parser, and no change
for `for` loops. Which earlier design decision bought that, in each case?

## Challenges

1. **Open.** Answer in prose: the resolver defines a function's own name before
   resolving its body. Why is that safe, when defining a variable before its
   initializer is resolved is not?
2. **Implemented** — unused locals, reported as warnings. See
   [the chapter notes](11-resolving-and-binding.md#unused-locals-challenge-2)
   for the two open decisions it had to make: whether a write counts as a use
   (no, as in Go), and whether parameters are checked (no). Worth arguing with:
   the challenge says to report an error, and this reports a warning.
3. **Implemented** — `(distance, index)` pairs and slice-backed local
   environments, at roughly −25% time and −54% memory. See
   [the chapter notes](11-resolving-and-binding.md#slots-instead-of-names-challenge-3).

Two extensions neither challenge asks for, if you want more:

- Report an unused local that is only read by itself — the recursive-function
  case `TestUnusedCheckIsSatisfiedBySelfReference` leaves open. Reachability
  from used names, rather than "was it read", is the analysis you need.
- Pre-size a call environment's slice from the resolver's scope size, so a call
  frame allocates once regardless of how many locals the body declares. It needs
  the scope size on the `ast.Function` node, which means touching the generator.
