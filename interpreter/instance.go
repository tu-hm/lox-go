package interpreter

import (
	"compiler101/lexer/token"
	"compiler101/pkg/errors"
)

// propertyOwner is anything `.` works on. Two types satisfy it: an instance,
// and — since the class-methods challenge — a class, because static methods and
// static fields are reached through the same syntax as everything else.
//
// The interpreter switches on this interface rather than on *LoxInstance, which
// is what keeps VisitGetExpr from having to know the two cases apart.
type propertyOwner interface {
	get(interpreter *Interpreter, name token.Token) (any, *errors.RuntimeError)
	set(name token.Token, value any)
}

var (
	_ propertyOwner = (*LoxInstance)(nil)
	_ propertyOwner = (*LoxClass)(nil)
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

func (o *LoxInstance) get(interpreter *Interpreter, name token.Token) (any, *errors.RuntimeError) {
	return lookUpProperty(interpreter, o, o.fields, o.class.findMethod, name)
}

// set always succeeds. There is no such thing as an undefined field to assign
// to, because assigning is how a field comes to exist.
func (o *LoxInstance) set(name token.Token, value any) {
	o.fields[name.Lexeme] = value
}

func (o *LoxInstance) String() string { return o.class.name + " instance" }

// lookUpProperty is the fields-then-methods rule, in one place because both
// instances and classes obey it.
//
// The order is the language design decision the chapter dwells on. A field
// shadows a method of the same name, which means `object.method` may return
// something that is not the method — and that is deliberate, because a field
// holding a function has to be callable the same way.
//
// receiver is what a found method binds to: the instance for an instance
// method, the class itself for a class method. findMethod is a function rather
// than a map so each caller decides where methods come from — and so chapter
// 13 can walk a superclass chain without changing this.
//
// The interpreter is a parameter because of the getters challenge: a getter's
// body has to run right here, during the property read, so this needs something
// to run it with. Nothing else about property lookup uses it.
//
// It returns an error rather than panicking with a runtime signal, mirroring
// Environment.Get: the caller in the interpreter owns the unwinding.
func lookUpProperty(
	interpreter *Interpreter,
	receiver any,
	fields map[string]any,
	findMethod func(string) *LoxFunction,
	name token.Token,
) (any, *errors.RuntimeError) {
	if value, ok := fields[name.Lexeme]; ok {
		return value, nil
	}
	if method := findMethod(name.Lexeme); method != nil {
		// Binding here, at access time, is what makes a method reference a
		// closure over its receiver: the returned function keeps working after
		// the expression that found it is gone.
		bound := method.bind(receiver)
		if method.declaration.IsGetter {
			// A getter is the one property whose read is a call. It happens
			// after the field check, so a field of the same name still wins —
			// the same rule, not an exception to it.
			return bound.Call(interpreter, nil), nil
		}
		return bound, nil
	}
	return nil, &errors.RuntimeError{
		Token:   name,
		Message: "Undefined property '" + name.Lexeme + "'.",
	}
}
