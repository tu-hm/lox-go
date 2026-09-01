# The Lox grammar

Canonical copy of the grammar. Every chapter from here to the end of the book
edits this file — chapter 6 rewrites the `expression` rule into a precedence
ladder, chapter 8 adds statements, chapter 9 adds control flow, and chapter 10
adds calls and user-defined functions, chapter 12 adds classes, and chapter
13 adds inheritance.

## Chapter 5 — expressions (ambiguous)

```
expression     → literal
               | unary
               | binary
               | grouping ;

literal        → NUMBER | STRING | "true" | "false" | "nil" ;
grouping       → "(" expression ")" ;
unary          → ( "-" | "!" ) expression ;
binary         → expression operator expression ;
operator       → "==" | "!=" | "<" | "<=" | ">" | ">="
               | "+"  | "-"  | "*"  | "/" ;
```

This grammar is **deliberately ambiguous**: `1 + 2 * 3` has two valid parses
(`(+ 1 (* 2 3))` and `(* (+ 1 2) 3)`). Chapter 6 fixes that with a
precedence-stratified rewrite. Don't fix it here.

## Chapter 6 — expressions (precedence ladder)

The ambiguity above is gone: each rung of the ladder binds tighter than the one
above it, and left recursion in the binary rules is what makes them
left-associative.

```
expression     → equality ;
equality       → comparison ( ( "!=" | "==" ) comparison )* ;
comparison     → term ( ( ">" | ">=" | "<" | "<=" ) term )* ;
term           → factor ( ( "-" | "+" ) factor )* ;
factor         → unary ( ( "/" | "*" ) unary )* ;
unary          → ( "!" | "-" ) unary | primary ;
primary        → NUMBER | STRING | "true" | "false" | "nil"
               | "(" expression ")" ;
```

This is what `parser/parser.go` implements, one function per rule: the `*` is a
`for` loop, and the rule it calls is the next rung down.

## Chapter 8 — statements and state

Chapter 8 changes the start symbol from one expression to a complete program,
adds declarations and blocks, and inserts right-associative assignment at the
bottom of the expression precedence ladder:

```
program        → declaration* EOF ;
declaration    → varDecl | statement ;
statement      → exprStmt | printStmt | block ;
block          → "{" declaration* "}" ;
varDecl        → "var" IDENTIFIER ( "=" expression )? ";" ;
printStmt      → "print" expression ";" ;
exprStmt       → expression ";" ;

expression     → assignment ;
assignment     → IDENTIFIER "=" assignment | equality ;
equality       → comparison ( ( "!=" | "==" ) comparison )* ;
comparison     → term ( ( ">" | ">=" | "<" | "<=" ) term )* ;
term           → factor ( ( "-" | "+" ) factor )* ;
factor         → unary ( ( "/" | "*" ) unary )* ;
unary          → ( "!" | "-" ) unary | primary ;
primary        → NUMBER | STRING | "true" | "false" | "nil"
               | "(" expression ")" | IDENTIFIER ;
```

The recursive-descent parser accepts the broader `equality ( "=" assignment )?`
shape first, then validates the left expression as a variable. That one-token
lookahead technique is what lets later chapters add property assignment without
backtracking.

## Chapter 9 — control flow

Chapter 9 adds conditional execution, short-circuiting logical expressions,
and loops. `for` is syntax sugar: parsing lowers it into the existing `Block`,
`While`, and `Expression` statement nodes instead of creating a `For` node.

