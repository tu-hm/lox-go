#include <stdio.h>
#include <string.h>

#include "common.h"
#include "scanner.h"

// MAX_INTERPOLATION is the only bound the scanner has, and challenge 1 is what
// introduced it. Before interpolation the scanner needed no memory at all beyond
// two pointers and a line number -- see the "what the scanner refuses to know"
// list in docs/front-end.md. Nesting means a '}' has to know which construct it
// closes, and that is a stack.
//
// Eight is a limit rather than a growable array on purpose: a string nested
// eight interpolations deep is not code anyone meant to write, and reporting it
// is more useful than supporting it.
#define MAX_INTERPOLATION 8

typedef struct {
  const char* start;   // the first character of the token being scanned
  const char* current; // the character being looked at
  int line;

  // One entry per open interpolation, counting the '{' seen inside it that are
  // still open. A '}' ends the interpolation when its count is zero and closes
  // an ordinary block otherwise.
  int braces[MAX_INTERPOLATION];
  int interpolation;
} Scanner;

Scanner scanner;

void initScanner(const char* source) {
  scanner.start = source;
  scanner.current = source;
  scanner.line = 1;
  scanner.interpolation = 0;
}

static bool isAlpha(char c) {
  return (c >= 'a' && c <= 'z') ||
         (c >= 'A' && c <= 'Z') ||
          c == '_';
}

static bool isDigit(char c) {
  return c >= '0' && c <= '9';
}

static bool isAtEnd(void) {
  return *scanner.current == '\0';
}

static char advance(void) {
  scanner.current++;
  return scanner.current[-1];
}

static char peek(void) {
  return *scanner.current;
}

static char peekNext(void) {
  if (isAtEnd()) return '\0';
  return scanner.current[1];
}

static bool match(char expected) {
  if (isAtEnd()) return false;
  if (*scanner.current != expected) return false;
  scanner.current++;
  return true;
}

static Token makeToken(TokenType type) {
  Token token;
  token.type = type;
  token.start = scanner.start;
  token.length = (int)(scanner.current - scanner.start);
  token.line = scanner.line;
  return token;
}

// errorToken is the whole difference between this scanner and the Go one. The
// Go scanner calls errors.Error, sets a package-global HadError, and returns a
// token slice the caller is separately told not to trust. This returns the
// error *as a token*, in the stream, in position -- so the consumer cannot
// forget to check, and the error arrives with the rest of the input rather than
// beside it.
//
// The cost is the cast in the struct: start points at a string literal here
// instead of into the source, so nothing may compute a source offset from a
// TOKEN_ERROR. Chapter 17's errorAtCurrent relies on exactly that distinction.
static Token errorToken(const char* message) {
  Token token;
  token.type = TOKEN_ERROR;
  token.start = message;
  token.length = (int)strlen(message);
  token.line = scanner.line;
  return token;
}

static void skipWhitespace(void) {
  for (;;) {
    char c = peek();
    switch (c) {
      case ' ':
      case '\r':
      case '\t':
        advance();
        break;
      case '\n':
        scanner.line++;
        advance();
        break;
      case '/':
        // A comment is whitespace, which is why it is skipped here rather than
        // scanned as a token that is later thrown away. Two characters of
        // lookahead, because '/' is also an operator.
        if (peekNext() == '/') {
          while (peek() != '\n' && !isAtEnd()) advance();
        } else {
          return;
        }
        break;
      default:
        return;
    }
  }
}

static TokenType checkKeyword(int start, int length, const char* rest,
                              TokenType type) {
  if (scanner.current - scanner.start == start + length &&
      memcmp(scanner.start + start, rest, length) == 0) {
    return type;
  }
  return TOKEN_IDENTIFIER;
}

