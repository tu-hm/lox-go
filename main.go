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
)

// options is what the flags add up to: which parsing algorithm runs, and how
// much of the pipeline to show.
type options struct {
	parser parser.Config
	tokens bool // stop after the scanner and dump the token stream
	show   string
}

func Run(source string, opt options) {
	tokens := lexer.Lex(source)

	if opt.tokens {
		for _, t := range tokens {
			fmt.Println(t)
		}
		return
	}

	p, err := parser.NewOf(opt.parser, tokens)
	if err != nil {
		// A bad -parser or -k, not bad source. Nothing to recover from.
		fmt.Fprintln(os.Stderr, err)
		os.Exit(64) // EX_USAGE
	}
	interp := interpreter.New()

	// A ';' means the source is a batch of statements, so parse it as one and
	// report every error it contains. Without one it is a single expression —
	// the REPL case, where demanding a terminator would be noise.
	if containsSemicolon(tokens) {
		exprs, _ := p.ParseAll()
		for _, expr := range exprs {
			emit(interp, expr, opt.show)
			if errors.HadRuntimeError {
				break
			}
		}
		return
	}

	expr, err := p.Parse()
	if err != nil {
		return // already reported through pkg/errors
	}
	emit(interp, expr, opt.show)
}

// emit prints one parsed expression using the requested view. Evaluation is
// the default; AST and RPN output remain available for debugging the parser.
func emit(interp *interpreter.Interpreter, expr ast.Expr, show string) {
	switch show {
	case "ast":
		fmt.Println((&ast.Printer{}).Print(expr))
	case "rpn":
		fmt.Println((&ast.RPNPrinter{}).Print(expr))
	default:
		value, err := interp.Interpret(expr)
		var rte *errors.RuntimeError
		if goerrors.As(err, &rte) {
			errors.ReportRuntimeError(rte)
			return
		}
		fmt.Println(ast.Stringify(value))
	}
}

func containsSemicolon(tokens []token.Token) bool {
	for _, t := range tokens {
		if t.Type == token.SEMICOLON {
			return true
		}
	}
	return false
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
	for {
		fmt.Print("> ")

		text, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println()
			return
		}

		Run(text, opt)

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
