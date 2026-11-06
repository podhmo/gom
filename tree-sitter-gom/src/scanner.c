// External scanner for tree-sitter-gom: emits the virtual NL token of
// docs/grammar.ebnf section 3.
//
// Gom inserts a separator (NL) after a newline iff
//
//     PREV in can-end  AND  NEXT not in line-start
//
// where PREV is the last non-trivia token before the newline and NEXT the
// first non-trivia token after it (NL-2).  Inside "(", "[", "${" newlines
// are plain whitespace (NL-1); "{" keeps them significant.
//
// Both halves reduce to cheap checks here:
//
//   - NL-1 / PREV: the grammar asks for _nl only where a separator is
//     legal — between block items, match/select arms, extern items and
//     top-level declarations — i.e. only when the innermost delimiter is
//     "{" (or top level) and the previous token can end an item.  The
//     scanner never runs inside "(" "[" "${" because _nl is never valid
//     there, so no delimiter stack is needed.
//   - NEXT: after the newline the scanner skips trivia (whitespace,
//     more newlines, // and /* */ comments — newlines inside block
//     comments are trivia by NL-3) and classifies the next character.
//
// The token covers just the first newline: whitespace before it is
// skipped, the '\n' is consumed, mark_end is called, and everything
// looked at afterwards is speculative lookahead that the parser rewinds
// to mark_end.  Zero-width-looking scans therefore always make progress,
// and comments stay outside the token so they keep their own nodes.

#include "tree_sitter/parser.h"

#include <stdbool.h>
#include <stdint.h>
#include <wctype.h>

enum TokenType { NL };

static inline void advance(TSLexer *lexer) { lexer->advance(lexer, false); }

static inline void skip(TSLexer *lexer) { lexer->advance(lexer, true); }

static bool is_ident_char(int32_t c) {
  return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
         (c >= '0' && c <= '9') || c == '_';
}

// Read ahead for the word "else"; true if the text is exactly that
// keyword (followed by a non-identifier character or EOF).
static bool is_else_keyword(TSLexer *lexer) {
  const char *word = "else";
  for (const char *p = word; *p; p++) {
    if (lexer->lookahead != *p) {
      return false;
    }
    advance(lexer);
  }
  return !is_ident_char(lexer->lookahead);
}

// Skip a "/* ... */" comment body (non-nesting, like Go).  The lexer is
// just past "/*"; it ends just past "*/" or at EOF.
static void consume_block_comment_body(TSLexer *lexer) {
  while (!lexer->eof(lexer)) {
    if (lexer->lookahead == '*') {
      advance(lexer);
      if (lexer->lookahead == '/') {
        advance(lexer);
        return;
      }
    } else {
      advance(lexer);
    }
  }
}

bool tree_sitter_gom_external_scanner_scan(void *payload, TSLexer *lexer,
                                           const bool *valid_symbols) {
  (void)payload;
  if (!valid_symbols[NL]) {
    return false;
  }

  // Only a newline can start an NL token.  Spaces/tabs/CR before it are
  // skipped so they stay outside the token; comments are left alone —
  // they lex as extras on their own, and a newline inside a block
  // comment never counts (NL-3).
  while (lexer->lookahead == ' ' || lexer->lookahead == '\t' ||
         lexer->lookahead == '\r') {
    skip(lexer);
  }
  if (lexer->lookahead != '\n') {
    return false;
  }
  advance(lexer);
  lexer->mark_end(lexer); // the token is just this one '\n'

  // Find the first non-trivia token after the newline.  Whitespace,
  // further newlines and both comment forms are skipped; a lone '/'
  // reached here is the division operator — in line-start, so the
  // newline cannot end a statement and the whole scan rolls back.
  for (;;) {
    int32_t c = lexer->lookahead;
    if (c == ' ' || c == '\t' || c == '\r' || c == '\n') {
      advance(lexer);
      continue;
    }
    if (c == '/') {
      advance(lexer);
      if (lexer->lookahead == '/') {
        while (lexer->lookahead != '\n' && !lexer->eof(lexer)) {
          advance(lexer);
        }
        continue; // the '\n' itself is whitespace, handled by the loop
      }
      if (lexer->lookahead == '*') {
        advance(lexer);
        consume_block_comment_body(lexer);
        continue;
      }
      return false; // '/' division operator: NEXT is in line-start
    }
    break;
  }

  if (lexer->eof(lexer)) {
    lexer->result_symbol = NL; // NL-3: a pending newline at EOF ends the file
    return true;
  }

  bool nl = true;
  switch (lexer->lookahead) {
  // Every token starting with these is in line-start:
  //   |  |>  |?>  ||        >  >>  >=        &&        +  *  %
  case '|':
  case '>':
  case '&':
  case '+':
  case '*':
  case '%':
    nl = false;
    break;
  // '<' '<=' '<<' are in line-start; only '<-' is not (select arms).
  case '<':
    advance(lexer);
    nl = (lexer->lookahead == '-');
    break;
  // '-' is in line-start; '->' is not (match/select arms, arrows).
  case '-':
    advance(lexer);
    nl = (lexer->lookahead == '>');
    break;
  // '==' is in line-start; '=' is not.
  case '=':
    advance(lexer);
    nl = (lexer->lookahead != '=');
    break;
  // '!=' is in line-start; '!' alone is not a token anyway.
  case '!':
    advance(lexer);
    nl = (lexer->lookahead != '=');
    break;
  // "else" is in line-start; other words (in, with, if, idents) are not.
  case 'e':
    nl = !is_else_keyword(lexer);
    break;
  default:
    break;
  }

  if (!nl) {
    return false; // roll back — the newline stays ordinary whitespace
  }
  lexer->result_symbol = NL;
  return true;
}

// The scanner is stateless.
void *tree_sitter_gom_external_scanner_create(void) { return NULL; }

void tree_sitter_gom_external_scanner_destroy(void *payload) {
  (void)payload;
}

unsigned tree_sitter_gom_external_scanner_serialize(void *payload,
                                                    char *buffer) {
  (void)payload;
  (void)buffer;
  return 0;
}

void tree_sitter_gom_external_scanner_deserialize(void *payload,
                                                  const char *buffer,
                                                  unsigned length) {
  (void)payload;
  (void)buffer;
  (void)length;
}