# Chapter 13 — Inheritance, in Go

Implementation of [Inheritance](https://craftinginterpreters.com/inheritance.html)
for this repository's tree-walking interpreter. This is the last chapter of
part II: with it, jlox is a complete language.

> Status: implemented. Superclass declarations, inherited methods, `super`, and
> the four new diagnostics all work, on both parser front ends and at every
> supported lookahead. Inheritance also composes with chapter 12's two
> challenges — class methods and getters are inherited, and `super` reaches
> both. The chapter's own challenges are open; see
> [Scope boundary](#scope-boundary).

For a guided walkthrough, follow the [Chapter 13 learning plan](13-learning-plan.md).

## What the chapter adds

One AST node and one field, declared in
[`cmd/genast/main.go`](../cmd/genast/main.go):

| Node | Shape | What it is |
|---|---|---|
| `Super` | `Keyword`, `Method` | `super.method` |
| `Class` | gains `Superclass *Variable` | nil for a root class |

`Superclass` is `*Variable` rather than `Expr` because the grammar allows
nothing else there. A superclass is named, never computed, so `class A < f() {}`
fails at the `(`. It is still a genuine *variable use*, though — it resolves
like one and obeys scope, which is what makes a class declared in a block able
to inherit from one declared outside it.

Inheritance itself is one pointer and two fall-throughs in
[`interpreter/class.go`](../interpreter/class.go): `findMethod` checks the
class's own map and then recurses into `superclass`. Own methods first *is*
overriding. Nothing is copied, so a method is found through the chain at call
time — a subclass built before its superclass gained a method still finds it.

Because `init` is found by that same walk, a subclass that declares no
initializer inherits its superclass's constructor, arity included.

## `super` is two lookups, and only one of them is dynamic

The mechanism the chapter exists to explain:

```lox
class A { speak() { return "A"; } }
class B < A { speak() { return "B then " + super.speak(); } }
class C < B { speak() { return "C then " + super.speak(); } }
print C().speak(); // C then B then A
```

If `super` meant "the superclass of this object's class", `B.speak` called on a
`C` would look in `B`'s superclass — no, worse: it would have to ask the
*object*, get `C`, and call `B.speak` again, forever. So `super` cannot depend
on the receiver.

It depends on **where the method was written**. `super` in `B.speak` always
means `A`, whatever object `speak` is running on. That is a lexical fact, fixed
when the class is declared — which is exactly what a closure captures. So the
interpreter puts the superclass in an environment that the methods close over,
and `super` becomes an ordinary captured variable.

The receiver, meanwhile, is still dynamic: `super.speak()` runs `A.speak` bound
to *this* object. Two lookups, one static and one dynamic, in one expression.

## Two synthetic scopes, and the invariant that keeps them honest

Chapter 12 gave `this` a slot rather than a name, because a local `Environment`
here holds no names — it is a `[]any` addressed by the `(distance, index)` pair
the resolver computed. `super` needs the same treatment, and now there are two
scopes to keep aligned:

```
resolver.VisitClassStmt                interpreter.VisitClassStmt / bind
  resolve the superclass name            evaluate it, check it is a class
  beginScope()   super @ slot 0    ⇔     NewEnvironment + Define("super", …)
  beginScope()   this  @ slot 0    ⇔     bind: NewEnvironment + Define("this", …)
    resolveFunction(method)        ⇔     Call: NewEnvironment for params+body
```

**One `beginScope`, one `NewEnvironment`** — the chapter 11 invariant, now
holding across three levels. Both sides create the `super` scope only when there
is a superclass. A scope one side makes and the other does not is not a missing
binding; it is every distance inside the class body wrong by one, which shows up
as a `this` that is silently a `LoxClass`, or a panic in `GetAt`.

From a method body that means:

| name | distance | index |
|---|---|---|
| a parameter or local | 0 | its slot |
| `this` | 1 | 0 |
| `super` | 2 | 0 |
| the class's own name | 3 | 0-based slot in the enclosing scope |

Without a superclass the last two shift in by one, which is why nothing may
create the `super` scope unconditionally.

`VisitSuperExpr` reads both operands by slot, and the second one is the trick
worth noticing:

```go
superclass := i.environment.GetAt(at.distance, at.index)
receiver   := i.environment.GetAt(at.distance-1, 0)
```

The receiver is one hop *nearer* than `super`, because the `this` scope nests
directly inside the `super` scope and holds exactly one binding. The book spells
the same thing `getAt(distance - 1, "this")`; here it is index 0 by
construction.

## What it composes with

Chapter 12's two challenges were not in the book, so nothing in the chapter says
what inheritance should do with them. Both fall out:

- **Class methods are inherited.** `findClassMethod` walks the chain the same
  way `findMethod` does. Keeping the two walks separate is what still makes
  `C.instanceMethod` an error.
- **`super` works inside a class method.** `VisitSuperExpr` picks which half of
  the superclass to search by looking at what the receiver *is* — an instance in
  a method, the class itself in a class method. That is not a special case so
  much as `this` already meaning the right thing in both.
- **Getters inherit, and `super.area` still runs the body.** Handing back the
  function instead would make `super.area` and `this.area` different kinds of
  expression.

Static *fields* are deliberately not inherited. They live in the class's own
`fields` map, which neither walk touches: inheriting a mutable slot would give
two classes one variable, which is a different feature from inheriting
behaviour.

## New diagnostics

Static, refused before anything runs:

```
A class can't inherit from itself.                a superclass naming its own class
Can't use 'super' outside of a class.             a Super with no enclosing class
Can't use 'super' in a class with no superclass.  a Super in a base class
```

The last two are separate messages because the mistakes are different: one is a
misplaced keyword, the other a class that forgot its `< Name`. Reporting either
as the other sends the reader to the wrong line.

Self-inheritance is static rather than runtime because it cannot be anything
else — for the name to refer to the class, the class would already have to
exist. It is a lexeme comparison, so it also catches the shadowing case
`class A {} { class A < A {} }`, where the inner `A` really does resolve to
itself.

Runtime, because what a name holds is not something the text decides:

```
Superclass must be a class.       the superclass name held a non-class
Undefined property 'x'.           no class in the chain declares it
```

`super.x` never consults fields — an instance's fields belong to the instance,
not to a class in its chain — which is why `VisitSuperExpr` does not go through
`lookUpProperty`.

## One crash this chapter had to fix

`VisitSuperExpr` originally read `i.locals[e]` without checking whether the
lookup succeeded. In a script that is safe: the resolver refuses every `super`
that would be unresolved. But a bare REPL line skips resolution entirely — an
expression declares nothing, so there are no scopes to resolve — and
`super.method` typed at the prompt therefore reached the interpreter unresolved,
took the zero `slot{0, 0}`, and read slot 0 of the globals. The globals have no
slots. It panicked and took the process with it.

It now reports `Undefined variable 'super'.`, the same way an unresolved `this`
already did, and `TestBareSuperAtTheReplIsARuntimeError` pins it. The general
shape is worth remembering: the REPL's expression path is the one place a tree
runs without having been resolved, so anything that trusts the resolver has to
say what it does when the resolver never ran.

## Verification

The suite covers both front ends at `k = 1..3`, the superclass clause and its
parse errors, `super` as a primary rather than a property suffix, inherited
methods and inherited `init`, overriding, a three-deep chain, `super` fixed at
declaration rather than at call, `super` through closures nested in methods,
inherited class methods and getters, static fields *not* inheriting, every new
static and runtime error, the unused-name exemptions for `super` and the
superclass name, and the bare-`super` REPL case.

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache go test ./...

env GOTOOLCHAIN=go1.26.6 go run . examples/inheritance.lox
env GOTOOLCHAIN=go1.26.6 go run . -parser=llk -k=3 examples/inheritance.lox
env GOTOOLCHAIN=go1.26.6 go run . -print=ast examples/inheritance.lox
```

## Scope boundary

All three challenges are open, and they are a different kind of exercise from
chapter 12's — each is a language design change rather than a feature to add.
There are sketches in the [learning plan](13-learning-plan.md#challenges).

This finishes the tree-walking interpreter. What it does *not* finish is the
performance story, and the two chapters just gone are where that shows: every
property read is a hash lookup, every method access allocates a fresh
`LoxFunction` to bind the receiver, and every call allocates an environment.
Chapter 11's slot work removed exactly one of those costs, for locals only.
Part III rebuilds the whole thing as a bytecode VM, and those three allocations
are most of the reason.
