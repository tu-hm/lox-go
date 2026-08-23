package llk

import (
	"sort"
	"strings"

	"compiler101/lexer/token"
)

// This file is the part of an LL(k) parser that an LL(1) parser does not need.
//
// With one token of lookahead, FIRST and FOLLOW are sets of terminals and the
// table is indexed by a single token type. With k tokens they become sets of
// *strings* of up to k terminals, and every set operation grows a truncation
// rule: you may only ever remember the first k symbols, because that is all the
// parser will ever get to look at.

// seq is a k-string: a lookahead window, or one element of a FIRST_k set. Two
// invariants hold everywhere in this package, both enforced by norm:
//
//	len(seq) <= k
//	nothing follows EOF — the stream ends there, so a longer string would
//	describe input that cannot exist
//
// The second invariant is what lets a set hold strings shorter than k without
// any prefix-matching at lookup time: a short string is always one that ran
// into the end of the input.
type seq []token.TokenType

// norm truncates a symbol sequence to a canonical k-string.
func norm(s seq, k int) seq {
	out := make(seq, 0, min(len(s), k))
	for _, t := range s {
		if len(out) == k {
			break
		}
		out = append(out, t)
		if t == token.EOF {
			break
		}
	}
	return out
}

// full reports whether s has room for another symbol. A full string swallows
// whatever is concatenated onto it, which is what makes ⊕_k terminate.
func (s seq) full(k int) bool {
	return len(s) >= k || (len(s) > 0 && s[len(s)-1] == token.EOF)
}

// key is the map-comparable form. Token types are already strings, so this is
// just a join — no hashing scheme to get wrong.
func (s seq) key() string {
	parts := make([]string, len(s))
	for i, t := range s {
		parts[i] = string(t)
	}
	return strings.Join(parts, " ")
}

func (s seq) String() string {
	if len(s) == 0 {
		return "ε"
	}
	return s.key()
}

// kset is a set of k-strings, keyed by seq.key.
type kset map[string]seq

func newKset(strs ...seq) kset {
	s := kset{}
	for _, x := range strs {
		s.add(x)
	}
	return s
}

// add reports whether the set grew, which is how the fixpoint loops below know
// they are not done yet.
func (s kset) add(x seq) bool {
	k := x.key()
	if _, ok := s[k]; ok {
		return false
	}
	s[k] = x
	return true
}

func (s kset) union(o kset) bool {
	changed := false
	for key, x := range o {
		if _, ok := s[key]; !ok {
			s[key] = x
			changed = true
		}
	}
	return changed
}

func (s kset) has(x seq) bool {
	_, ok := s[x.key()]
	return ok
}

// sorted gives a deterministic order — for conflict messages, for tests, and
// so a printed table looks the same twice.
func (s kset) sorted() []seq {
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]seq, len(keys))
	for i, k := range keys {
		out[i] = s[k]
	}
	return out
}

// allFull reports whether every string in the set is full, i.e. no further
// concatenation can change the set.
func (s kset) allFull(k int) bool {
	for _, x := range s {
		if !x.full(k) {
			return false
		}
	}
	return true
}

// concat is ⊕_k, truncated concatenation of sets: every way of following a
// string from a with a string from b, cut back to k symbols.
//
// The empty set is absorbing, and that is deliberate: during the fixpoint an
// empty FIRST set means "not known yet", so a body containing such a symbol
// must contribute nothing at all this round rather than contribute a prefix.
// Monotonicity is what makes the loops terminate at the right answer.
func concat(a, b kset, k int) kset {
	out := make(kset, len(a))
	for _, x := range a {
		if x.full(k) {
			out.add(x)
			continue
		}
		for _, y := range b {
			z := make(seq, 0, k)
			z = append(z, x...)
			for _, t := range y {
				if len(z) == k {
					break
				}
				z = append(z, t)
				if t == token.EOF {
					break
				}
			}
			out.add(z)
		}
	}
	return out
}

// sets holds FIRST_k and FOLLOW_k for one grammar at one k.
type sets struct {
	g      *grammar
	k      int
	first  map[string]kset
	follow map[string]kset
}

// analyze computes both by fixpoint iteration: start empty, apply the defining
// equations to every production, repeat until nothing changes. The sets only
// ever grow and are bounded by the number of k-strings over the terminals, so
// this terminates.
func analyze(g *grammar, k int) *sets {
	s := &sets{g: g, k: k, first: map[string]kset{}, follow: map[string]kset{}}
	for _, name := range g.order {
		s.first[name] = kset{}
		s.follow[name] = kset{}
	}

	// FIRST_k(A) = ⋃ FIRST_k(body) over A's productions.
	for changed := true; changed; {
		changed = false
		for _, p := range g.prods {
			if s.first[p.head].union(s.firstOf(p.body)) {
				changed = true
			}
		}
	}

	// An entry point may be started at, which means the input is allowed to end
	// right after it. Seeding EOF is what tells the ε-productions inside it
	// that end-of-stream is a legal place to stop.
	for _, e := range g.entries {
		s.follow[e].add(seq{token.EOF})
	}

	// For A → α B β: whatever can begin β, followed by whatever can follow A.
	for changed := true; changed; {
		changed = false
		for _, p := range g.prods {
			for i, it := range p.body {
				if it.kind != itemNonterminal {
					continue
				}
				rest := s.firstOf(p.body[i+1:])
				if s.follow[it.name].union(concat(rest, s.follow[p.head], k)) {
					changed = true
				}
			}
		}
	}

	return s
}

// firstOf is FIRST_k of a production body: the terminals' own strings and the
// nonterminals' FIRST sets, concatenated left to right. Actions are skipped —
// they match no input, so they are invisible to the analysis.
func (s *sets) firstOf(body []item) kset {
	out := newKset(seq{}) // ε: the empty body derives the empty string
	for _, it := range body {
		if it.kind == itemAction {
			continue
		}
		if out.allFull(s.k) || len(out) == 0 {
			break
		}
		if it.kind == itemTerminal {
			out = concat(out, newKset(seq{it.term}), s.k)
			continue
		}
		out = concat(out, s.first[it.name], s.k)
	}
	return out
}

// lookahead is the predict set of a production: the k-strings that, seen in the
// input, mean this production is the one to expand. Deriving ε is not special —
// FIRST_k(ε) is {ε}, and ⊕_k with FOLLOW_k(head) fills the window from what
// comes after.
func (s *sets) lookahead(p production) kset {
	return concat(s.firstOf(p.body), s.follow[p.head], s.k)
}
