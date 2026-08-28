# Chapter 11 — Resolving and binding, in Go

Implementation of
[Resolving and Binding](https://craftinginterpreters.com/resolving-and-binding.html)
for this repository's tree-walking interpreter.

> Status: implemented, including both code challenges. A static resolution pass
> runs between parsing and execution. Every local variable use is bound to a
> declaration by scope distance and slot index, closures no longer observe
> declarations that appear after them, three static errors are refused before the
> program runs, and locals nobody reads are reported as warnings. The grammar and
> both parser front ends are unchanged.

For a guided walkthrough, follow the [Chapter 11 learning plan](11-learning-plan.md).

## What the chapter changes, and what it does not

Chapter 11 adds no syntax. There is no new AST node, so
[`cmd/genast/main.go`](../cmd/genast/main.go) and both parsers are untouched.
What changes is the answer to a question the interpreter used to re-ask on every
read: *which declaration does this name refer to?*

Until now the answer came from walking the environment chain at runtime, so it
depended on what happened to be defined at that moment. The chapter's motivating
program shows the consequence:

```lox
var a = "global";
{
  fun showA() { print a; }
  showA();
  var a = "block";
  showA();
}
```

Both calls must print `global`. Before this chapter the second printed `block`:
`showA` captured the block's environment, and the later `var a` added a binding
to that same environment, quietly changing what an already-declared closure saw.

## The resolver

[`resolver/resolver.go`](../resolver/resolver.go) walks the tree once. It visits
every node, but unlike the interpreter it branches on nothing: both arms of an
`if` are resolved, a loop body is resolved once rather than once per iteration,
and no expression is evaluated. Scope structure is a property of the text.

The pass keeps a stack of scopes, each a map from name to *whether its
initializer has finished*. That extra bit is what separates the two shadowing
cases:

```lox
var a = "outer";
{ var a = a; }        // error: reads the a being declared
{ var a = "outer"; }  // fine: shadows the outer a
```

`declare` adds the name unfinished, the initializer is resolved, then `define`
marks it readable. A read of a name that is present in the innermost scope but
unfinished can only be the variable whose initializer is being resolved.

For every variable use, `resolveLocal` walks the stack innermost-first and hands
the interpreter the number of scopes crossed. Not finding the name is not an
error: that is what *global* means here. The global scope is deliberately not on
the stack, which is what keeps forward references and the REPL working.

Function declarations define their own name before the body is resolved, so a
function can recurse.

## The one invariant

Each `beginScope` in the resolver must correspond to exactly one
`NewEnvironment` at runtime, or every distance below it is wrong. There are
exactly two pairs:

| Resolver | Interpreter |
| --- | --- |
| `VisitBlockStmt` | `VisitBlockStmt` creates one environment per block |
| `resolveFunction` | `LoxFunction.Call` creates one environment per call |

Nothing else creates a scope. `if` and `while` do not; `for` only does through
the `Block` the parser already builds when desugaring it. That is also why
`resolveFunction` resolves the body statements directly instead of as a block:
at runtime, parameters and body share one environment.

## Static errors

Three rules that the grammar cannot express are reported here, as
`*errors.ResolveError` values through the same `pkg/errors` reporting the parser
uses:

| Program | Message |
| --- | --- |
| `{ var a = 1; var a = 2; }` | `Already a variable with this name in this scope.` |
| `{ var a = a; }` | `Can't read local variable in its own initializer.` |
| `return 1;` at top level | `Can't return from top-level code.` |

Redeclaration is still legal in the global scope, on purpose: it is what makes a
long-lived REPL usable.

Unlike a parse error, a static error is not a signal to unwind. The resolver has
no ambiguity to recover from, so it keeps walking and reports every error in one
pass. `main.go` refuses to execute a program that produced any, which reaches
the user as exit code 65 — the same as a syntax error, because in both cases
nothing ran.

## Unused locals (challenge 2)

A scope keeps a `binding` per name rather than a bare bool: the name token, what
it was declared as, whether its initializer finished, and whether anything reads
it. `endScope` reports the names nobody read, because once a scope closes
nothing outside it can name them — which is what makes this a static question at
all.

```
[line 17] Warning at 'a': Local variable 'a' is never used.
```

Two decisions the book leaves open:

- **Assigning to a variable does not count as using it.** `resolveLocal` is told
  whether the mention is a read or a write, so `{ var a = 0; a = 1; }` is
  reported. Go draws the line in the same place.
- **Parameters are exempt.** A parameter's presence is dictated by the caller's
  signature rather than by the body, and Lox has no way to spell a deliberately
  unused name — no `_`, no attribute — so a report would be unescapable.

This reports rather than refuses, which is a departure from the challenge as
worded. An unused local cannot make a program behave wrongly, so refusing to run
it buys nothing and costs the user a working program. `pkg/errors.Warning`
exists for exactly that class: it prints, it does not touch `HadError`, and it
deliberately has no `Error` method so it cannot be returned where an error is
expected and quietly become fatal. Promoting it to an error is one line — call
`r.fail` instead of `r.warn` in `endScope`.

Bindings are kept in declaration order as well as by name, so warnings come out
in source order, innermost scope first. One known limitation, pinned by
`TestUnusedCheckIsSatisfiedBySelfReference`: a recursive call is a read, so an
unused recursive local function goes unreported. Answering that properly means
asking whether a name is reachable from anything that is used, which is a much
larger analysis.

## Reading a resolved variable

The interpreter keeps the answers in a side table, `locals`, and asks it before
touching an environment. A resolved use goes straight to `slot{distance, index}`;
an unresolved one is global by definition, and only that case can still fail at
runtime with `Undefined variable`.

`Environment.GetAt` and `AssignAt` have no not-found case, no name comparison,
and no search. That absence is the payoff of the pass rather than an oversight:
the resolver proved where the value sits. `Get` and `Assign`, which do search by
name, remain for globals.

An index out of range would mean the resolver and the interpreter disagree about
the shape of a scope. That is a bug in this interpreter rather than bad Lox, so
the panic it produces is the right outcome — the same line this repository
already draws in `TestInterpretDoesNotLaunderUnexpectedPanics`.

The table is keyed by the syntax node itself. Every `ast` node is used through a
pointer and Go compares pointer keys by identity, which is the same guarantee
the book gets from Java's `IdentityHashMap`: two textually identical uses in
different places stay separate entries. The cost is an invariant — nothing may
copy a node by value or rebuild the tree between resolution and execution, or
the recorded entry becomes unreachable and the use silently falls back to
globals.

## Slots instead of names (challenge 3)

The resolver numbers each scope's declarations in source order, and reports that
index alongside the distance. A local environment then needs no names at all:

| Scope | Storage | Found by |
| --- | --- | --- |
| global | `map[string]any` | name, at runtime |
| local | `[]any` | `(distance, index)`, computed once |

`Define` appends in a local environment, which is what makes the indices line
up: the resolver numbered the declarations in source order, and a scope's
declarations execute in that same order, at most once each. Lox cannot declare a
variable conditionally — `if (c) var a = 1;` is a syntax error, because `varDecl`
is a declaration and not a statement — and a redeclared local is now a static
error, so the n-th `Define` at runtime is always the n-th declaration in the
text. The one way a scope can run without filling every slot is an early
`return`, which leaves an unreachable tail; `TestEarlyReturnSkipsLaterSlots`
covers it.

Dropping the map from local environments is where most of the win comes from —
not the lookup, but the allocation it takes to make one per call frame:

| Benchmark | Before | After | |
| --- | --- | --- | --- |
| `Fib` time | 3.018 ms | 2.490 ms | −17% |
| `Fib` memory | 3221 KiB | 869 KiB | −73% |
| `Fib` allocations | 52,750 | 44,388 | −16% |
| `LocalAccess` time | 80.76 µs | 55.78 µs | −31% |
| `LocalAccess` memory | 70.7 KiB | 54.8 KiB | −22% |
| `LocalAccess` allocations | 3,004 | 2,004 | −33% |

Ten runs each on an Apple M5, `p < 0.001` on every row. Reproduce with:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache \
  go test ./interpreter -run '^$' -bench . -benchmem -count 10
```

## Where the pass sits

```
source → scanner → parser → resolver → interpreter
```

Resolution runs in `emitProgram`, only on the evaluating path, so
`-print=ast` still dumps the tree of a statically broken program. A bare REPL
expression skips the pass, because an expression cannot declare anything and
therefore has no local scopes to resolve.

The REPL builds a new resolver per line and keeps the same interpreter: the
scope stack is per-program, while the resolved distances accumulate alongside
the globals they were computed against.

Because the resolver reads trees rather than tokens, it is front-end agnostic.
Both parsers desugar `for` into the same `Block`/`While` shape, so one pass
serves `-parser=rd` and `-parser=llk` at every supported lookahead, and neither
parser needed a line of change.

## Verification

The suite covers the three static errors and their tokens, the legal bindings
they must not reject, multiple errors reported in one pass, the motivating
shadowing program, distances greater than one through nested closures,
assignment at a distance, slot order under rotation and under an early return,
front-end parity at `k = 1..3`, REPL survival after a static error, unused
locals — including the write-only case, the exempt parameter, and the source
ordering of the reports — and what an unresolved tree does: locals fall back to
globals, which is why resolution is a required stage and not an optimization.

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache \
  go test ./...

env GOTOOLCHAIN=go1.26.6 go run . examples/resolving-and-binding.lox
env GOTOOLCHAIN=go1.26.6 go run . -parser=llk -k=3 examples/resolving-and-binding.lox
```

## Scope boundary

Both code challenges are implemented. The remaining challenge is the prose one:
why is defining a function's own name before resolving its body safe, when
defining a variable before resolving its initializer is not? See the
[learning plan](11-learning-plan.md#challenges).

Chapter 12 will add binding kinds — methods, initializers, `this` — to
`functionType` and to the slots a scope hands out. The `binding` struct and the
`(distance, index)` pair are the two places that will grow.

That is how it turned out: `this` became a binding at slot 0 of a synthetic
class scope, precisely because a local environment here has no names to look up.
See [Chapter 12 — classes](12-classes.md#this-has-a-slot-not-a-name).
