# Chapter 11 — Resolving and binding, in Go

Implementation of
[Resolving and Binding](https://craftinginterpreters.com/resolving-and-binding.html)
for this repository's tree-walking interpreter.

> Status: implemented. A static resolution pass runs between parsing and
> execution. Every local variable use is bound to a declaration by scope
> distance, closures no longer observe declarations that appear after them, and
> three static errors are reported before the program runs. The grammar and both
> parser front ends are unchanged.

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

## Reading a resolved variable

The interpreter keeps the answers in a side table, `locals`, and asks it before
touching an environment. A resolved use hops a known number of environments; an
unresolved one is global by definition, and only that case can still fail at
runtime with `Undefined variable`.

`Environment.GetAt` and `AssignAt` have no not-found case. That absence is the
payoff of the pass rather than an oversight: the resolver proved the name lives
in that exact scope, so there is nothing to search for and no error to report.
`Get` and `Assign` remain, used only for globals.

The table is keyed by the syntax node itself. Every `ast` node is used through a
pointer and Go compares pointer keys by identity, which is the same guarantee
the book gets from Java's `IdentityHashMap`: two textually identical uses in
different places stay separate entries. The cost is an invariant — nothing may
copy a node by value or rebuild the tree between resolution and execution, or
the recorded entry becomes unreachable and the use silently falls back to
globals.

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
assignment at a distance, front-end parity at `k = 1..3`, REPL survival after a
static error, and what an unresolved tree does — locals fall back to globals,
which is why resolution is a required stage and not an optimization.

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache \
  go test ./...

env GOTOOLCHAIN=go1.26.6 go run . examples/resolving-and-binding.lox
env GOTOOLCHAIN=go1.26.6 go run . -parser=llk -k=3 examples/resolving-and-binding.lox
```

## Scope boundary

The chapter's challenges are not implemented: reporting unused local variables,
and replacing the map lookups with array slot indices. Both are additive to this
pass. The slot-index version is better attempted after Chapter 12, which adds
binding kinds that would otherwise force it to be rewritten.