```
program        → declaration* EOF ;
declaration    → varDecl | statement ;
statement      → exprStmt | forStmt | ifStmt | printStmt | whileStmt | block ;
block          → "{" declaration* "}" ;
varDecl        → "var" IDENTIFIER ( "=" expression )? ";" ;
ifStmt         → "if" "(" expression ")" statement
                 ( "else" statement )? ;
whileStmt      → "while" "(" expression ")" statement ;
forStmt        → "for" "(" ( varDecl | exprStmt | ";" )
                 expression? ";" expression? ")" statement ;
printStmt      → "print" expression ";" ;
exprStmt       → expression ";" ;

expression     → assignment ;
assignment     → IDENTIFIER "=" assignment | logic_or ;
logic_or       → logic_and ( "or" logic_and )* ;
logic_and      → equality ( "and" equality )* ;
equality       → comparison ( ( "!=" | "==" ) comparison )* ;
comparison     → term ( ( ">" | ">=" | "<" | "<=" ) term )* ;
term           → factor ( ( "-" | "+" ) factor )* ;
factor         → unary ( ( "/" | "*" ) unary )* ;
unary          → ( "!" | "-" ) unary | primary ;
primary        → NUMBER | STRING | "true" | "false" | "nil"
               | "(" expression ")" | IDENTIFIER ;
```

The recursive-descent parser consumes an optional `else` before returning from
`ifStatement`, so an `else` binds to the nearest preceding unmatched `if`.
The three optional `for` clauses become an optional outer initializer block, a
`While` whose missing condition defaults to `true`, and an optional increment
expression appended to the loop body.

## Chapter 10 — functions

Chapter 10 adds postfix calls, native and user-defined functions, parameters,
returns, and closures. A call's callee is any sufficiently high-precedence
expression, which is why repeated call suffixes support `factory()(argument)`.

```
program        → declaration* EOF ;
declaration    → funDecl | varDecl | statement ;
funDecl        → "fun" function ;
function       → IDENTIFIER "(" parameters? ")" block ;
parameters     → IDENTIFIER ( "," IDENTIFIER )* ;
statement      → exprStmt | forStmt | ifStmt | printStmt
               | returnStmt | whileStmt | block ;
block          → "{" declaration* "}" ;
varDecl        → "var" IDENTIFIER ( "=" expression )? ";" ;
ifStmt         → "if" "(" expression ")" statement
                 ( "else" statement )? ;
whileStmt      → "while" "(" expression ")" statement ;
forStmt        → "for" "(" ( varDecl | exprStmt | ";" )
                 expression? ";" expression? ")" statement ;
printStmt      → "print" expression ";" ;
returnStmt     → "return" expression? ";" ;
exprStmt       → expression ";" ;

expression     → assignment ;
assignment     → IDENTIFIER "=" assignment | logic_or ;
logic_or       → logic_and ( "or" logic_and )* ;
logic_and      → equality ( "and" equality )* ;
equality       → comparison ( ( "!=" | "==" ) comparison )* ;
comparison     → term ( ( ">" | ">=" | "<" | "<=" ) term )* ;
term           → factor ( ( "-" | "+" ) factor )* ;
factor         → unary ( ( "/" | "*" ) unary )* ;
unary          → ( "!" | "-" ) unary | call ;
call           → primary ( "(" arguments? ")" )* ;
arguments      → expression ( "," expression )* ;
primary        → NUMBER | STRING | "true" | "false" | "nil"
               | "(" expression ")" | IDENTIFIER ;
```

Both parameter and argument lists are limited to 255 entries for compatibility
with the bytecode interpreter built later in the book. A missing return value,
or reaching the end of a function body, produces `nil`.

## Chapter 11 — resolving and binding

Chapter 11 adds no productions. The grammar above is the whole language, and the
resolver introduced in that chapter reads the tree the rules below produce.

That is not a coincidence about this chapter so much as a limit of the notation:
a context-free grammar can say where an identifier may appear, but not which
declaration it refers to. The rules the resolver enforces are exactly the ones
the notation cannot state.

```
Already a variable with this name in this scope.   redeclaration in one local scope
Can't read local variable in its own initializer.  a name inside its own varDecl
Can't return from top-level code.                  returnStmt outside a function body
```

The same pass answers one more question the notation cannot ask — whether
anything ever reads a local — and reports that as a warning rather than an error,
because an unused variable cannot make a program behave wrongly.

