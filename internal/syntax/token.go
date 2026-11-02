package syntax

// tokenKind identifies a lexical token of the Gom grammar
// (docs/grammar.ebnf section 2.4). NL is the single virtual token:
// it is not produced by scanning but inserted afterwards by the
// newline rules of grammar.ebnf section 3. The newlineMarker kind is
// an internal bookkeeping token for a candidate newline; it never
// reaches the parser.
type tokenKind uint8

const (
	tokEOF     tokenKind = iota
	tokNewline           // newline candidate (removed by NL insertion)
	tokNL                // virtual newline separator

	tokIdent  // lowercase identifier (also "_" + name)
	tokUIdent // uppercase identifier

	// literals
	tokInt
	tokFloat
	tokChar
	tokString // complete string literal without interpolation

	// interpolated-string parts: '"' , parts , '"'
	tokStrOpen     // opening '"' of a string containing "${"
	tokStrText     // a literal chunk inside an interpolated string
	tokInterpStart // "${"
	tokInterpEnd   // "}" closing a "${"
	tokStrClose    // closing '"' of an interpolated string

	// keywords (grammar.ebnf section 2.2)
	tokAs
	tokDefault
	tokElse
	tokExtern
	tokFalse
	tokFn
	tokFor
	tokGo
	tokIf
	tokIgnore
	tokImport
	tokIn
	tokLet
	tokMatch
	tokPackage
	tokSelect
	tokSend
	tokTest
	tokTrue
	tokTry
	tokType
	tokWith

	// punctuation and operators (grammar.ebnf section 2.4)
	tokLParen   // (
	tokRParen   // )
	tokLBrack   // [
	tokRBrack   // ]
	tokLBrace   // {
	tokRBrace   // }
	tokComma    // ,
	tokSemi     // ;
	tokColon    // :
	tokDot      // .
	tokDotDot   // ..
	tokTilde    // ~
	tokAt       // @
	tokEq       // =
	tokArrow    // ->
	tokLArrow   // <-
	tokBar      // |
	tokPipeFwd  // |>
	tokPipeBack // "<" + "|"
	tokPipeTry  // |?>
	tokComposeF // >>
	tokComposeB // <<
	tokOr       // ||
	tokAnd      // &&
	tokEqEq     // ==
	tokNe       // !=
	tokLt       // <
	tokLe       // <=
	tokGt       // >
	tokGe       // >=
	tokPlus     // +
	tokMinus    // -
	tokStar     // *
	tokSlash    // /
	tokPercent  // %
	tokUnder    // _
)

// keywords maps the reserved words of grammar.ebnf section 2.2 to
// their token kinds.
var keywords = map[string]tokenKind{
	"as":      tokAs,
	"default": tokDefault,
	"else":    tokElse,
	"extern":  tokExtern,
	"false":   tokFalse,
	"fn":      tokFn,
	"for":     tokFor,
	"go":      tokGo,
	"if":      tokIf,
	"ignore":  tokIgnore,
	"import":  tokImport,
	"in":      tokIn,
	"let":     tokLet,
	"match":   tokMatch,
	"package": tokPackage,
	"select":  tokSelect,
	"send":    tokSend,
	"test":    tokTest,
	"true":    tokTrue,
	"try":     tokTry,
	"type":    tokType,
	"with":    tokWith,
}

// IsKeyword reports whether s is a reserved word of grammar.ebnf
// section 2.2.
func IsKeyword(s string) bool {
	_, ok := keywords[s]
	return ok
}

