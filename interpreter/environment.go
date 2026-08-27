package interpreter

import (
	"compiler101/lexer/token"
	"compiler101/pkg/errors"
)

// Environment stores the bindings for one lexical scope. Enclosing links a
// local scope to its parent; the global environment has no enclosing scope.
type Environment struct {
	Enclosing *Environment
	values    map[string]any
}

func NewEnvironment(enclosing *Environment) *Environment {
	return &Environment{Enclosing: enclosing, values: make(map[string]any)}
}

// Define creates or replaces a binding in this environment only. Lox allows
// redeclaration, which is especially convenient in a long-lived REPL.
func (e *Environment) Define(name string, value any) {
	if e.values == nil {
		e.values = make(map[string]any)
	}
	e.values[name] = value
}

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

// GetAt reads a binding the resolver already located. The missing not-found
// case is the payoff of the resolver pass, not an oversight: the name is known
// to live in that exact scope, so there is nothing to search and no error to
// report. Get remains for globals, whose scope is never resolved statically.
func (e *Environment) GetAt(distance int, name string) any {
	return e.ancestor(distance).values[name]
}

// AssignAt is the write half of GetAt and skips the same search. It cannot
// create a variable by accident: the resolver found an existing declaration at
// this distance, which is why the assignment was given one.
func (e *Environment) AssignAt(distance int, name string, value any) {
	e.ancestor(distance).Define(name, value)
}

func undefinedVariable(name token.Token) *errors.RuntimeError {
	return &errors.RuntimeError{Token: name, Message: "Undefined variable '" + name.Lexeme + "'."}
}
