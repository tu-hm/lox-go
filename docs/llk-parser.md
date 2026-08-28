# The LL(k) parser — a second algorithm over the same grammar

The repo now has two parser front ends. They take the same tokens, build the
same `ast.Expr` and `ast.Stmt` trees, and are checked against each other in
`parser/algorithm_test.go`:

| | `parser` | `parser/llk` |
|---|---|---|
| Algorithm | recursive descent | table-driven LL(k) |
| Where decisions live | in code, one function per rule | in a table, built before parsing |
| Recursion | the Go call stack | an explicit stack, no recursion |
| Grammar | implied by the code | data (`grammar.go`), inspectable |
| Ambiguity | silently resolved by branch order | refuses to build, names both rules |
| Lookahead | whatever a rule peeks at | exactly k, uniformly |
| Error messages | written per call site | attached to grammar positions |
| Left recursion | impossible either way | impossible, and it *tells* you |

Pick one on the command line:

```
go run . script.lox                  # recursive descent (default)
go run . -parser=llk script.lox      # LL(k), k = 1
go run . -parser=llk -k=3 script.lox # LL(3)
```

Both print the same trees. That is the point of having two.

---

## 1. What LL(k) means

**L**eft-to-right input, **L**eftmost derivation, **k** tokens of lookahead. The
parser always holds a sentential form — what it currently believes the input
will turn out to be — and repeatedly expands its leftmost unexpanded
nonterminal. The only question it ever asks is:

> *the leftmost nonterminal is `A`, and the next k tokens are `w` — which of
> `A`'s productions is it?*

If that question has exactly one answer for every `(A, w)` the grammar can
reach, the grammar is LL(k) and the answers fit in a table. Parsing is then a
loop: pop, look up, push. No backtracking, no guessing, linear time.

Recursive descent asks the same question, but answers it with hand-written
`if`s. When two branches could both match, it takes the first one written and
says nothing. The table cannot do that — two answers for one question is a
build error, which is the practical reason to have a table at all.

## 2. The grammar has to be rewritten first

An LL parser expands the leftmost symbol, so a rule that begins with itself
never terminates:

```
term → term ( "-" | "+" ) factor      // left recursion: expands to itself forever
```

The standard fix consumes one operand, then loops in a tail rule:

```
term     → factor termTail
termTail → "-" factor termTail
         | "+" factor termTail
         | ε
```

