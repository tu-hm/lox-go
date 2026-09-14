package llk

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"compiler101/lexer/token"
)

// table is the parser. Everything after this file is a loop over a stack: the
// decisions all live here, precomputed, one row per nonterminal and one column
// per k-string of lookahead.
type table struct {
	g    *grammar
	k    int
	sets *sets
	rows map[string]map[string]int // head → lookahead key → index into g.prods
}

// ConflictError says the grammar is not LL(k) for this k: two productions of
// the same nonterminal want the same lookahead, so no amount of table lookup
// can choose between them. This is the check a hand-written recursive-descent
// parser never performs — it just picks whichever branch is written first and
// silently parses a different language than the one you wrote down.
type ConflictError struct {
	K         int
	Head      string
	Lookahead seq
	First     production
	Second    production
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("llk: grammar is not LL(%d): on lookahead %s both\n\t%s\nand\n\t%s\napply",
		e.K, e.Lookahead, e.First, e.Second)
}

func newTable(g *grammar, k int) (*table, error) {
	if k < MinK || k > MaxK {
		return nil, fmt.Errorf("llk: k = %d out of range [%d, %d]: the sets are strings of k terminals, so they grow steeply with k", k, MinK, MaxK)
	}
	if err := g.validate(); err != nil {
		return nil, err
	}

	t := &table{g: g, k: k, sets: analyze(g, k), rows: map[string]map[string]int{}}
	for i, p := range g.prods {
		row := t.rows[p.head]
		if row == nil {
			row = map[string]int{}
			t.rows[p.head] = row
		}
		// sorted, not range-over-map: a conflict must be reported with the same
		// pair of productions every run, or the failure is unreproducible.
		for _, la := range t.sets.lookahead(p).sorted() {
			if j, dup := row[la.key()]; dup {
				return nil, &ConflictError{K: k, Head: p.head, Lookahead: la, First: g.prods[j], Second: p}
			}
			row[la.key()] = i
		}
	}
	return t, nil
}

// predict is the one decision an LL parser makes: given the nonterminal on top
// of the stack and the next k tokens, which production expands it. A miss is a
// syntax error, and it is detected here — before a single token of the bad
// production has been consumed.
func (t *table) predict(head string, la seq) (production, bool) {
	i, ok := t.rows[head][la.key()]
	if !ok {
		return production{}, false
	}
	return t.g.prods[i], true
}

// recover is the error path's second chance, and it exists to buy back the one
// thing this parser's messages otherwise cost.
//
// When predict misses, the parser knows the input is wrong but not which part.
// All it can say is the message attached to the nonterminal — and with k > 1
// that lands further out than a hand-written parser would put it. `egg.;` fails
// at callTail rather than at the missing property name, because the whole
// two-token window `. ;` matches no production of callTail; at k = 3 it fails
// further out still, before `egg` is even consumed. Wider lookahead notices the
// mistake earlier and so describes it less specifically, which is a bad trade
// for a message a human reads.
//
// So: find the production whose lookahead comes closest to the window, measured
// by longest common prefix, and expand it anyway. The driver then matches the
// prefix that did agree and fails on the first terminal that did not — with the
// message the grammar attached to that terminal. That is exactly the token and
// exactly the message recursive descent reports, at every k.
//
// This cannot make a bad program parse. A production whose lookahead set lacks
// the window derives nothing that starts with the window, so committing to it
// can only move the failure, never avoid one. Nor can it loop: the grammar is
// LL(k), so it has no left recursion and expansion reaches a terminal in a
// bounded number of steps.
//
// A tie means two productions match the window equally well and neither is
// evidence over the other, so the generic message stands rather than a guess.
//
// When nothing resembles the window at all, vanish gets a look first.
func (t *table) recover(head string, la seq) (production, bool) {
	best, longest, tied := production{}, 0, false
	for _, i := range t.g.byHead[head] {
		switch n := t.sets.lookahead(t.g.prods[i]).nearest(la); {
		case n > longest:
			best, longest, tied = t.g.prods[i], n, false
		case n == longest && n > 0:
			tied = true
		}
	}
	if longest == 0 {
		if empty, ok := t.vanish(head); ok {
			return empty, true
		}
	}
	if longest == 0 || tied {
		return production{}, false
	}
	return best, true
}