Each is checked over the tree, not the token stream, which is why both front ends
get them for free. See [Chapter 11 — resolving and binding](11-resolving-and-binding.md).

## Chapter 12 — classes

Chapter 12 adds one declaration form, one postfix expression suffix, and one
keyword. The suffix is the interesting one: `.` joins `(` on the *same* rung of
the ladder, which is what makes `egg.scramble(3).with` parse without any rule
about how the two interleave.

```
program        → declaration* EOF ;
declaration    → classDecl | funDecl | varDecl | statement ;
classDecl      → "class" IDENTIFIER "{" function* "}" ;
funDecl        → "fun" function ;
function       → IDENTIFIER "(" parameters? ")" block ;
parameters     → IDENTIFIER ( "," IDENTIFIER )* ;
statement      → exprStmt | forStmt | ifStmt | printStmt
               | returnStmt | whileStmt | block ;
block          → "{" declaration* "}" ;
varDecl        → "var" IDENTIFIER ( "=" expression )? ";" ;
ifStmt         → "if" "(" expression ")" statement
                 ( "else" statement )? ;
whileStmt      → "while" "(" expression ")" statement ;
forStmt        → "for" "(" ( varDecl | exprStmt | ";" )
                 expression? ";" expression? ")" statement ;
printStmt      → "print" expression ";" ;
returnStmt     → "return" expression? ";" ;
exprStmt       → expression ";" ;

expression     → assignment ;
assignment     → ( call "." )? IDENTIFIER "=" assignment | logic_or ;
logic_or       → logic_and ( "or" logic_and )* ;
logic_and      → equality ( "and" equality )* ;
equality       → comparison ( ( "!=" | "==" ) comparison )* ;
comparison     → term ( ( ">" | ">=" | "<" | "<=" ) term )* ;
term           → factor ( ( "-" | "+" ) factor )* ;
factor         → unary ( ( "/" | "*" ) unary )* ;
unary          → ( "!" | "-" ) unary | call ;
call           → primary ( "(" arguments? ")" | "." IDENTIFIER )* ;
arguments      → expression ( "," expression )* ;
primary        → NUMBER | STRING | "true" | "false" | "nil" | "this"
               | "(" expression ")" | IDENTIFIER ;
```

A method reuses the `function` rule without the `fun` keyword: inside a class
body the keyword carries no information, so the grammar drops it. `init` is not
a terminal anywhere — it is an ordinary identifier that the *interpreter*
treats specially, which is why `class C { initialize() {} }` is a plain method.

The `assignment` rule is written the way the book writes it, but neither front
end implements it literally. Both parse the left side as an ordinary
expression and then decide, from what it turned out to be, which node to build:

```
Variable  →  Assign      a = 1
Get       →  Set         a.b = 1
anything else            Invalid assignment target.
```

That one-token-of-hindsight trick is the same one chapter 8 introduced for
plain assignment, and it is why property assignment needs no backtracking and
no extra lookahead.

Three rules the notation still cannot state, all enforced by the resolver:

```
Can't use 'this' outside of a class.            a This expression with no enclosing class
Can't return a value from an initializer.       returnStmt with a value inside init()
Local class 'C' is never used.                  a class binding nothing reads (a warning)
```

A property name is a terminal in the grammar but not a variable in the tree:
nothing resolves it, because which properties an object has depends on which
lines ran. That split — variables static, properties dynamic — is the whole
subject of [Chapter 12 — classes](12-classes.md).

## Chapter 13 — inheritance

Chapter 13 adds an optional clause to one rule and one production to another.
The grammar delta is the smallest of any chapter since 11; almost all of the
work is in what the two names *mean*.

```
classDecl      → "class" IDENTIFIER ( "<" IDENTIFIER )? "{" function* "}" ;
primary        → NUMBER | STRING | "true" | "false" | "nil" | "this"
               | "(" expression ")" | IDENTIFIER
               | "super" "." IDENTIFIER ;
```

Everything else is unchanged from chapter 12.

Two things the notation is saying deliberately.