And `( a | b )` becomes one production per alternative, because prediction picks
a whole production, not a branch inside one. That is `docs/grammar.md`'s
challenge-1 desugaring done for real rather than as an exercise — see
[grammar.md § LL(k) form](grammar.md#the-llk-form--what-the-table-driven-parser-reads).

The rewrite costs something: **associativity is no longer in the shape of the
rule.** `termTail` recurses to the right, so building nodes on the way out of
the recursion would give `1 - 2 - 3` as `(- 1 (- 2 3))` — right-associative, and
wrong for arithmetic.

## 3. Semantic actions, and where they sit

The fix is to put the tree-building *inside* the production, at the position
where it runs:

```
termTail → "-" factor {mkBinary} termTail
```

Every matched terminal pushes its token on a value stack; every completed
nonterminal leaves its node there. `mkBinary` runs after `"-" factor` and before
the recursive `termTail`, so it always finds `[left, operator, right]` on top,
folds them into one node, and leaves that as the left operand of the next
iteration:

```
1 - 2 - 3   [1] → [1,-,2] → [(- 1 2)] → [(- 1 2),-,3] → [(- (- 1 2) 3)]
```

Move that action to the end of the body and every test still passes except
`TestLeftAssociativity`. It is a one-token change with a language-sized effect,
which is why it has its own test.

## 4. FIRST_k and FOLLOW_k

To fill the table you need, for each production, the set of k-token windows that
predict it. Two ingredients, both sets of **k-strings** — sequences of at most k
terminals (`sets.go`):

- **FIRST_k(α)** — the k-token prefixes of everything α can derive.
- **FOLLOW_k(A)** — the k-token windows that can appear right after an `A`.

The predict set of `A → α` is then

```
LA_k(A → α) = FIRST_k(α) ⊕_k FOLLOW_k(A)
```

where `⊕_k` is concatenation truncated back to k symbols. FOLLOW is what makes
ε-productions work: `termTail → ε` is predicted exactly when the next tokens
are ones that can *follow* a term — `;`, `)`, a comparison operator, or end of
input. Which is also the LL(1) condition for this grammar in one line:

> `FOLLOW_1(termTail)` must not contain `+` or `-`, or the ε-production and the
> operator productions would both apply.

`TestFollowSetDecidesEpsilon` asserts exactly that.

Two rules keep k-strings canonical, both in `norm`:

1. never longer than k — that is all the parser can see;
2. **nothing after EOF** — no input continues past the end of the stream.

Rule 2 is what lets a set hold strings shorter than k with no prefix-matching at
lookup time: a short string is always one that ran into the end of the input.
The lookahead window is normalised the same way, so window and table agree on
how to spell "the input ends here".

Both sets are computed by fixpoint: start empty, apply the defining equations to
every production, repeat until nothing changes. During that loop an empty set
means *not known yet*, so `⊕_k` treats it as absorbing — a body containing such
a symbol contributes nothing this round rather than contributing a wrong prefix
that could never be removed. `TestConcatIsAbsorbing` pins that down.

### Why the table carries the core program grammar

`program → declarations` and the declaration/statement/block productions give
FOLLOW analysis the complete context, even though recovery starts a fresh
driver for each individual declaration. Without the complete grammar,
`FOLLOW_2(expression)` would be `{"; EOF"}`, and an LL(2) parser would reject
the perfectly good `print 1 + 2; print 3;` — the window after the `;` reaches
into the next statement. `TestFollowSpansStatements` guards it.

That context is required by the internal whole-program grammar entry and keeps
the table's model complete. The public recovering entry point additionally
bounds each leaf-statement run at its semicolon, so it can safely restart the
driver for the next declaration.

Chapter 9 adds one deliberate boundary. `if`, `while`, and `for` are
orchestrated recursively in `parser.go`, alongside recoverable blocks. Putting
the conventional `ifStmt → ... ( "else" statement )?` rule into the prediction
table would introduce the dangling-`else` conflict that the table is designed
to reject. The orchestration layer eagerly parses an `else`, matching recursive
descent's nearest-`if` rule.

Nested header expressions still use the table. For `k > 1`, their lookahead is
masked after the closing `)` or `;`; otherwise a condition run could see tokens
from the following statement, a continuation outside its table entry. The
delimiter remains visible, while anything beyond it is normalized to EOF.

Chapter 10 keeps that division. Postfix calls and return statements are
ordinary LL(1) productions, so the table parses them. The call tail folds
before recurring, making `factory()(1)` a call whose callee is `factory()`.
Argument-list actions accumulate expressions from left to right and report the
book's 255-argument limit without abandoning an otherwise valid parse.

Function declarations are orchestrated in `parser.go`. Their braced bodies can
contain nested functions, recoverable declarations, `if`, `while`, and `for`,
so delegating the body to the existing recursive block boundary preserves all
of those behaviors. Parameter parsing is kept beside that boundary and applies
the same 255-item limit as recursive descent.

## 5. The table, and the check you get for free

`newTable` walks the predict set of every production and files it under
`(head, lookahead)`. A slot claimed twice is a `ConflictError` naming both
productions and the lookahead that cannot separate them:

```
llk: grammar is not LL(1): on lookahead NUMBER both
	s → NUMBER PLUS NUMBER
and
	s → NUMBER
apply
```

That grammar is LL(2) — the second token separates the two — and
`TestConflictIsReported` builds it at both k to show the difference. This is the
whole payoff of the table: an ambiguity that recursive descent would resolve by
accident, reported before a single input is parsed.

Table size is the cost, and it grows steeply with the number of k-strings the
grammar admits. `MaxK` is 3 for that reason. The curve is also why real tools
stop at LL(1) plus a hack, or move to LL(*)/PEG, rather than raising k. Tables
are cached per k and shared by every parser, since the grammar is a compile-time
constant.

## 6. The driver

With the table built, parsing is twenty lines (`parser.go`, `run`). Push the
start symbol; then repeatedly take the top of the stack:

- **terminal** — match it against the input and push its token on the value
  stack, or fail with the message the grammar attached to that position;
- **action** — run it against the value stack, possibly returning a semantic
  syntax error such as an invalid assignment target;
- **nonterminal** — ask the table, push the predicted body reversed so its
  leftmost symbol comes off first.

When the stack empties, exactly one value must remain: an expression, statement,
or statement list depending on the entry point. Anything else
is an action popping the wrong number of values — a grammar bug, reported as an
internal error rather than as a syntax error, because the user's source did
nothing wrong.

This loop is identical for any LL(k) grammar. Lox's table-driven expression and
leaf-statement rules live in `grammar.go`; recoverable blocks and compound
statement orchestration live beside the driver in `parser.go`.

## 7. Errors

Recursive descent writes its messages where it notices the problem, with all the
context of the surrounding function. A table has no such context, so the
messages come from the grammar: terminals carry theirs (`Expect ')' after
expression.`) and nonterminals carry one for "nothing predicts here"
(`Expect expression.`).

Recovery is the same rule as recursive descent's, deliberately: discard tokens
until just past a `;` or just before a keyword that can only start a statement.
The one addition is that both stacks are dropped — whatever was half-expanded
describes a parse that is not going to happen. `TestSynchronizeClearsTheStacks`
checks the next statement really does start clean.

`ParseProgram` restarts the driver per declaration rather than running one parse
over the whole program. Blocks recursively do the same for their contents;
chapter 9 control statements and the chapter 10 function and chapter 12 class
declarations recursively call the same declaration and statement dispatchers.
Recovery is much easier to reason about when the driver stack is empty at each
declaration boundary. The chapter 7 `ParseAll` expression entry point remains
for compatibility and uses the same reset rule.

Where the two front ends genuinely differ is *when* they notice. On `1 2`, the
LL(k) parser fails at `factorTail` — the lookahead `NUMBER` is neither an
operator nor anything that may follow an expression — while recursive descent
returns the finished expression and only then checks for leftovers. Same token,
same line, different message (`Expect end of expression.` either way here, but
raised from different places). That is the LL "viable prefix" property: the
parser never consumes a token that cannot be part of a valid program.

Chapter 12's property access shows the same effect costing something legible.
`egg.;` is rejected at every `k`, but by a different rule each time:

| `k` | fails at | message |
|---|---|---|
| 1 | the `IDENTIFIER` terminal inside the `.` production | `Expect property name after '.'.` |
| 2 | `callTail`, where the window `. ;` predicts nothing | `Expect end of expression.` |
| 3 | `call`, which cannot be predicted at all | `Expect expression.` |

Only `k = 1` gives the message recursive descent gives, because only at `k = 1`
does the parser commit to the `.` production before discovering the problem.
Wider lookahead notices the same mistake *sooner* and therefore describes it
*less* specifically — the viable-prefix property working against readability.
This is the sharpest argument in the repo for `DefaultK = 1`, and
`TestPropertyNameErrorMovesWithK` pins all three rather than hiding the spread.

## 8. Reading the table

`table.String()` prints it row by row, lookahead by lookahead — the same thing
the driver reads:

```go
tab, _ := newTable(loxGrammar(), 1)
fmt.Print(tab)
```

```
LL(1) table

termTail
  BANG_EQUAL                   termTail → ε
  EOF                          termTail → ε
  EQUAL_EQUAL                  termTail → ε
  GREATER                      termTail → ε
  GREATER_EQUAL                termTail → ε
  LESS                         termTail → ε
  LESS_EQUAL                   termTail → ε
  MINUS                        termTail → MINUS factor termTail
  PLUS                         termTail → PLUS factor termTail
  RIGHT_PAREN                  termTail → ε
  SEMICOLON                    termTail → ε
```

Eleven lookaheads, two of them doing work: that row *is* `FOLLOW_1(termTail)`
plus the two operators, and you can read the LL(1) condition straight off it —
`MINUS` and `PLUS` appear once each.

## 9. Files

```
parser/
├── algorithm.go        Algorithm interface, Kind, Config, NewOf — pick one
├── algorithm_test.go   both parsers, same inputs, same trees
├── parser.go           recursive descent (chapter 6)
└── llk/
    ├── grammar.go      the grammar as data, plus the semantic actions
    ├── sets.go         k-strings, FIRST_k, FOLLOW_k
    ├── table.go        prediction table, conflict detection, cache
    ├── parser.go       the driver
    └── llk_test.go     sets, table, conflicts, parses
```

## 10. What to try

- Delete `g.add(nStatements, ...)` and run `go test ./parser/... -run Follow`.
- Move `act(mkBinary)` to the end of the body in `level()` — everything passes
  but `TestLeftAssociativity`.
- Add `g.add("primary", term(token.NUMBER, ""), term(token.NUMBER, ""))` and
  watch the table refuse to build.
- Add a rule the grammar cannot predict at k=1 but can at k=2, and see `-k=2`
  accept what `-k=1` rejects.