// identifierType is a trie, hand-written as nested switches. It runs only after
// a maximal run of identifier characters has been scanned, which is the property
// that makes keywords work at all: ask "does the input start with or?" and
// `orchid` lexes as `or` followed by `chid`.
//
// The Go implementation does the same thing with a map lookup
// (lexer/scanner/scanner.go). This is the same algorithm with the hash replaced
// by the first character, and it is faster for the same reason a trie usually
// is: almost every identifier fails on its first byte and never touches memory
// beyond it.
static TokenType identifierType(void) {
  switch (scanner.start[0]) {
    case 'a': return checkKeyword(1, 2, "nd", TOKEN_AND);
    case 'c': return checkKeyword(1, 4, "lass", TOKEN_CLASS);
    case 'e': return checkKeyword(1, 3, "lse", TOKEN_ELSE);
    case 'f':
      if (scanner.current - scanner.start > 1) {
        switch (scanner.start[1]) {
          case 'a': return checkKeyword(2, 3, "lse", TOKEN_FALSE);
          case 'o': return checkKeyword(2, 1, "r", TOKEN_FOR);
          case 'u': return checkKeyword(2, 1, "n", TOKEN_FUN);
        }
      }
      break;
    case 'i': return checkKeyword(1, 1, "f", TOKEN_IF);
    case 'n': return checkKeyword(1, 2, "il", TOKEN_NIL);
    case 'o': return checkKeyword(1, 1, "r", TOKEN_OR);
    case 'p': return checkKeyword(1, 4, "rint", TOKEN_PRINT);
    case 'r': return checkKeyword(1, 5, "eturn", TOKEN_RETURN);
    case 's': return checkKeyword(1, 4, "uper", TOKEN_SUPER);
    case 't':
      if (scanner.current - scanner.start > 1) {
        switch (scanner.start[1]) {
          case 'h': return checkKeyword(2, 2, "is", TOKEN_THIS);
          case 'r': return checkKeyword(2, 2, "ue", TOKEN_TRUE);
        }
      }
      break;
    case 'v': return checkKeyword(1, 2, "ar", TOKEN_VAR);
    case 'w': return checkKeyword(1, 4, "hile", TOKEN_WHILE);
  }
  return TOKEN_IDENTIFIER;
}

static Token identifier(void) {
  while (isAlpha(peek()) || isDigit(peek())) advance();
  return makeToken(identifierType());
}

static Token number(void) {
  while (isDigit(peek())) advance();

  // A fractional part needs a digit after the dot, so `123.` scans as NUMBER
  // then DOT rather than as a malformed number. That is what makes `123.sqrt()`
  // parse, and it is why peekNext exists.
  if (peek() == '.' && isDigit(peekNext())) {
    advance();
    while (isDigit(peek())) advance();
  }

  // No strtod. The Go scanner parses here and stores a float64 on the token,
  // which is how it can report "Number literal is too large." at scan time; this
  // one has nothing to report it with and nowhere to put the answer.
  return makeToken(TOKEN_NUMBER);
}

// string scans from just past an opening delimiter to the next one. The opening
// delimiter is a quote at the start of a literal and a '}' when resuming after
// an interpolated expression, and the closing one is a quote (TOKEN_STRING) or a
// "${" (TOKEN_INTERPOLATION) -- so a segment's lexeme always includes both of
// its own delimiters, and a consumer strips one character from the front and
// either one or two from the back depending on the type.
static Token string(void) {
  for (;;) {
    if (isAtEnd()) return errorToken("Unterminated string.");

    char c = peek();
    if (c == '"') {
      advance();
      return makeToken(TOKEN_STRING);
    }

    if (c == '$' && peekNext() == '{') {
      if (scanner.interpolation == MAX_INTERPOLATION) {
        return errorToken("Interpolation nested too deeply.");
      }
      advance(); // $
      advance(); // {
      scanner.braces[scanner.interpolation++] = 0;
      return makeToken(TOKEN_INTERPOLATION);
    }

    // Lox strings are multi-line, so a newline is an ordinary character here and
    // the line counter has to be maintained by hand.
    if (c == '\n') scanner.line++;
    advance();
  }
}

