package interpreter

import (
	"compiler101/lexer/token"
	"compiler101/pkg/errors"
)

// LoxClass is the runtime form of a class declaration. It is Callable because
// in Lox the class object is the constructor: `Breakfast()` calls the class.
//
// It is also a propertyOwner, which is the class-methods challenge: a class has
// properties of its own, entirely separate from its instances'. `methods` is
// what instances find; `classMethods` and `fields` are what the class itself
// does.
//
// The book's hint is to make LoxClass extend LoxInstance, so a class becomes an
// instance of a metaclass and `Math.square` needs no new lookup path. That does
// not translate: Go embedding is not subtyping, so a promoted method receives
// the *embedded* value, and `this` inside a class method would be bound to a
// hidden inner LoxInstance rather than to the class. Two small maps and a
// shared lookUpProperty get the same behaviour without the indirection — and
// `this` inside a class method is the class, which was the point of the hint.
type LoxClass struct {
	name         string
	methods      map[string]*LoxFunction
	classMethods map[string]*LoxFunction
	fields       map[string]any
}

var _ Callable = (*LoxClass)(nil)

func newLoxClass(name string, methods, classMethods map[string]*LoxFunction) *LoxClass {
	return &LoxClass{
		name:         name,
		methods:      methods,
		classMethods: classMethods,
		fields:       make(map[string]any),
	}
}

// findMethod returns nil rather than an error: the caller decides whether a
// missing method is a missing property or just a class without an initializer.
func (c *LoxClass) findMethod(name string) *LoxFunction {
	return c.methods[name]
}

// findClassMethod is the same lookup over the other map. Keeping the two apart
// is what makes `C.instanceMethod` an error rather than an unbound function.
func (c *LoxClass) findClassMethod(name string) *LoxFunction {
	return c.classMethods[name]
}

// get and set make the class itself a property owner. A class method binds
// `this` to the class, so one class method can call another through `this`.
func (c *LoxClass) get(interpreter *Interpreter, name token.Token) (any, *errors.RuntimeError) {
	return lookUpProperty(interpreter, c, c.fields, c.findClassMethod, name)
}

func (c *LoxClass) set(name token.Token, value any) {
	c.fields[name.Lexeme] = value
}

// Arity is the initializer's, because that is what the call actually passes its
// arguments to. A class with no init() takes none.
func (c *LoxClass) Arity() int {
	if initializer := c.findMethod("init"); initializer != nil {
		return initializer.Arity()
	}
	return 0
}

// Call constructs an instance and, if the class declares one, runs the
// initializer bound to it. The instance is returned either way: an initializer
// exists to configure the new object, not to choose what construction produces.
func (c *LoxClass) Call(interpreter *Interpreter, arguments []any) any {
	instance := newLoxInstance(c)
	if initializer := c.findMethod("init"); initializer != nil {
		initializer.bind(instance).Call(interpreter, arguments)
	}
	return instance
}

func (c *LoxClass) String() string { return c.name }