// spellings gives a printable name for every token kind, used in
// diagnostic messages. Operator spellings are quoted so that
// "expected ->" reads like the source. The "<" + "|" concatenation
// keeps the two-character operator out of comments that tooling
// might mangle.
var spellings = map[tokenKind]string{
	tokEOF:         "end of file",
	tokNL:          "newline",
	tokIdent:       "identifier",
	tokUIdent:      "identifier",
	tokInt:         "integer literal",
	tokFloat:       "float literal",
	tokChar:        "character literal",
	tokString:      "string literal",
	tokStrOpen:     "\"",
	tokStrText:     "string text",
	tokInterpStart: "\"${\"",
	tokInterpEnd:   "\"}\"",
	tokStrClose:    "\"",
	tokLParen:      "\"(\"",
	tokRParen:      "\")\"",
	tokLBrack:      "\"[\"",
	tokRBrack:      "\"]\"",
	tokLBrace:      "\"{\"",
	tokRBrace:      "\"}\"",
	tokComma:       "\",\"",
	tokSemi:        "\";\"",
	tokColon:       "\":\"",
	tokDot:         "\".\"",
	tokDotDot:      "\"..\"",
	tokTilde:       "\"~\"",
	tokAt:          "\"@\"",
	tokEq:          "\"=\"",
	tokArrow:       "\"->\"",
	tokLArrow:      "\"<-\"",
	tokBar:         "\"|\"",
	tokPipeFwd:     "\"|>\"",
	tokPipeBack:    "\"" + "<" + "|" + "\"",
	tokPipeTry:     "\"|?>\"",
	tokComposeF:    "\">>\"",
	tokComposeB:    "\"<<\"",
	tokOr:          "\"||\"",
	tokAnd:         "\"&&\"",
	tokEqEq:        "\"==\"",
	tokNe:          "\"!=\"",
	tokLt:          "\"<\"",
	tokLe:          "\"<=\"",
	tokGt:          "\">\"",
	tokGe:          "\">=\"",
	tokPlus:        "\"+\"",
	tokMinus:       "\"-\"",
	tokStar:        "\"*\"",
	tokSlash:       "\"/\"",
	tokPercent:     "\"%\"",
	tokUnder:       "\"_\"",
	tokAs:          "\"as\"",
	tokDefault:     "\"default\"",
	tokElse:        "\"else\"",
	tokExtern:      "\"extern\"",
	tokFn:          "\"fn\"",
	tokFor:         "\"for\"",
	tokGo:          "\"go\"",
	tokIf:          "\"if\"",
	tokIgnore:      "\"ignore\"",
	tokImport:      "\"import\"",
	tokIn:          "\"in\"",
	tokLet:         "\"let\"",
	tokMatch:       "\"match\"",
	tokPackage:     "\"package\"",
	tokSelect:      "\"select\"",
	tokSend:        "\"send\"",
	tokTest:        "\"test\"",
	tokTry:         "\"try\"",
	tokType:        "\"type\"",
	tokWith:        "\"with\"",
	tokFalse:       "\"false\"",
	tokTrue:        "\"true\"",
}

// String returns a printable name of the token kind for diagnostics.
func (k tokenKind) String() string {
	if s, ok := spellings[k]; ok {
		return s
	}
	return "token"
}

// token is one lexical unit: its kind and the half-open byte range
// [start, end) it occupies in the source. The text is recovered by
// slicing the source.
type token struct {
	kind       tokenKind
	start, end int
}

// canEnd reports whether a token of this kind may END an expression
// or declaration — the PREV set of NL-2 (docs/grammar.ebnf section 3):
// identifiers, literals, "_", ")", "]", "}" plus the literals true
// and false and the closing quote of an interpolated string.
func (k tokenKind) canEnd() bool {
	switch k {
	case tokIdent, tokUIdent,
		tokInt, tokFloat, tokChar, tokString, tokStrClose,
		tokTrue, tokFalse,
		tokUnder, tokRParen, tokRBrack, tokRBrace:
		return true
	default:
		return false
	}
}

// lineStart reports whether a token of this kind may NOT begin a
// continuation line — the NEXT set of NL-2 (docs/grammar.ebnf
// section 3): binary operators, "else" and "|". A newline before one
// of these does not produce an NL.
func (k tokenKind) lineStart() bool {
	switch k {
	case tokPipeFwd, tokPipeBack, tokPipeTry,
		tokComposeF, tokComposeB,
		tokOr, tokAnd,
		tokEqEq, tokNe, tokLt, tokLe, tokGt, tokGe,
		tokPlus, tokMinus, tokStar, tokSlash, tokPercent,
		tokElse, tokBar:
		return true
	default:
		return false
	}
}

// isKeyword reports whether k is one of the keyword token kinds —
// used so that a keyword may serve as a member name after "."
// (grammar.ebnf member-name).
func (k tokenKind) isKeyword() bool {
	return k >= tokAs && k <= tokWith
}
