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
	name string
	// superclass is nil for a root class. Inheritance is one pointer and two
	// fall-throughs below: a subclass copies nothing, so a method added to a
	// superclass at declaration time is found by subclasses already built.
	superclass   *LoxClass
	methods      map[string]*LoxFunction
	classMethods map[string]*LoxFunction
	fields       map[string]any
}

var _ Callable = (*LoxClass)(nil)

func newLoxClass(name string, superclass *LoxClass, methods, classMethods map[string]*LoxFunction) *LoxClass {
	return &LoxClass{
		name:         name,
		superclass:   superclass,
		methods:      methods,
		classMethods: classMethods,
		fields:       make(map[string]any),
	}
}

// findMethod walks up the chain and returns nil rather than an error: the
// caller decides whether a missing method is a missing property or just a class
// without an initializer.
//
// Own methods are checked first, which is the whole of overriding — and because
// `init` is found this way too, a subclass that declares none inherits its
// superclass's constructor, arity included.
func (c *LoxClass) findMethod(name string) *LoxFunction {
	if method, ok := c.methods[name]; ok {
		return method
	}
	if c.superclass != nil {
		return c.superclass.findMethod(name)
	}
	return nil
}

// findClassMethod is the same walk over the other map. Keeping the two apart is
// what makes `C.instanceMethod` an error rather than an unbound function;
// walking both is what makes a class method inherited like any other.
//
// Static *fields* are deliberately not inherited: they live in c.fields, which
// this does not touch. A class's fields are its own for the same reason an
// instance's are — inheriting a mutable slot would give two classes one
// variable.
func (c *LoxClass) findClassMethod(name string) *LoxFunction {
	if method, ok := c.classMethods[name]; ok {
		return method
	}
	if c.superclass != nil {
		return c.superclass.findClassMethod(name)
	}
	return nil
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
