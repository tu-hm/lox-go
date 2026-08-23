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
	token.SEMICOLON:     "';'",
	token.MINUS:         "'-'",
	token.PLUS:          "'+'",
	token.SLASH:         "'/'",
	token.STAR:          "'*'",
	token.BANG:          "'!'",
	token.BANG_EQUAL:    "'!='",
	token.EQUAL_EQUAL:   "'=='",
	token.GREATER:       "'>'",
	token.GREATER_EQUAL: "'>='",
	token.LESS:          "'<'",
	token.LESS_EQUAL:    "'<='",
	token.NUMBER:        "a number",
	token.STRING:        "a string",
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
