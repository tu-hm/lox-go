# Chapter 13 learning plan — Inheritance

This plan follows
[Crafting Interpreters, Chapter 13](https://craftinginterpreters.com/inheritance.html)
through this repository's Go implementation. It is the last chapter of part II:
at the end of it, the tree-walking interpreter is a complete language.

## Learning outcomes

By the end, you should be able to:

1. Explain why overriding needs no mechanism beyond "check your own map first".
2. Say why a subclass with no `init` still constructs correctly, and what it
   would take to break that.
3. Explain why `super` cannot mean "the superclass of this object's class",
   using a three-deep chain as the argument.
4. Say which half of `super.method()` is lexical and which is dynamic.
5. Count the distance to `this`, `super`, and the class's own name from inside a
   method, with and without a superclass.
6. Explain why the `super` scope must be conditional in *both* passes.
7. Say why self-inheritance is a static error and a bad superclass is a runtime
   one.
8. Explain why `super.x` never finds a field.

Use this command prefix in the current environment:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache
```

## Session 1 — Inheriting is one pointer

Read §13.1–13.2, then run [`examples/inheritance.lox`](../examples/inheritance.lox)
and read `findMethod` in [`interpreter/class.go`](../interpreter/class.go).

Predict each, and say which line of `findMethod` decides it:

```lox
class A { m() { return "A.m"; } }
class B < A {} print B().m();
class B2 < A { m() { return "B2.m"; } } print B2().m();
class A2 { init(x) { this.x = x; } } class B3 < A2 {} print B3(7).x;
class B4 < A2 { init() { this.x = 0; } } print B4().x;
class B5 < A2 {} B5();
```

Then break it: make `findMethod` return `c.methods[name]` without walking the
chain, and run `go test ./interpreter -run 'Super|Inherit'`. Put it back.

Checkpoint: nothing is copied into the subclass at declaration time. Write a
program whose output would differ if it were.

## Session 2 — Why `super` cannot ask the object

Read §13.3, and this program, before running it:

```lox
class A { speak() { return "A"; } }
class B < A { speak() { return "B then " + super.speak(); } }
class C < B { speak() { return "C then " + super.speak(); } }
print C().speak();
```

Suppose `super` meant "the superclass of the receiver's class". Trace
`C().speak()` by hand under that rule and say exactly where it goes wrong — not
"it gives the wrong answer", but which call repeats.

Then read `VisitSuperExpr` in
[`interpreter/interpreter.go`](../interpreter/interpreter.go) and identify the
two lookups. One is fixed when the class is declared; the other is not.

Checkpoint: `super.speak()` runs a method found on `A` against an object whose
class is `C`. Which of those two facts is the closure responsible for?

## Session 3 — Two synthetic scopes

Read `VisitClassStmt` in [`resolver/resolver.go`](../resolver/resolver.go) and
in the interpreter, side by side, and pair every `beginScope` with the
`NewEnvironment` it predicts. Remember `bind` in
[`interpreter/function.go`](../interpreter/function.go) is one of them.

For this program, give the `(distance, index)` pair for every name marked `<--`:

```lox
{
  class Base { tag() { return "base"; } }
  class Sub < Base {
    make() { return Sub(); }          // Sub   <--
    describe(sep) {                   // sep   <--
      var mine = this.tag();          // this  <--
      return mine + sep + super.tag(); // mine, super <--
    }
  }
  print Sub().describe("/");
}
```

Then work out the same table for a `Base` with no superclass, and say which
entries move.

Now break the pairing on purpose. In the interpreter's `VisitClassStmt`, build
the `super` environment unconditionally:

```go
methodEnvironment := NewEnvironment(i.environment)
methodEnvironment.Define("super", superclass)
```

Run `go test ./interpreter -run 'Class|This|Super'`. The failure is
`Operands must be two numbers or two strings.` — which is worth sitting with,
because nothing about it says "scope". Work out how a spare environment turns
into a type error before reading on. Put it back.

Checkpoint: the sabotage above adds a scope the resolver did not. Would adding
one the *interpreter* did not be easier or harder to diagnose?

## Session 4 — The receiver is one hop nearer

Read these two lines in `VisitSuperExpr`:

```go
superclass := i.environment.GetAt(at.distance, at.index)
receiver   := i.environment.GetAt(at.distance-1, 0)
```

Say why `distance-1` is always the `this` scope, and why index 0 needs no
lookup. Then say what would have to change for `at.distance-1` to be negative,
and whether the resolver can produce that.

Break it: bind the found method to the superclass instead of the receiver —
`method.bind(superclass)` — and run `go test ./interpreter -run 'Super|Inherit'`.
Note that the three-deep chain in session 2 *still prints correctly* under this
bug. Work out why, and what kind of method it takes to expose it. Put it back.

Checkpoint: the book writes `getAt(distance - 1, "this")` and this repo writes
`GetAt(at.distance-1, 0)`. What makes the index a constant here?

## Session 5 — Three static errors and two runtime ones

Read `VisitSuperExpr` and the superclass branch of `VisitClassStmt` in the
resolver, then the superclass branch in the interpreter.

Predict each, including which phase reports it:

```lox
class C < C {}
class A {} { class A < A {} print A; }
print super.method;
class C { m() { return super.m(); } }
var NotAClass = 1; class C < NotAClass {}
class A {} class B < A {} print B().nope;
class A { m() { return 1; } } class B < A { m() { return super.nope(); } } B().m();
```

Two of those are worth arguing about. Self-inheritance is checked by comparing
lexemes, so it fires on the shadowing case even though a human might read
`class A < A` inside a block as "inherit from the outer A". And
`Superclass must be a class.` has to be a runtime error while
`A class can't inherit from itself.` cannot be.

Checkpoint: state the rule that decides which of those two phases a superclass
check belongs to.

## Session 6 — What inheritance composes with

Chapter 12's class methods and getters are not in the book, so the chapter says
nothing about how they should inherit. Read `findClassMethod` in
[`interpreter/class.go`](../interpreter/class.go) and the `find` selection in
`VisitSuperExpr`, then predict:

```lox
class Shape { class describe() { return "shape"; } sides { return 0; } }
class Square < Shape {
  class describe() { return "square < " + super.describe(); }
  sides { return 4 + super.sides; }
}
print Square.describe();
print Square().sides;
print Square.sides;
print Square().describe;
Shape.tag = "s"; print Square.tag;
```

The last one is a deliberate design decision rather than an oversight.

Checkpoint: `VisitSuperExpr` chooses between `findMethod` and `findClassMethod`
by asking what the *receiver* is, not what the expression looks like. Why is
that the same question as "which half of the class am I in?"

## Session 7 — Both front ends, and the one path with no resolver

```sh
env GOTOOLCHAIN=go1.26.6 go run . examples/inheritance.lox
env GOTOOLCHAIN=go1.26.6 go run . -parser=llk -k=1 examples/inheritance.lox
env GOTOOLCHAIN=go1.26.6 go run . -parser=llk -k=3 examples/inheritance.lox
env GOTOOLCHAIN=go1.26.6 go run . -print=ast examples/inheritance.lox
```

The three evaluated outputs must be identical. Read the `super` production in
[`parser/llk/grammar.go`](../parser/llk/grammar.go) and say why three terminals
in one production keeps it LL(1), and why the superclass clause is *not* in the
table at all.

Then the REPL. Declare a superclass on one line and a subclass on the next, call
an inherited method on a third — and then type a bare `super.method`.

That last one used to panic and take the process with it. Read the guard at the
top of `VisitSuperExpr` and
`TestBareSuperAtTheReplIsARuntimeError` in [`main_test.go`](../main_test.go).

Finish with:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache go test ./...
```

Final checkpoint: the REPL's expression path is the only place a tree runs
without having been resolved. List everything in the interpreter that trusts the
resolver, and say what each one does when the resolver never ran.

## Challenges

All three are open, and all three are a different kind of exercise from chapter
12's: each changes the language rather than adding to it. Sketches, not
instructions.

1. **Another way to reuse code** — multiple inheritance, mixins, traits, or
   extension methods; pick one and argue for it first.

   The shape of the work is mostly in `findMethod`. Single inheritance is a
   *walk up a chain*, which is why it is one recursive call; anything else is a
   *search over a graph*, and the moment there are two parents you owe an answer
   to the diamond question — if `B` and `C` both override `A.m`, what does `D`
   get? Python answers with C3 linearisation, Ruby by flattening mixins into the
   chain at include time, Go by refusing to answer and making the ambiguity a
   compile error.

   Flattening is the cheapest to fit here: resolve the linear order once in
   `VisitClassStmt` and keep `superclass` a single pointer, so `super` and the
   distance arithmetic in `VisitSuperExpr` are untouched. Note what that gives
   up — the order is fixed when the class is declared, so a method added to a
   mixin afterwards is not seen, which breaks the property session 1 pins.

2. **BETA-style dispatch: `inner` instead of `super`** — start the lookup at the
   *top* of the chain and work down, with a superclass method deciding where the
   subclass gets to refine it.

   This inverts the chapter rather than extending it, so expect to delete as
   much as you add: `findMethod` starts at the root, and `inner` is the
   *subclass's* version of the current method — the reverse of `super`. The
   interesting part is that `inner` has no static answer. `super` is lexical, so
   it is a captured variable; `inner` depends on the receiver's class, so it
   cannot be. Work out where it has to live instead before writing any code.

   A calibration: what should `inner` do when there is no subclass version?

3. **A feature you proposed** — the book means one you named in an earlier
   chapter's challenge. Two from this repo, if you would rather not go looking:
   `break`/`continue` from chapter 9, and the reachability-based unused-local
   analysis left open in
   [the chapter 11 plan](11-learning-plan.md#challenges).

One extension none of the challenges asks for, and the most useful thing to do
before part III: measure. Every property read is a hash lookup, every method
access allocates a `LoxFunction` to bind the receiver, and every call allocates
an environment. Add a benchmark over a method-heavy program next to the ones in
[`interpreter/bench_test.go`](../interpreter/bench_test.go), and keep the number.
Part III's whole argument is that it can be beaten by an order of magnitude, and
that claim is more interesting when you measured the starting point yourself.