// vanish is the repair for the rules recursive descent writes as a loop rather
// than as a choice: a rule whose every alternative but ε opens with a literal
// token is a `while (match(...))` over there, and that loop ends on any token
// that is none of them. This parser instead asks the table to predict the rule
// and finds no column, so it reports the rule's own generic message — one level
// too deep, and one level less specific than what the user needs.
//
// `foo(a b)` is the case. After the argument, callTail has to decide, and an
// identifier begins neither '(' nor '.' nor anything that may follow a call, so
// prediction misses and the message is "Expect end of expression." Recursive
// descent is already out of its suffix loop and sitting on the ')' it wants,
// which is why it says "Expect ')' after arguments." Deriving ε here puts this
// parser on that same ')', with that terminal's own message.
//
// The terminal test is what keeps this from over-applying. A rule like
// returnValue → expression | ε is not a loop: recursive descent guards it with
// `if (!check(SEMICOLON))` and then commits to parsing an expression, so `return`
// at end of input must still say "Expect expression." rather than skipping the
// value and complaining about a missing ';'. Such a rule opens with a
// nonterminal, so it is not one that may vanish.
//
// Like expanding the nearest production, this cannot accept a bad program: ε
// consumes nothing, so every token the program was going to be rejected for is
// still there to reject it. Nor can it loop — ε pushes no terminal or
// nonterminal, so each use strictly shrinks the work stack.
func (t *table) vanish(head string) (production, bool) {
	empty, canVanish := production{}, false
	for _, i := range t.g.byHead[head] {
		prod := t.g.prods[i]
		if prod.derivesEmpty() {
			empty, canVanish = prod, true
			continue
		}
		if !prod.startsWithTerminal() {
			return production{}, false
		}
	}
	return empty, canVanish
}

// fail is the message for a prediction miss. The grammar names most of them;
// the fallback lists what the row would have accepted, which beats a bare
// "syntax error" when a new rule forgets its message.
func (t *table) fail(head string) string {
	if msg := t.g.fail[head]; msg != "" {
		return msg
	}
	seen := map[token.TokenType]bool{}
	var names []string
	for key := range t.rows[head] {
		first := key
		if i := strings.IndexByte(key, ' '); i >= 0 {
			first = key[:i]
		}
		tt := token.TokenType(first)
		if !seen[tt] {
			seen[tt] = true
			names = append(names, describe(tt))
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "Expect expression."
	}
	return "Expect one of " + strings.Join(names, ", ") + "."
}

// describe renders a terminal the way an error message wants it, rather than as
// the scanner's SCREAMING_CASE type name.
func describe(t token.TokenType) string {
	if s, ok := terminalText[t]; ok {
		return s
	}
	return string(t)
}

var terminalText = map[token.TokenType]string{
	token.LEFT_PAREN:    "'('",
	token.RIGHT_PAREN:   "')'",
	token.LEFT_BRACE:    "'{'",
	token.RIGHT_BRACE:   "'}'",
	token.SEMICOLON:     "';'",
	token.MINUS:         "'-'",
	token.PLUS:          "'+'",
	token.SLASH:         "'/'",
	token.STAR:          "'*'",
	token.BANG:          "'!'",
	token.BANG_EQUAL:    "'!='",
	token.EQUAL:         "'='",
	token.EQUAL_EQUAL:   "'=='",
	token.GREATER:       "'>'",
	token.GREATER_EQUAL: "'>='",
	token.LESS:          "'<'",
	token.LESS_EQUAL:    "'<='",
	token.NUMBER:        "a number",
	token.STRING:        "a string",
	token.IDENTIFIER:    "an identifier",
	token.PRINT:         "'print'",
	token.VAR:           "'var'",
	token.TRUE:          "'true'",
	token.FALSE:         "'false'",
	token.NIL:           "'nil'",
	token.EOF:           "end of input",
}

// String renders the table for humans: `go test -run TestTableDump -v`, or a
// call from a debugger, and you can read the same thing the driver reads.
func (t *table) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "LL(%d) table — %d nonterminals, %d productions\n", t.k, len(t.g.order), len(t.g.prods))
	for _, head := range t.g.order {
		fmt.Fprintf(&b, "\n%s\n", head)
		keys := make([]string, 0, len(t.rows[head]))
		for key := range t.rows[head] {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			la := key
			if la == "" {
				la = "ε"
			}
			fmt.Fprintf(&b, "  %-28s %s\n", la, t.g.prods[t.rows[head][key]])
		}
	}
	return b.String()
}

// Building the table costs more than parsing with it, and the grammar is fixed
// at compile time, so every parser at a given k shares one. The table is
// read-only after construction, hence safe to share across goroutines.
var (
	tableMu sync.Mutex
	tables  = map[int]*table{}
)

func tableFor(k int) (*table, error) {
	tableMu.Lock()
	defer tableMu.Unlock()

	if t, ok := tables[k]; ok {
		return t, nil
	}
	t, err := newTable(loxGrammar(), k)
	if err != nil {
		return nil, err
	}
	tables[k] = t
	return t, nil
}
