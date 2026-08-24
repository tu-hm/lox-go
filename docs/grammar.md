# The Lox grammar

Canonical copy of the grammar. Every chapter from here to the end of the book
edits this file — chapter 6 rewrites the `expression` rule into a precedence
ladder, chapter 8 adds statements, and chapter 9 adds control flow.

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
| `grouping` | `Grouping` | `Expression Expr` |
| `literal` | `Literal` | `Value any` |
| logical operator | `Logical` | `Left Expr`, `Operator token.Token`, `Right Expr` |
| `unary` | `Unary` | `Operator token.Token`, `Right Expr` |
| variable access | `Variable` | `Name token.Token` |
| block | `Block` | `Statements []Stmt` |
| expression statement | `Expression` | `Expression Expr` |
| if statement | `If` | `Condition Expr`, `ThenBranch Stmt`, `ElseBranch Stmt` |
| print statement | `Print` | `Expression Expr` |
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
