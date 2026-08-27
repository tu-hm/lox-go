package interpreter

import (
	"compiler101/lexer/token"
	"compiler101/pkg/errors"
)

// Environment stores the bindings for one lexical scope. Enclosing links a
// local scope to its parent; the global environment has no enclosing scope.
//
// The two kinds of scope store their bindings differently because they are
// found differently. A global is found by name at runtime, since the code that
// reads it may be compiled before it is declared. A local is found by the
// (distance, slot) pair the resolver computed, so a local environment needs no
// names at all — only its values, in declaration order.
type Environment struct {
	Enclosing *Environment
	values    map[string]any // the global environment only
	slots     []any          // local environments only
}

func NewEnvironment(enclosing *Environment) *Environment {
	if enclosing == nil {
		return &Environment{values: make(map[string]any)}
	}
	return &Environment{Enclosing: enclosing}
}

// Define binds a value in this environment only.
//
// In the global environment it creates or replaces a named binding: Lox allows
// global redeclaration, which is especially convenient in a long-lived REPL.
//
// In a local environment it appends, and that is what makes the resolver's slot
// indices line up. The resolver numbered this scope's declarations in source
// order; a scope's declarations execute in that same order, at most once each,
// because Lox has no way to declare a variable conditionally and a redeclared
// local is a static error. So the n-th Define here is the n-th declaration
// there.
func (e *Environment) Define(name string, value any) {
	if e.Enclosing == nil {
		if e.values == nil {
			e.values = make(map[string]any)
		}
		e.values[name] = value
		return
	}
	e.slots = append(e.slots, value)
}

// Get reads a binding by name, which is what an unresolved use needs — and an
// unresolved use is global by definition. It still walks Enclosing so a caller
// holding a local environment reaches the globals behind it; the local
// environments on the way hold no names, so they match nothing.
func (e *Environment) Get(name token.Token) (any, *errors.RuntimeError) {
	if value, ok := e.values[name.Lexeme]; ok {
		return value, nil
	}
	if e.Enclosing != nil {
		return e.Enclosing.Get(name)
	}
	return nil, undefinedVariable(name)
}

// Assign updates the nearest existing binding. Unlike Define, it never
// creates a variable implicitly.
func (e *Environment) Assign(name token.Token, value any) *errors.RuntimeError {
	if _, ok := e.values[name.Lexeme]; ok {
		e.values[name.Lexeme] = value
		return nil
	}
	if e.Enclosing != nil {
		return e.Enclosing.Assign(name, value)
	}
	return undefinedVariable(name)
}

// ancestor returns the environment exactly distance hops up the chain. The
// walk is unconditional because the resolver counted the hops from the syntax,
// so a shorter chain would be a bug in this interpreter, not bad Lox.
func (e *Environment) ancestor(distance int) *Environment {
	environment := e
	for range distance {
		environment = environment.Enclosing
	}
	return environment
}

// GetAt reads the binding the resolver located: distance environments up, at
// index within that one. No search, no name comparison, and no not-found case —
// that is the payoff of the pass rather than an oversight. An index out of range
// would mean the resolver and the interpreter disagree about the shape of a
// scope, which is a bug in this interpreter and not bad Lox, so the panic it
// produces is the right outcome. Get remains for globals.
func (e *Environment) GetAt(distance, index int) any {
	return e.ancestor(distance).slots[index]
}

// AssignAt is the write half of GetAt and skips the same search. It cannot
// create a variable by accident: the slot exists because the resolver found a
// declaration for it.
func (e *Environment) AssignAt(distance, index int, value any) {
	e.ancestor(distance).slots[index] = value
}

func undefinedVariable(name token.Token) *errors.RuntimeError {
	return &errors.RuntimeError{Token: name, Message: "Undefined variable '" + name.Lexeme + "'."}
}
