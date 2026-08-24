# Chapter 10 — Functions, in Go

Implementation of [Functions](https://craftinginterpreters.com/functions.html)
for this repository's recursive-descent and table-driven LL(k) front ends.

> Status: implemented. Programs support postfix calls, the native `clock()`
> function, user-defined functions, parameters, return values, recursion,
> local functions, and closures. Both parsers produce the same trees for every
> supported lookahead (`k = 1..3`).

For a guided walkthrough, follow the [Chapter 10 learning plan](10-learning-plan.md).

## Syntax and trees

`Call` stores the callee, the closing parenthesis used for runtime diagnostics,
and the argument expressions. Because calls are postfix operators above unary
precedence, the parser accepts both `-clock()` and chained calls such as
`makeAdder(1)(2)`.

`Function` stores its name, parameter tokens, and body statements. `Return`
keeps the keyword token and an optional value expression. The scanner already
recognized `fun` and `return`, so its production code did not change.

Both parameter and argument lists are capped at 255 entries. Like the book,
the parsers report the limit without abandoning the otherwise valid syntax.

## Calling values

Every runtime value that can be called implements the same `Callable`
interface: it reports its arity and accepts an interpreter plus evaluated
arguments. A call evaluates its callee first and its arguments from left to
right, then checks that the callee is callable and that the arity matches.
Both failures point at the call's closing parenthesis.

The interpreter owns a stable global environment and defines `clock` there.
`clock()` is a zero-argument native function returning seconds since the Unix
epoch. Native and user functions print as `<native fn>` and `<fn name>`.

## Function calls and returns

A function declaration wraps its syntax node in a runtime `LoxFunction` and
binds it like any other value. Each invocation creates a fresh environment,
binds parameters to arguments, and executes the body there. Fresh call
environments are what make recursion safe.

Falling off the end of a body returns `nil`. An explicit `return` panics with a
private control-flow signal; the function call boundary catches only that
signal and turns its payload into the call's value. Runtime-error signals pass
through unchanged, and the existing deferred environment restoration runs in
both cases.

## Closures

A `LoxFunction` retains the environment active when it is declared. The fresh
call environment uses that captured environment as its parent, so a returned
local function can continue reading and assigning variables from an outer call
that has already completed.

The function captures an environment pointer before its own name is defined
into that environment. Once the binding is added, recursive calls can find it
through the same captured scope.

Chapter 11 still needs to resolve one deliberate limitation: without static
resolution, a later declaration in a captured environment can incorrectly
change which binding a closure observes.

## Two parser front ends

Recursive descent translates `call → primary ( "(" arguments? ")" )*` into a
loop and parses function declarations with a dedicated helper.

The LL(k) grammar uses a `callTail` production and semantic actions that fold a
call before recurring, preserving left association. Return statements remain
table-driven. Function declarations use the orchestration layer because their
bodies share the recoverable block parser and may contain compound statements.

## Verification

The suite covers call precedence and chaining, syntax diagnostics, both
255-item limits, parser parity, native calls, non-callable and arity errors,
argument evaluation order, implicit and explicit returns, early return through
nested control flow, recursion, independent closure state, and REPL reuse.

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache \
  go generate ./...

env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache \
  go test ./...
```

## Scope boundary

The chapter's anonymous-function challenge is not implemented. Rejecting a
top-level `return` and repairing the remaining closure binding ambiguity belong
to Chapter 11's resolver.
