package main

import (
	"bufio"
	goerrors "errors"
	"flag"
	"fmt"
	"os"

	"compiler101/ast"
	"compiler101/interpreter"
	"compiler101/lexer"
	"compiler101/lexer/token"
	"compiler101/parser"
	"compiler101/parser/llk"
	"compiler101/pkg/errors"
	"compiler101/resolver"
)

// options is what the flags add up to: which parsing algorithm runs, and how
// much of the pipeline to show.
type options struct {
	parser parser.Config
	tokens bool // stop after the scanner and dump the token stream
	show   string
}

func Run(source string, opt options) {
	runSource(source, opt, interpreter.New(), false)
}

// runSource is shared by scripts and the REPL. A caller-owned interpreter is
// what lets bindings survive from one REPL line to the next.
func runSource(source string, opt options, interp *interpreter.Interpreter, repl bool) {
	tokens := lexer.Lex(source)

	if opt.tokens {
		for _, t := range tokens {
			fmt.Println(t)
		}
		return
	}
	if errors.HadError {
		return // scanner already reported the malformed source
	}

	p, err := parser.NewOf(opt.parser, tokens)
	if err != nil {
		// A bad -parser or -k, not bad source. Nothing to recover from.
		fmt.Fprintln(os.Stderr, err)
		os.Exit(64) // EX_USAGE
	}

	// Preserve the expression-friendly REPL from the chapter challenge. Scripts
	// always parse as programs; a REPL line without statement syntax may omit
	// its semicolon and prints the resulting value automatically.
	if repl && isBareExpression(tokens) {
		expr, err := p.Parse()
		if err != nil {
			return // already reported through pkg/errors
		}
		emitExpression(interp, expr, opt.show)
		return
	}

	statements, parseErrors := p.ParseProgram()
	if len(parseErrors) != 0 || errors.HadError {
		return
	}
	emitProgram(interp, statements, opt.show)
}

func emitExpression(interp *interpreter.Interpreter, expr ast.Expr, show string) {
	switch show {
	case "ast":
		fmt.Println((&ast.Printer{}).Print(expr))
	case "rpn":
		fmt.Println((&ast.RPNPrinter{}).Print(expr))
	default:
		// No resolution step: an expression cannot declare anything, so a bare
		// REPL line has no local scopes and every name in it is global.
		value, err := interp.Interpret(expr)
		if reportRuntimeError(err) {
			return
		}
		fmt.Println(ast.Stringify(value))
	}
}

func emitProgram(interp *interpreter.Interpreter, statements []ast.Stmt, show string) {
	var expressions ast.ExprFormatter
	switch show {
	case "ast":
		expressions = &ast.Printer{}
	case "rpn":
		expressions = &ast.RPNPrinter{}
	default:
		// Warnings are already on stderr and do not stop the program; a
		// static error does, because nothing has run yet.
		if errs, _ := resolver.Resolve(interp, statements); len(errs) != 0 {
			return // already reported through pkg/errors
		}
		reportRuntimeError(interp.Execute(statements))
		return
	}

	printed := ast.NewStmtPrinter(expressions).PrintProgram(statements)
	if printed != "" {
		fmt.Println(printed)
	}
}

func reportRuntimeError(err error) bool {
	var rte *errors.RuntimeError
	if goerrors.As(err, &rte) {
		errors.ReportRuntimeError(rte)
		return true
	}
	return false
}

func isBareExpression(tokens []token.Token) bool {
	for _, t := range tokens {
		if t.Type == token.SEMICOLON {
			return false
		}
	}
	if len(tokens) == 0 {
		return true
	}
	switch tokens[0].Type {
	case token.CLASS, token.FUN, token.PRINT, token.RETURN, token.VAR,
		token.LEFT_BRACE, token.FOR, token.IF, token.WHILE:
		return false
	default:
		return true
	}
}

func RunFile(path string, opt options) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(74) // EX_IOERR
	}

	Run(string(bytes), opt)

	if errors.HadError {
		os.Exit(65) // EX_DATAERR
	}
	if errors.HadRuntimeError {
		os.Exit(70) // EX_SOFTWARE
	}
}

// RunPrompt is the interpreter REPL.
func RunPrompt(opt options) {
	reader := bufio.NewReader(os.Stdin)
	interp := interpreter.New()
	for {
		fmt.Print("> ")

		text, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println()
			return
		}

		runSource(text, opt, interp, true)

		errors.Reset()
	}
}

// printSampleAST is the chapter 5 deliverable: a hand-built tree for
// -123 * (45.67), rendered by both visitors. It predates the parser and is kept
// because it is the one output that depends on no parsing at all — if the
// printers are wrong, this shows it without a parser in the way.
func printSampleAST() {
	expr := &ast.Binary{
		Left: &ast.Unary{
			Operator: token.Token{Type: token.MINUS, Lexeme: "-", Line: 1},
			Right:    &ast.Literal{Value: 123.0},
		},
		Operator: token.Token{Type: token.STAR, Lexeme: "*", Line: 1},
		Right: &ast.Grouping{
			Expression: &ast.Literal{Value: 45.67},
		},
	}

	fmt.Println("source :  -123 * (45.67)")
	fmt.Println("ast    : ", (&ast.Printer{}).Print(expr))
	fmt.Println("rpn    : ", (&ast.RPNPrinter{}).Print(expr))
}

func main() {
	showSample := flag.Bool("sample", false, "print a hand-built sample AST and exit")
	showTokens := flag.Bool("tokens", false, "print the token stream instead of parsing")
	show := flag.String("print", "eval", "what to print: eval, ast, or rpn")
	algorithm := flag.String("parser", "rd", "parsing algorithm: rd (recursive descent) or llk (table-driven LL(k))")
	k := flag.Int("k", 0, fmt.Sprintf("lookahead for -parser=llk, %d..%d (default %d)", llk.MinK, llk.MaxK, llk.DefaultK))
	flag.Parse()

	if *showSample {
		printSampleAST()
		return
	}

	kind, err := parser.ParseKind(*algorithm)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(64) // EX_USAGE
	}
	opt := options{
		parser: parser.Config{Kind: kind, K: *k},
		tokens: *showTokens,
		show:   *show,
	}

	// flag.Args() is os.Args with the program name and any flags removed.
	args := flag.Args()

	switch {
	case len(args) > 1:
		fmt.Fprintln(os.Stderr, "Usage: glox [-parser=rd|llk] [-k=n] [-print=eval|ast|rpn] [script]")
		os.Exit(64) // EX_USAGE
	case len(args) == 1:
		RunFile(args[0], opt)
	default:
		RunPrompt(opt)
	}
}
