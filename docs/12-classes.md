# Chapter 12 — Classes, in Go

Implementation of [Classes](https://craftinginterpreters.com/classes.html) for
this repository's tree-walking interpreter.

> Status: implemented, including both code challenges. Class declarations,
> instances, property get and set, methods, `this`, `init()`, `class`-prefixed
> class methods, and getters all work, on both parser front ends and at every
> supported lookahead. Three static errors and three runtime errors are new.

For a guided walkthrough, follow the [Chapter 12 learning plan](12-learning-plan.md).

## What the chapter adds

Four AST nodes, declared in [`cmd/genast/main.go`](../cmd/genast/main.go) and
generated into [`ast/expr.go`](../ast/expr.go) and
[`ast/stmt.go`](../ast/stmt.go):

| Node | Shape | What it is |
|---|---|---|
| `Class` | `Name`, `Methods []*Function` | a class declaration |
| `Get` | `Object`, `Name` | `object.property` |
| `Set` | `Object`, `Name`, `Value` | `object.property = value` |
| `This` | `Keyword` | the receiver of the enclosing method |

Two runtime types, in [`interpreter/class.go`](../interpreter/class.go) and
[`interpreter/instance.go`](../interpreter/instance.go): `LoxClass`, which is
`Callable` because in Lox the class object *is* the constructor, and
`LoxInstance`, which holds a class pointer and a field map.

`Methods` is `[]*Function` rather than `[]Stmt`. Every consumer wants the
concrete node, and a method is never executed as a statement — nothing calls
`Accept` on one through the `Stmt` interface.

## Variables are static, properties are not

The dividing line this chapter draws is the one worth remembering, because it is
the reason `Get` and `Set` are so much simpler than `Variable` and `Assign`.

The resolver binds a variable use to a declaration, because the source lists
declarations and the answer is fixed by the text. It cannot do the same for a
property: which properties an object has depends on which lines *ran*. There is
no declaration to point at, because assignment is what brings a field into
existence.

So `VisitGetExpr` and `VisitSetExpr` in
[`resolver/resolver.go`](../resolver/resolver.go) resolve the object and stop.
The property name is a terminal in the grammar and a `token.Token` in the tree,
and no visitor may recurse into it. At runtime it is a map lookup that can fail,
which is why `Undefined property` is a runtime error while `Undefined variable`
for a local is impossible by construction.

## `this` has a slot, not a name

This is where the chapter departs most from the book, and it is a consequence of
chapter 11's third challenge.

The book binds the receiver by name — `environment.define("this", instance)` —
and reads it back with `closure.getAt(0, "this")`. That does not work here.
Since chapter 11, a local `Environment` in
[`interpreter/environment.go`](../interpreter/environment.go) has no names at
all: it is a `[]any` addressed by the `(distance, index)` pair the resolver
computed. A name lookup in a local scope matches nothing.

So `this` becomes a real resolver binding:

```
VisitClassStmt         beginScope()
                       declare(this, bindingThis)   ← slot 0 of the class scope
                       resolveFunction(method, …)   ← beginScope() for params+body
                       endScope()

LoxFunction.bind       NewEnvironment(f.closure)
                       Define("this", instance)     ← appends: slot 0
LoxFunction.Call       NewEnvironment(f.closure)    ← params+body
```

Read the two columns together and the invariant chapter 11 established still
holds: **one `beginScope` in the resolver, one `NewEnvironment` at runtime**. The
class scope pairs with the environment `bind` creates. From a method body, `this`
is therefore at distance 1, slot 0 — and the class's own name is at distance 2,
one scope further out, which is what lets a method name the class it belongs to.

The payoff is that `VisitThisExpr` in the interpreter is one line: `this` is read
through `lookUpVariable`, exactly like every other local, because by execution
time it *is* one.

Two details fall out of this:

- `bindingThis` is exempt from the unused-local warning, next to
  `bindingParameter`. Without the exemption, every class whose methods ignore
  `this` would warn about a name the programmer never wrote.
- `LoxFunction.receiver()` reads `f.closure.GetAt(0, 0)`. That is safe because
  an initializer is only ever reached through `bind`, so its closure *is* the
  environment `bind` built, and that environment holds exactly one slot.

## Binding at access time, not call time

`LoxInstance.get` returns `method.bind(o)` — a new `LoxFunction` closed over one
extra scope. The binding happens when the method is *found*, which is what makes
this work:

```lox
var method = jane.sayName;
method();                   // Jane
```

Nothing at that call site says `jane`. The receiver came along inside the
closure. It is the same mechanism as any other captured variable, which is the
elegance of the design: `this` needed no new machinery in the environment at
all, only a scope nobody wrote.

Lookup order is fields, then methods, so a field shadows a method of the same
name. That is deliberate rather than incidental: a field holding a function has
to be callable exactly like a method, so the two share one namespace.

## One `Define`, not the book's two-step

The book's `visitClassStmt` defines the name as `nil`, builds the `LoxClass`,
then assigns over it. That two-step does not survive slot-addressed locals:
`Environment.Assign` matches by name, a local environment holds no names, so the
assignment would walk straight past the class's own scope and fail in the
globals.

One `Define` after building the class is correct here, and it keeps the
resolver's slot numbering intact, because a class declaration performs exactly
one `Define` in its scope. Method bodies still see the name — they only read it
when called, long after the binding completed. Chapter 13's superclass scope
will need to revisit this.

## init()

`init` is not a keyword. It is an identifier the *interpreter* treats specially,
which is why `class C { initialize() {} }` is a plain method. Two rules make it a
constructor:

- `LoxClass.Arity()` is the initializer's arity, because that is what the call
  passes its arguments to. A class with no `init()` takes none.
- `LoxFunction.isInitializer` forces a call to evaluate to the receiver — both
  when the body falls off the end and when it exits through a bare `return;`.
  That holds even when `init()` is called directly on an existing instance.

The resolver refuses `return <value>;` inside an initializer. A bare `return;`
stays legal: it is an early exit, and construction still has only one answer to
give.

## New diagnostics

Static, refused before anything runs:

```
Can't use 'this' outside of a class.            This with no enclosing class
Can't return a value from an initializer.       returnStmt with a value inside init()
Can't declare an initializer as a getter.       init declared without a parameter list
```

A warning, because it cannot make a program behave wrongly:

```
Local class 'C' is never used.                  a class binding nothing reads
```

Method names are deliberately *not* bindings — a method is reached through its
class's method map, never through a scope — so there is no unused-method
warning to give.

Runtime, because none of them is a question the text can answer:

```
Only instances have properties.                 a Get on a non-instance
Only instances have fields.                     a Set on a non-instance
Undefined property 'x'.                         no such field and no such method
```

Since challenge 1, a class is a property owner too, so the first two apply only
to values that are neither — a number, a string, a boolean, `nil`, or a plain
function.

`VisitSetExpr` evaluates the object before the value, matching source order, so
a bad target is reported before the right-hand side runs at all.

## One CLI change

`isBareExpression` in [`main.go`](../main.go) gained `token.CLASS`. A class
declaration carries no semicolon, so without the keyword in that list a REPL
line reading `class Cake {}` would be parsed as an expression and rejected.

The flip side is worth knowing: a bare REPL expression skips resolution
entirely, so `this` typed at the prompt is *not* caught as the static error it
would be in a script. It falls through to the global lookup and fails there.
`TestBareThisAtTheReplIsARuntimeError` pins that.

## Challenge 1 — class methods, without metaclasses

```lox
class Math {
  class square(n) { return n * n; }
  class cube(n) { return n * this.square(n); }
}
print Math.cube(3); // 27
```

A leading `class` inside a class body puts the method on the class. Reusing the
keyword costs no lookahead: `class` cannot begin a method name, so one token
settles which list the method joins, and both front ends handle it in the same
orchestration layer that already handled the body — the LL(k) table is
untouched.

**The book's hint does not translate.** It suggests making `LoxClass` extend
`LoxInstance`, so a class becomes an instance of a metaclass and `Math.square`
needs no new lookup path at all. In Go, embedding is not subtyping, and it fails
in two separate ways:

- `object.(*LoxInstance)` does not match a `*LoxClass` that embeds one. Every
  type switch in the interpreter would silently stop working for classes.
- Worse, a promoted method receives the *embedded* value. Inside
  `LoxInstance.get`, the receiver is the inner `*LoxInstance`, not the outer
  `*LoxClass` — so `method.bind(o)` would bind `this` to a hidden object the
  program can never otherwise reach.

The second one is the real obstacle: it needs a back-pointer from the embedded
instance to whatever embeds it, and at that point the embedding has stopped
saving anything.

What is here instead: `LoxClass` carries its own `classMethods` and `fields`
maps and implements `get`/`set` directly, and the fields-then-methods rule lives
once in `lookUpProperty`, shared by both types. `VisitGetExpr` and
`VisitSetExpr` switch on a `propertyOwner` interface rather than on
`*LoxInstance`, which is the one line of the interpreter that had to change.

Two things fall out for free:

- **Static fields.** `Math.description = "arithmetic"` works, because a class is
  a property owner and assignment is what creates a field. That was the point of
  the metaclass hint, and it survives without it.
- **`this` in a class method is the class.** `bind` takes `any` now, and nothing
  in it inspects the receiver — `this` is just a value in slot 0. So
  `this.square(n)` inside a class method reaches a sibling class method exactly
  as an instance method reaches its siblings, because the resolver puts class
  methods in the *same* class scope.

Two behaviours worth stating, since neither is arbitrary:

- The namespaces are separate in both directions. `C.instanceMethod` and
  `C().classMethod` are both `Undefined property`, because `findMethod` and
  `findClassMethod` read different maps. This changed one existing message:
  `C.m` used to be `Only instances have properties.`, and now the class *is* a
  valid owner that simply lacks the property.
- A class method named `init` is not a constructor. Construction happens to
  instances, and a class method never receives one, so `isInitializer` is false
  regardless of the name and returning a value from it is legal.

`findMethod` is passed to `lookUpProperty` as a function rather than a map for
chapter 13's benefit: walking a superclass chain then changes one closure, not
the shared rule.

## Challenge 2 — getters

```lox
class Circle {
  init(radius) { this.radius = radius; }
  diameter { return this.radius * 2; }
}
print Circle(4).diameter; // 8
```

Drop the parameter list and the body runs on property *access*. What that buys
is the thing challenge 3 argues about from the other side: a stored field can
become a computed one later without a single caller changing.

**The flag has to live on the AST.** A getter and a zero-parameter method are
otherwise identical in the tree — both have no `Params` — so `ast.Function`
gained `IsGetter bool`. That mirrors how `LoxFunction` already carries
`isInitializer`: a property of how the thing was declared, not of the call.

The optional parameter list is a class-body shape only. Both parsers route class
members through a `functionBody(kind, allowGetter)` that plain `fun` declarations
call with `false`, so `fun f {}` stays the error it has always been — pinned by a
test in each front end.

**One signature had to change.** A getter body runs *during* the property read,
so `lookUpProperty` needs something to run it with, and `get` now takes the
interpreter. Nothing else about property lookup uses it.

The invocation sits *inside* the fields-then-methods rule rather than beside it,
which is what keeps the rule a rule:

```
fields[name]            → the field, if there is one
findMethod(name)        → bind, then Call it if IsGetter
otherwise               → Undefined property
```

So a field shadows a getter exactly as it shadows a method. A getter recomputes
on every read, because it is a call and not a cached field. And a runtime error
inside a getter body surfaces at the read, not at some later call — pinned by
`TestGetterErrorsPropagate`.

Static getters compose for free, since a class is already a property owner:
`class version { return "1.0"; }` works, and `this` inside it is the class.

**One new static error.** An initializer may not be declared as a getter:

```
Can't declare an initializer as a getter.
```

Construction calls `init` with the class's arguments, and a getter cannot be
called at all, so the two shapes are incompatible. A *class-method* getter named
`init` is fine — it is not a constructor.

The printer composes tags rather than special-casing: `(fun name (params) …)`,
`(get name …)`, and `(static …)` wrapping either.

## Challenge 3 — how open should fields be?

The prose challenge: Lox lets any code read or write any field, as Python and
JavaScript do. Ruby and Smalltalk make fields private and force access through
methods. Statically typed languages mostly use per-member modifiers. What are
the trade-offs?

The honest answer is that the three positions optimise for different lifespans
of code.

Open access is the cheapest thing to *build with*. You need no boilerplate to
expose state, and a field can become a computed property later without callers
changing — provided the language has getters, which is exactly why that is one
of this chapter's challenges. Its cost lands later: every field is part of the
public interface whether you meant it to be, so nothing can be renamed or
removed with confidence, and there is no place to put an invariant that all
mutations must pass through.

Enforced encapsulation inverts that trade. It costs an accessor per exposed
field, and it makes the common case — a plain data holder — wordier than it
should be. What it buys is a boundary the compiler defends: an invariant written
in a setter cannot be bypassed, so a class can promise something about its own
state.

Modifiers try to have both by making the decision per member. That works, and it
is why most large-codebase languages landed there, but it moves the cost to the
programmer: the default now matters enormously, and every field becomes a small
design decision that is easy to get wrong in the direction of "public, for now".

For Lox the open choice is right, and not only for implementation simplicity. It
is a dynamically typed scripting language, and it has no visibility system for
*anything* else — no module boundaries, no private functions. Adding one for
fields alone would be the only enforced boundary in the language, which is a
strange shape for a language this small.

## Verification

The suite covers both front ends at `k = 1..3`, property and call suffixes
interleaving, chained access folding from the left, both assignment target
cases, the class body parse errors, the `.` error message moving with `k`, field
and method lookup order, a bound method outliving its access, `this` through a
closure nested inside a method and through a class declared inside a function,
`this` compared by identity, initializers with arguments and called directly,
the bare-`return` case, every new static and runtime error, evaluation order in
`Set`, the unused-class warning and the two exemptions, and the REPL across
lines.

```sh
env GOTOOLCHAIN=go1.26.6 GOCACHE=/private/tmp/compiler101-gocache go test ./...

env GOTOOLCHAIN=go1.26.6 go run . examples/classes.lox
env GOTOOLCHAIN=go1.26.6 go run . -parser=llk -k=3 examples/classes.lox
env GOTOOLCHAIN=go1.26.6 go run . -print=ast examples/classes.lox
```

## Scope boundary

All three challenges are done: challenge 1 without metaclasses and challenge 2
with a flag on the AST, both for reasons above, and challenge 3 in prose.

Chapter 13 adds inheritance, and two things in this chapter looked like where it
would push. `super` needs a second synthetic scope, which seemed to mean the
class scope would stop holding exactly one binding and `receiver()`'s
`GetAt(0, 0)` would stop being obvious. And the superclass expression has to be
evaluated and bound *before* the methods close over it, which is precisely the
case the book's define-then-assign two-step exists to handle — so the single
`Define` here looked like it would need revisiting.

Neither bit, and for the same reason. The `super` scope goes *outside* the
`this` scope rather than into it, so each still holds exactly one binding and
slot 0 keeps meaning what it meant; and chapter 13 builds that scope as a local
variable instead of swapping `i.environment`, so the one `Define` of the class
name still lands in the enclosing scope at the slot the resolver numbered. See
[Chapter 13 — inheritance](13-inheritance.md#two-synthetic-scopes-and-the-invariant-that-keeps-them-honest).