**The superclass is `IDENTIFIER`, not `expression`.** A superclass is named,
never computed, so `class A < f() {}` is a syntax error at the `(` rather than a
call nobody wanted. Both front ends parse it straight into a `Variable` node —
which is still a *variable use*, so it resolves like one and obeys scope.

**`super` is never alone.** The production is `"super" "." IDENTIFIER`, three
terminals in one rule, so a bare `super` is a parse error rather than something
the resolver has to catch later. That is the grammar encoding a real fact:
`super` names no value, only the place a method lookup starts.

Three more rules the notation cannot state, all enforced by the resolver:

```
A class can't inherit from itself.               a superclass naming its own class
Can't use 'super' outside of a class.            a Super expression with no enclosing class
Can't use 'super' in a class with no superclass. a Super expression in a base class
```

The last two are separate because the mistakes are: one is a misplaced keyword,
the other a class that forgot to say what it inherits from. And one rule the
resolver cannot state either, because it is about a value rather than a name:

```
Superclass must be a class.                      the superclass name held something else
```

See [Chapter 13 — inheritance](13-inheritance.md).

## The LL(k) form — what the table-driven parser reads

`parser/llk` parses the same language from the same grammar written differently,
because a predictive parser cannot use the form above. Two mechanical rewrites:

**1. Left recursion out.** `a ( op a )*` hides left recursion — the loop is an
`a` that starts with an `a`. An LL parser expands the leftmost symbol, so it
would recurse forever. Consume one operand, then loop in a tail rule:

```
term           → factor termTail ;
termTail       → "-" factor termTail
               | "+" factor termTail
               | ε ;
```

**2. Alternation out.** Prediction picks a whole production, not a branch inside
one, so `( "-" | "+" )` becomes one production per operator — the same
desugaring as challenge 1 below, needed for real this time.

The full chapter 8 LL form is:

```
program        → declarations ;
declarations   → declaration declarations | ε ;
declaration    → varDeclaration | statement ;
statement      → exprStatement | printStatement | block ;
varDeclaration → "var" IDENTIFIER varInitializer ";" ;
varInitializer → "=" expression | ε ;
printStatement → "print" expression ";" ;
exprStatement  → expression ";" ;
block          → "{" declarations "}" ;

expression     → assignment ;
assignment     → equality assignmentTail ;
assignmentTail → "=" assignment | ε ;
equality       → comparison equalityTail ;
equalityTail   → "!=" comparison equalityTail
               | "==" comparison equalityTail
               | ε ;
comparison     → term comparisonTail ;
comparisonTail → ">" term comparisonTail | ">=" term comparisonTail
               | "<" term comparisonTail | "<=" term comparisonTail
               | ε ;
term           → factor termTail ;
termTail       → "-" factor termTail | "+" factor termTail | ε ;
factor         → unary factorTail ;
factorTail     → "/" unary factorTail | "*" unary factorTail | ε ;
unary          → "!" unary | "-" unary | primary ;
primary        → NUMBER | STRING | "true" | "false" | "nil"
               | IDENTIFIER | "(" expression ")" ;
```

`assignmentTail` is the left-factored form of assignment. Its action runs after
the right-hand side and checks that the value already on the stack is a
`Variable`; this preserves both right associativity and LL(1) prediction.

