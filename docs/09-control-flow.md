# Chapter 9 — Control Flow, in Go

Implementation of [Control Flow](https://craftinginterpreters.com/control-flow.html)
for this repository's recursive-descent and table-driven LL(k) front ends.

> Status: implemented. Programs support `if`/`else`, short-circuiting `and`
> and `or`, `while`, and C-style `for`. Both parsers produce the same trees for
> every supported lookahead (`k = 1..3`).

For a guided walkthrough, follow the [Chapter 9 learning plan](09-learning-plan.md).

## What landed

- `ast.Logical` represents `and` and `or` separately from eager binary
  operators.
- `ast.If` stores the condition and both statement branches. A missing `else`
  is a nil `ElseBranch`.
- `ast.While` stores the loop condition and body.
- `for` has no AST node. Both parsers lower it to blocks, a `While`, and an
  optional increment expression.
- The CLI recognizes control statements as statements even when a one-line
  REPL input contains no semicolon.

The scanner already recognized every chapter 9 keyword, so its production code
did not change.

## Short-circuit evaluation

Logical expressions cannot use `Binary`: a binary expression always evaluates
both children before applying its operator, while `and` and `or` may skip the
right child.

The interpreter evaluates the left operand first:

- `left or right` returns `left` immediately when it is truthy.
- `left and right` returns `left` immediately when it is falsey.
- Otherwise it evaluates and returns `right`.

The result is an operand value, not a coerced boolean. Existing Lox truthiness
still applies: only `false` and `nil` are falsey.

## Branches and loops

`If` evaluates exactly one branch. An absent `else` does nothing when the
condition is falsey. Parsing consumes `else` before the current `ifStatement`
returns, which resolves the dangling-`else` case in favor of the nearest `if`.

`While` reevaluates its condition before every iteration. The interpreter does
not need a separate `for` visitor because parsing rewrites:

```lox
for (var i = 0; i < 3; i = i + 1) print i;
```

into the equivalent tree for:

```lox
{
  var i = 0;
  while (i < 3) {
    print i;
    i = i + 1;
  }
}
```

The outer block gives a variable initializer the correct loop-local scope. A
missing condition becomes `true`; missing initializer and increment clauses
simply omit their corresponding statements.

## Two parser front ends

Recursive descent adds `or()` and `and()` to its precedence ladder and parses
compound statements with one method per grammar rule.

The LL(k) grammar adds factored `or` and `and` tail rules with semantic actions
that fold left. `if`, `while`, and `for` use the parser's existing statement
orchestration layer. This avoids adding the ambiguous dangling-`else` grammar
to a table builder whose job is to reject prediction conflicts.

For `k > 1`, a nested condition parse could otherwise see tokens from the
following statement in its lookahead window. The LL(k) parser therefore masks
tokens beyond the condition's closing `)` or `;` as EOF for that nested run.
Parser parity tests exercise this behavior at `k = 1`, `2`, and `3`.

## Verification

The suite covers logical precedence and associativity, short-circuit side
effects, returned operand values, nearest-`if` binding, truthiness, bounded
loops, all optional `for` clauses, initializer scope, parser parity, generated
AST freshness, and REPL classification.

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache \
  go generate ./...

env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache \
  go test ./...
```

## Chapter challenges

The chapter's optional challenges are not implemented. In particular, there
is no `break` or `continue` statement yet.
