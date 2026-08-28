# Chapter 12 learning plan — Classes

This plan follows
[Crafting Interpreters, Chapter 12](https://craftinginterpreters.com/classes.html)
through this repository's Go implementation.

## Learning outcomes

By the end, you should be able to:

1. Say why a property lookup is dynamic when a variable lookup is static, and
   what that costs.
2. Explain why `.` sits on the same precedence rung as `(`, and what breaks if
   it does not.
3. Trace `object.property = value` through both parsers without backtracking.
4. Count the scope distance for `this` from anywhere inside a class body.
5. Explain why this implementation gives `this` a slot when the book gives it a
   name.
6. Say why a method is bound when it is found rather than when it is called, and
   name a program that distinguishes the two.
7. Explain why a field shadows a method and not the reverse.
8. Say what makes `init` special and what does not.
9. Explain why the book's define-then-assign for a class name would fail here.
10. Say why the getter flag has to be recorded at parse time and cannot be
    recovered from the tree.

Use this command prefix in the current environment:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache
```

## Session 1 — Two lookups, one syntax

Read §12.1–12.4 of the chapter, then run
[`examples/classes.lox`](../examples/classes.lox) and read it top to bottom.

Predict each of these before running it, and say for each whether the failure is
static or runtime, and why it has to be that one:

```lox
print undefined_name;
class C {} print C().missing;
class C {} print C().missing = 1;
print "text".length;
var n = 1; n.field = 2;
```

Then read `VisitGetExpr` and `VisitSetExpr` in
[`resolver/resolver.go`](../resolver/resolver.go) and in
[`interpreter/interpreter.go`](../interpreter/interpreter.go).

Checkpoint: the resolver visits `e.Object` and stops. What would it even mean to
resolve `e.Name`, and what would have to be true about Lox for that to be
possible?

## Session 2 — The postfix rung

Read the `call` rule in [`docs/grammar.md`](grammar.md#chapter-12--classes),
then `call()` in [`parser/parser.go`](../parser/parser.go) and the `callTail`
productions in [`parser/llk/grammar.go`](../parser/llk/grammar.go).

Predict the tree for each, then check with `-print=ast`:

```lox
egg.scramble(3).with(cheese);
a.b.c;
a.b.c = d.e;
a.x = b.y = 1;
```

Now break it on purpose in the LL(k) grammar: move `act(mkGet)` to *after*
`nt(nCallTail)` in the `.` production and run
`go test ./parser/llk -run Classes`. Read the failure, then put it back.

Checkpoint: `mkGet` before the recursive tail gives `(. (. a b) c)` and after it
gives something else. Which is right, and how would you have known without
running it?

## Session 3 — Assignment without backtracking

Read `assignment()` in [`parser/parser.go`](../parser/parser.go) and `mkAssign`
in [`parser/llk/grammar.go`](../parser/llk/grammar.go).

Both parse the left side as an ordinary expression first and only then decide
which node to build. Work out what each of these produces, and which arm of the
switch handles it:

```lox
a = 1;
a.b = 1;
a.b() = 1;
(a) = 1;
a + b = 1;
```

Checkpoint: the grammar in the book says
`assignment → ( call "." )? IDENTIFIER "=" assignment`. Neither front end
implements that literally. What would it cost to implement it as written, in each
of the two parsers?

## Session 4 — Where `this` lives

Read §12.6, then `VisitClassStmt` in
[`resolver/resolver.go`](../resolver/resolver.go) and `bind` in
[`interpreter/function.go`](../interpreter/function.go), side by side.

For this program, list every resolver scope in order, then every runtime
environment, and pair them up:

```lox
class Thing {
  init() { this.name = "thing"; }
  makeReader() {
    var prefix = "read ";
    fun read() {
      var suffix = "!";
      return prefix + this.name + suffix;
    }
    return read;
  }
}
print Thing().makeReader()();
```

Then give the `(distance, index)` pair for: `this` inside `init`, `this` inside
`read`, `prefix` inside `read`, `suffix` inside `read`, and `read` in
`return read;`. Then say why `Thing` has no pair at all.

Now break the pairing on purpose. In `bind`, wrap the `Define` in a second
`NewEnvironment` so the runtime opens two scopes where the resolver opened one.
Run `go test ./interpreter -run This`. Note that it does not fail cleanly — it
panics with an index out of range, which is deliberate: read the comment on
`GetAt` in [`interpreter/environment.go`](../interpreter/environment.go) for why
that is the right outcome rather than an error return. Put it back.

Checkpoint: the book writes `closure.getAt(0, "this")` and this repo writes
`GetAt(0, 0)`. Which chapter made the second one necessary, and what exactly
would the book's version match against here?

## Session 5 — Bound at access, not at call

Read `get` in [`interpreter/instance.go`](../interpreter/instance.go).

Predict the output, then run it:

```lox
class Person {
  init(name) { this.name = name; }
  sayName() { print this.name; }
}
var jane = Person("Jane");
var method = jane.sayName;
jane.name = "Renamed";
method();

fun callTwice(f) { f(); f(); }
callTwice(Person("Bill").sayName);

class C { m() { return 1; } }
var c = C();
print c.m == c.m;
```

That last line is worth sitting with. Then change `get` to return the unbound
`method` instead of `method.bind(o)` and run
`go test ./interpreter -run 'Bound|This'`. It panics in `GetAt` for the same
reason session 4's sabotage did, and for an instructive reason: an unbound
method's closure is missing the scope its body was resolved against, so `this`
reads a slot that was never created.

Checkpoint: binding at access time creates a new `LoxFunction` on every property
read. What would binding at *call* time have to know that the call site cannot
see?

## Session 6 — init(), and what is not special about it

Read §12.7, then `Arity` and `Call` in
[`interpreter/class.go`](../interpreter/class.go), `isInitializer` and
`receiver` in [`interpreter/function.go`](../interpreter/function.go), and
`VisitReturnStmt` in [`resolver/resolver.go`](../resolver/resolver.go).

Predict each, including which of the three phases reports any failure:

```lox
class C { init() { return; } } print C();
class C { init() { return 1; } }
class C { init(a) {} } C();
class C {} C(1);
class C { initialize() { return 1; } } print C().initialize();
class C { init() { this.v = 1; } } var c = C(); print c.init() == c;
```

Checkpoint: `receiver()` reads slot 0 of the closure with no bounds check and no
fallback. What makes that safe today, and which single change in chapter 13
would make it unsafe?

## Session 7 — The class binding, and both front ends

Read `VisitClassStmt` in
[`interpreter/interpreter.go`](../interpreter/interpreter.go) and the comment
about the book's two-step.

Try to write the two-step version: `Define(name, nil)`, build the class, then
`Assign(name, class)`. Run `{ class C { m() { return C(); } } print C().m(); }`
and explain the error you get from
[`interpreter/environment.go`](../interpreter/environment.go). Then revert.

Confirm the pipeline end to end:

```sh
env GOTOOLCHAIN=go1.26.6 go run . examples/classes.lox
env GOTOOLCHAIN=go1.26.6 go run . -parser=llk -k=1 examples/classes.lox
env GOTOOLCHAIN=go1.26.6 go run . -parser=llk -k=3 examples/classes.lox
env GOTOOLCHAIN=go1.26.6 go run . -print=ast examples/classes.lox
env GOTOOLCHAIN=go1.26.6 go run . -print=rpn examples/classes.lox
```

The three evaluated outputs must be identical. Then use the REPL: declare a
class on one line, instantiate it on the next, call a method on a third, and
finally type a bare `this`.

Finish with:

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache go test ./...
```

Final checkpoint: `class Cake {}` at the REPL needed one line of change in
`main.go`, and a bare `this` at the REPL is a *runtime* error rather than the
static one a script gets. Both come from the same design decision. Which one?

## Challenges

1. **Implemented** — class methods, using `class` as a prefix inside a class
   body:

   ```lox
   class Math {
     class square(n) { return n * n; }
   }
   print Math.square(3); // 9
   ```

   The book's hint is metaclasses, and it does not translate to Go — read
   [the chapter notes](12-classes.md#challenge-1--class-methods-without-metaclasses)
   for the two reasons and what replaced it.

   Before reading that: predict what each of these prints, and which are errors.

   ```lox
   class Math { class square(n) { return n * n; } }
   print Math.square(3);
   print Math.square;
   var f = Math.square; print f(4);
   Math.tag = "arith"; print Math.tag;
   class C { m() { return 1; } } print C.m;
   class C { class m() { return 1; } } print C().m;
   class C { class init() { return 1; } } print C.init(); print C();
   ```

   Then read `lookUpProperty` in
   [`interpreter/instance.go`](../interpreter/instance.go) and `get` in
   [`interpreter/class.go`](../interpreter/class.go).

   Checkpoint: `lookUpProperty` takes `findMethod` as a function rather than a
   map. Nothing today needs that. What in chapter 13 will?

2. **Implemented** — getter methods, whose body runs on property access:

   ```lox
   class Circle {
     init(radius) { this.radius = radius; }
     area { return 3.141592653 * this.radius * this.radius; }
   }
   print Circle(4).area;
   ```

   Read [the chapter notes](12-classes.md#challenge-2--getters) for why the flag
   has to live on the AST and what signature had to change.

   Before that: predict each of these, and say which phase reports any failure.

   ```lox
   class C { v { return 1; } } print C().v;
   class C { v { return 1; } } print C().v();
   class C { v { return 1; } } var c = C(); c.v = 2; print c.v;
   class C { g { return "g"; } m() { return "m"; } } print C().g; print C().m();
   class C { init { this.a = 1; } }
   class C { class init { return 1; } } print C.init;
   fun f { return 1; }
   ```

   Then read `lookUpProperty` in
   [`interpreter/instance.go`](../interpreter/instance.go) again, and note that
   the getter call happens *inside* the fields-then-methods rule.

   Checkpoint: a getter is invoked during the property read, so
   `TestGetterErrorsPropagate` asserts the error surfaces at the read. What
   would have to be true for it to surface anywhere else?

3. **Answered in prose** — how open should field access be? See
   [the chapter notes](12-classes.md#challenge-3--how-open-should-fields-be).

Two extensions neither challenge asks for, if you want more:

- Report a method that shadows a field assigned in `init()`, or the reverse, as a
  warning. It is the one place the fields-before-methods rule can silently
  surprise, and the information is all in `ast.Class` plus the initializer body.
- Pre-size an instance's field map from the properties `init()` assigns to. It
  needs the count on the `ast.Class` node, which means touching the generator —
  the same shape as chapter 11's unimplemented extension for call frames.