The tail rewrite loses the associativity that left recursion gave for free.
`parser/llk` gets it back by placing the tree-building action *inside* the
production, before the recursive tail — see
[docs/llk-parser.md § 3](llk-parser.md#3-semantic-actions-and-where-they-sit).

This grammar is LL(1), so `llk.DefaultK` is 1; `-k=2` and `-k=3` parse it too,
since every LL(1) grammar is LL(k) for larger k.

Chapter 9 inserts two more factored expression levels:

```
assignment     → logic_or assignmentTail ;
logic_or       → logic_and logic_orTail ;
logic_orTail   → "or" logic_and logic_orTail | ε ;
logic_and      → equality logic_andTail ;
logic_andTail  → "and" equality logic_andTail | ε ;
```

Those productions remain LL(1) and build `Logical` nodes before recurring into
their tails, preserving left associativity. Compound statements are
orchestrated in `parser/llk/parser.go`, just as recoverable blocks already were.
This is especially important for `if`: the usual optional-`else` grammar has a
dangling-`else` prediction conflict, while eager recursive statement parsing
binds `else` to the nearest `if` without weakening the table's conflict checks.
Nested expression runs mask lookahead after their closing `)` or `;`, so the
same logic works for every supported `k`.

Chapter 10 inserts a factored postfix-call level below `unary`:

```
unary          → "!" unary | "-" unary | call ;
call           → primary callTail ;
callTail       → "(" arguments ")" {mkCall} callTail | ε ;
arguments      → expression {startArguments} argumentsTail | ε ;
argumentsTail  → "," expression {appendArgument} argumentsTail | ε ;
```

`mkCall` runs before the recursive tail, so `factory()(1)` folds from the left.
Return statements remain table-driven leaf statements. Function declarations
stay in the orchestration layer because their bodies may contain recoverable
declarations and compound statements that intentionally sit outside the table.

Chapter 12 adds one production to `callTail` and one to `primary`:

```
callTail       → "(" arguments ")" {mkCall} callTail
               | "." IDENTIFIER {mkGet} callTail
               | ε ;
primary        → ... | "this" {mkThis} ;
```

Prediction stays LL(1): the three `callTail` productions begin with `(`, `.`,
and ε, which are disjoint. `mkGet` sits before the recursive tail for the same
reason `mkCall` does, so `a.b.c` folds from the left. `mkAssign` gains the
`Get → Set` case and keeps rejecting everything else.

Class declarations join function declarations in the orchestration layer: a
class body is a repetition of a rule whose own body contains recoverable
declarations, which is exactly what that layer is for.

Chapter 13 adds one production to `primary`, and nothing else:

```
primary        → ... | "super" "." IDENTIFIER {mkSuper} ;
```

Three terminals in one production, so no lookahead window can split it and no
other primary begins with `super`: still LL(1). The superclass clause is *not*
in the table — like the class body it introduces, it is one optional token pair
inside a repetition the orchestration layer already owns.

A visible cost of the table used to show up here. `egg.;` is rejected at every
`k`, but not by the same rule — at `k=1` the `.` production is predicted and the
`IDENTIFIER` match fails, giving the specific "Expect property name after
'.'."; at `k=2` the window `. ;` matches no production of `callTail` at all, so
the failure was one step earlier and the message more generic.

`table.recover` closes that: on a prediction miss the driver expands the
production whose lookahead shares the longest prefix with the window, so the
error lands on the terminal that actually disagrees and carries its message. All
three lookaheads, and both front ends, now say "Expect property name after
'.'." See [the LL(k) notes](llk-parser.md#recovery-buying-the-message-back).

## Notation

- `→` separates a rule head from its body; `;` ends the rule.
- **Terminals** are quoted (`"("`) or CAPITALISED (`NUMBER`). These are
  `token.TokenType` values.
- **Nonterminals** are lowercase. These become AST node types.
- `|` alternation · `( )` grouping · `*` zero-or-more · `+` one-or-more ·
  `?` optional.

## Rule → node mapping

| Grammar rule | AST node | Fields |
|---|---|---|
| assignment | `Assign` | `Name token.Token`, `Value Expr` |
| `binary` | `Binary` | `Left Expr`, `Operator token.Token`, `Right Expr` |
| call | `Call` | `Callee Expr`, `Paren token.Token`, `Arguments []Expr` |
| `grouping` | `Grouping` | `Expression Expr` |
| `literal` | `Literal` | `Value any` |
| logical operator | `Logical` | `Left Expr`, `Operator token.Token`, `Right Expr` |
| `unary` | `Unary` | `Operator token.Token`, `Right Expr` |
| variable access | `Variable` | `Name token.Token` |
| property read | `Get` | `Object Expr`, `Name token.Token` |
| property write | `Set` | `Object Expr`, `Name token.Token`, `Value Expr` |
| `this` | `This` | `Keyword token.Token` |
| `super` | `Super` | `Keyword token.Token`, `Method token.Token` |
| block | `Block` | `Statements []Stmt` |
| class declaration | `Class` | `Name token.Token`, `Superclass *Variable`, `Methods []*Function`, `ClassMethods []*Function` |
| expression statement | `Expression` | `Expression Expr` |
| function declaration | `Function` | `Name token.Token`, `Params []token.Token`, `Body []Stmt` |
| if statement | `If` | `Condition Expr`, `ThenBranch Stmt`, `ElseBranch Stmt` |
| print statement | `Print` | `Expression Expr` |
| return statement | `Return` | `Keyword token.Token`, `Value Expr` |
| variable declaration | `Var` | `Name token.Token`, `Initializer Expr` |
| while statement | `While` | `Condition Expr`, `Body Stmt` |

`operator` gets no node — the token already carries which operator it is. That
is the "abstract" in abstract syntax tree: only what the interpreter needs
survives.

---

## Challenge 1 — the metasyntax is only sugar

`*`, `+`, `?`, `|` and `( )` add no power. Every one of them desugars into extra
rules and recursion. Removing them from the chapter 5 grammar: each alternative
becomes its own production, and the parenthesised operator choice becomes a
named nonterminal.

```
expression     → literal ;
expression     → unary ;
expression     → binary ;
expression     → grouping ;

literal        → NUMBER ;
literal        → STRING ;
literal        → "true" ;
literal        → "false" ;
literal        → "nil" ;

grouping       → "(" expression ")" ;

unary          → unary_op expression ;
unary_op       → "-" ;
unary_op       → "!" ;

binary         → expression operator expression ;

operator       → "==" ;   operator → "!=" ;
operator       → "<"  ;   operator → "<=" ;
operator       → ">"  ;   operator → ">=" ;
operator       → "+"  ;   operator → "-"  ;
operator       → "*"  ;   operator → "/"  ;
```

The interesting half is repetition, which this grammar has none of. The
chapter's challenge grammar does:

```
expr → expr ( "(" ( expr ( "," expr )* )? ")" | "." IDENTIFIER )+
     | IDENTIFIER
     | NUMBER
```

Desugared — `+` becomes left recursion, `*` becomes left recursion, `?` becomes
an empty alternative:

```
expr     → expr calls ;
expr     → IDENTIFIER ;
expr     → NUMBER ;

calls    → call ;              // the "+": one...
calls    → calls call ;        // ...or more

call     → "(" arguments ")" ;
call     → "." IDENTIFIER ;

arguments →  ;                 // the "?": empty
arguments → arglist ;

arglist  → expr ;              // the "*": one...
arglist  → arglist "," expr ;  // ...or more, comma separated
```

**The lesson:** repetition *is* recursion, and the sugar exists so the grammar
stays readable — not so it can say anything new. Worth knowing because chapter 6
hand-writes a recursive-descent parser, where `*` in the grammar becomes a
`for` loop and recursion in the grammar becomes recursion in the code.

## Challenge 2 — the complement to Visitor

Visitor makes **adding operations** cheap and adding types expensive. The
functional mirror — bundle every operation on one *type* together — makes
adding types cheap and adding operations expensive. Go can express both, and
the repo shows both sides: the visitor interface in `ast/expr.go` is the OO
half, and a type switch over `Expr` (plan §4, Option B) is the functional half.
The write-up of that trade is [plan §4](05-representing-code.md#4-design-decision-how-to-represent-the-ast-in-go);
this repo took Option A for the compile-time exhaustiveness check.

## Challenge 3 — RPN printer

Done: `ast/rpn.go`, tested in `ast/rpn_test.go`. A second operation over the
same tree with zero changes to any node type — the argument for the pattern,
in about forty lines.
