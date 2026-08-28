package interpreter

// LoxClass is the runtime form of a class declaration. It is Callable because
// in Lox the class object is the constructor: `Breakfast()` calls the class.
type LoxClass struct {
	name    string
	methods map[string]*LoxFunction
}

var _ Callable = (*LoxClass)(nil)

func newLoxClass(name string, methods map[string]*LoxFunction) *LoxClass {
	return &LoxClass{name: name, methods: methods}
}

// findMethod returns nil rather than an error: the caller decides whether a
// missing method is a missing property or just a class without an initializer.
func (c *LoxClass) findMethod(name string) *LoxFunction {
	return c.methods[name]
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
