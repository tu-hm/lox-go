package ast

import "strings"

// ExprFormatter is the common surface of Printer and RPNPrinter. StmtPrinter
// uses it so the CLI can keep both debug views after programs replace bare
// expressions as the parser's normal output.
type ExprFormatter interface {
	Print(Expr) string
}

// StmtPrinter renders statement structure while delegating expression syntax
// to either the parenthesized or RPN expression printer.
type StmtPrinter struct {
	expressions ExprFormatter
}

var _ StmtVisitor = (*StmtPrinter)(nil)

func NewStmtPrinter(expressions ExprFormatter) *StmtPrinter {
	return &StmtPrinter{expressions: expressions}
}

func (p *StmtPrinter) Print(stmt Stmt) string {
	s, _ := stmt.Accept(p).(string)
	return s
}

func (p *StmtPrinter) PrintProgram(statements []Stmt) string {
	lines := make([]string, 0, len(statements))
	for _, statement := range statements {
		lines = append(lines, p.Print(statement))
	}
	return strings.Join(lines, "\n")
}

func (p *StmtPrinter) VisitBlockStmt(stmt *Block) any {
	parts := make([]string, 0, len(stmt.Statements)+1)
	parts = append(parts, "block")
	for _, statement := range stmt.Statements {
		parts = append(parts, p.Print(statement))
	}
	return "(" + strings.Join(parts, " ") + ")"
}

func (p *StmtPrinter) VisitExpressionStmt(stmt *Expression) any {
	return "(expr " + p.expressions.Print(stmt.Expression) + ")"
}

func (p *StmtPrinter) VisitFunctionStmt(stmt *Function) any {
	params := make([]string, len(stmt.Params))
	for index, param := range stmt.Params {
		params[index] = param.Lexeme
	}
	body := p.VisitBlockStmt(&Block{Statements: stmt.Body}).(string)
	return "(fun " + stmt.Name.Lexeme + " (" + strings.Join(params, " ") + ") " + body + ")"
}

func (p *StmtPrinter) VisitIfStmt(stmt *If) any {
	printed := "(if " + p.expressions.Print(stmt.Condition) + " " + p.Print(stmt.ThenBranch)
	if stmt.ElseBranch != nil {
		printed += " " + p.Print(stmt.ElseBranch)
	}
	return printed + ")"
}

func (p *StmtPrinter) VisitPrintStmt(stmt *Print) any {
	return "(print " + p.expressions.Print(stmt.Expression) + ")"
}

func (p *StmtPrinter) VisitReturnStmt(stmt *Return) any {
	if stmt.Value == nil {
		return "(return)"
	}
	return "(return " + p.expressions.Print(stmt.Value) + ")"
}

func (p *StmtPrinter) VisitVarStmt(stmt *Var) any {
	if stmt.Initializer == nil {
		return "(var " + stmt.Name.Lexeme + ")"
	}
	return "(var " + stmt.Name.Lexeme + " " + p.expressions.Print(stmt.Initializer) + ")"
}

func (p *StmtPrinter) VisitWhileStmt(stmt *While) any {
	return "(while " + p.expressions.Print(stmt.Condition) + " " + p.Print(stmt.Body) + ")"
}