Token scanToken(void) {
  skipWhitespace();
  scanner.start = scanner.current;

  if (isAtEnd()) return makeToken(TOKEN_EOF);

  char c = advance();
  if (isAlpha(c)) return identifier();
  if (isDigit(c)) return number();

  switch (c) {
    case '(': return makeToken(TOKEN_LEFT_PAREN);
    case ')': return makeToken(TOKEN_RIGHT_PAREN);
    case '{':
      // Inside an interpolation, an ordinary '{' has to be counted, or the '}'
      // that matches it would be read as the end of the interpolation.
      if (scanner.interpolation > 0) scanner.braces[scanner.interpolation - 1]++;
      return makeToken(TOKEN_LEFT_BRACE);
    case '}':
      if (scanner.interpolation > 0 &&
          scanner.braces[scanner.interpolation - 1] == 0) {
        // This '}' closes an interpolation rather than a block, so the string it
        // interrupted resumes here. The token that comes back covers from this
        // '}' to the next "${" or closing quote.
        scanner.interpolation--;
        return string();
      }
      if (scanner.interpolation > 0) scanner.braces[scanner.interpolation - 1]--;
      return makeToken(TOKEN_RIGHT_BRACE);
    case ';': return makeToken(TOKEN_SEMICOLON);
    case ',': return makeToken(TOKEN_COMMA);
    case '.': return makeToken(TOKEN_DOT);
    case '-': return makeToken(TOKEN_MINUS);
    case '+': return makeToken(TOKEN_PLUS);
    case '/': return makeToken(TOKEN_SLASH);
    case '*': return makeToken(TOKEN_STAR);
    case '!':
      return makeToken(match('=') ? TOKEN_BANG_EQUAL : TOKEN_BANG);
    case '=':
      return makeToken(match('=') ? TOKEN_EQUAL_EQUAL : TOKEN_EQUAL);
    case '<':
      return makeToken(match('=') ? TOKEN_LESS_EQUAL : TOKEN_LESS);
    case '>':
      return makeToken(match('=') ? TOKEN_GREATER_EQUAL : TOKEN_GREATER);
    case '"': return string();
  }

  return errorToken("Unexpected character.");
}

const char* tokenTypeName(TokenType type) {
  switch (type) {
    case TOKEN_LEFT_PAREN:    return "LEFT_PAREN";
    case TOKEN_RIGHT_PAREN:   return "RIGHT_PAREN";
    case TOKEN_LEFT_BRACE:    return "LEFT_BRACE";
    case TOKEN_RIGHT_BRACE:   return "RIGHT_BRACE";
    case TOKEN_COMMA:         return "COMMA";
    case TOKEN_DOT:           return "DOT";
    case TOKEN_MINUS:         return "MINUS";
    case TOKEN_PLUS:          return "PLUS";
    case TOKEN_SEMICOLON:     return "SEMICOLON";
    case TOKEN_SLASH:         return "SLASH";
    case TOKEN_STAR:          return "STAR";
    case TOKEN_BANG:          return "BANG";
    case TOKEN_BANG_EQUAL:    return "BANG_EQUAL";
    case TOKEN_EQUAL:         return "EQUAL";
    case TOKEN_EQUAL_EQUAL:   return "EQUAL_EQUAL";
    case TOKEN_GREATER:       return "GREATER";
    case TOKEN_GREATER_EQUAL: return "GREATER_EQUAL";
    case TOKEN_LESS:          return "LESS";
    case TOKEN_LESS_EQUAL:    return "LESS_EQUAL";
    case TOKEN_IDENTIFIER:    return "IDENTIFIER";
    case TOKEN_STRING:        return "STRING";
    case TOKEN_NUMBER:        return "NUMBER";
    case TOKEN_INTERPOLATION: return "INTERPOLATION";
    case TOKEN_AND:           return "AND";
    case TOKEN_CLASS:         return "CLASS";
    case TOKEN_ELSE:          return "ELSE";
    case TOKEN_FALSE:         return "FALSE";
    case TOKEN_FOR:           return "FOR";
    case TOKEN_FUN:           return "FUN";
    case TOKEN_IF:            return "IF";
    case TOKEN_NIL:           return "NIL";
    case TOKEN_OR:            return "OR";
    case TOKEN_PRINT:         return "PRINT";
    case TOKEN_RETURN:        return "RETURN";
    case TOKEN_SUPER:         return "SUPER";
    case TOKEN_THIS:          return "THIS";
    case TOKEN_TRUE:          return "TRUE";
    case TOKEN_VAR:           return "VAR";
    case TOKEN_WHILE:         return "WHILE";
    case TOKEN_ERROR:         return "ERROR";
    case TOKEN_EOF:           return "EOF";
  }
  return "?";
}
