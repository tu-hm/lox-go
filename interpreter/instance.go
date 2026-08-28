package interpreter

import (
	"compiler101/lexer/token"
	"compiler101/pkg/errors"
)

// LoxInstance is the runtime form of an object: the class it was made from,
// plus whatever fields have been written to it.
//
// Fields are a map and not slots, unlike a local scope. The resolver can number
// a scope's declarations because the source lists them; it cannot number an
// instance's fields, because Lox never declares them. `object.field = value`
// creates the field, and which fields exist depends on which lines ran.
type LoxInstance struct {
	class *LoxClass
	// fields is created eagerly. A nil map reads fine but panics on write, and
	// the first write is the common case here.
	fields map[string]any
}

func newLoxInstance(class *LoxClass) *LoxInstance {
	return &LoxInstance{class: class, fields: make(map[string]any)}
}

// get resolves a property: fields first, then the class's methods.
//
// The order is the language design decision the chapter dwells on. A field
// shadows a method of the same name, which means `object.method` may return
// something that is not the method — and that is deliberate, because a field
// holding a function has to be callable the same way.
//
// It returns an error rather than panicking with a runtime signal, mirroring
// Environment.Get: the caller in the interpreter owns the unwinding.
func (o *LoxInstance) get(name token.Token) (any, *errors.RuntimeError) {
	if value, ok := o.fields[name.Lexeme]; ok {
		return value, nil
	}
	if method := o.class.findMethod(name.Lexeme); method != nil {
		// Binding here, at access time, is what makes a method reference a
		// closure over its receiver: the returned function keeps working after
		// the expression that found it is gone.
		return method.bind(o), nil
	}
	return nil, &errors.RuntimeError{
		Token:   name,
		Message: "Undefined property '" + name.Lexeme + "'.",
	}
}

// set always succeeds. There is no such thing as an undefined field to assign
// to, because assigning is how a field comes to exist.
func (o *LoxInstance) set(name token.Token, value any) {
	o.fields[name.Lexeme] = value
}

func (o *LoxInstance) String() string { return o.class.name + " instance" }
