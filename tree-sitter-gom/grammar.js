/**
 * tree-sitter grammar for Gom — a functional language that transpiles to Go.
 *
 * Derived from docs/grammar.ebnf (the normative grammar).  Two deliberate
 * deviations, both documented in docs/decisions/:
 *
 *  1. Newline separators (the NL virtual token of grammar.ebnf §3) are
 *     produced by the external scanner (src/scanner.c).  The grammar only
 *     asks for `_nl` where a separator is legal, which encodes the "PREV in
 *     can-end" half of NL-2; the scanner looks ahead and suppresses NL when
 *     the next token is in the `line-start` set.  Inside `(` `[` `${` the
 *     grammar never asks for `_nl`, so newlines there are whitespace (NL-1)
 *     without the scanner tracking a delimiter stack.
 *
 *  2. Lenient points (editor grammars should keep parsing while you type):
 *     - .gom and .gomi share one start rule (`source_file`); the body-less
 *       .gomi forms are simply top-level items the compiler rejects per-file.
 *     - A newline between a construct's head and its `{` block is tolerated
 *       (`if c\n{ }`), where the reference grammar reports an error.
 *     - `let pat [: T] = e` carries the type annotation the real parser
 *       accepts (docs/problems/2026-11-02-let-type-annotation.md).
 */

/// <reference types="tree-sitter-cli/dsl" />
// @ts-check

// Keywords of grammar.ebnf §2.2 — usable as member names after ".".
const KEYWORDS = [
  'as', 'default', 'else', 'extern', 'false', 'fn', 'for', 'go', 'if',
  'ignore', 'import', 'in', 'let', 'match', 'package', 'select', 'send',
  'test', 'true', 'try', 'type', 'with',
];

module.exports = grammar({
  name: 'gom',

  // The virtual newline separator.  Emitted by src/scanner.c — see the
  // comment at the top of this file.
  externals: $ => [$._nl],

  extras: $ => [/\s/, $.comment],

  word: $ => $.ident,

  conflicts: $ => [
    // "~x: pat" vs the let's own ":" annotation — the label wins, like
    // the app-arg rule.
    [$.pat_arg],
    // "x <- e : T -> body": "->" may extend the type or open the arm
    // body; only one tree survives.
    [$.arrow_type],
  ],

  rules: {
    // ------------------------------------------------------------------
    // 1. Source files and declarations
    // ------------------------------------------------------------------

    source_file: $ => seq(
      $.package_decl,
      repeat(seq($._sep, $._top_level_item)),
      optional($._sep),
    ),

    package_decl: $ => seq('package', $.ident),

    // Union of .gom top-decl and .gomi iface-decl (grammar.ebnf §1.1).
    _top_level_item: $ => seq(
      optional($.attribute),
      choice(
        $.fn_decl,
        $.let_decl,
        $.type_decl,
        $.import_decl,
        $.extern_decl,
        $.test_decl,
        // .gomi-only forms — parsed here, rejected per-file in check.
        // ("let x : T" is already a let_decl with no "=" part.)
        $.fn_sig,
      ),
    ),

    attribute: $ => seq('@', $.member_name),

    _sep: $ => repeat1(choice($._nl, ';')),

    import_decl: $ => seq(
      'import',
      optional('go'),
      optional($.ident),
      $.string,
    ),

    extern_decl: $ => seq(
      'extern', 'go', $.string, '{',
      optional(seq(
        $.extern_item,
        repeat(seq($._sep, $.extern_item)),
        optional($._sep),
      )),
      '}',
    ),

    extern_item: $ => seq(
      'fn',
      field('name', choice($.ident, $.uident)),
      $.fn_param,
      repeat($.fn_param),
      '->',
      $.type_expr,
    ),

    // Union of "type X = body" (.gom) and abstract "type X" (.gomi).
    type_decl: $ => seq(
      'type',
      field('name', $.uident),
      repeat($.ident),
      optional(seq('=', $.type_body)),
    ),

    type_body: $ => choice($.variant_list, $.record_type),

    variant_list: $ => seq(
      optional('|'),
      $.variant,
      repeat(seq('|', $.variant)),
    ),

    variant: $ => seq($.uident, repeat($.variant_field)),

    variant_field: $ => choice(
      seq('(', optional('~'), $.ident, ':', $.type_expr, ')'),
      $.type_atom,
    ),

    record_type: $ => seq(
      '{',
      optional(seq(
        $.record_field,
        repeat(seq(',', $.record_field)),
        optional(','),
      )),
      '}',
    ),

    record_field: $ => seq($.ident, ':', $.type_expr),

    fn_decl: $ => seq($.fn_sig, '=', $.expr),

    fn_sig: $ => seq(
      'fn',
      field('name', $.ident),
      $.fn_param,
      repeat($.fn_param),
      '->',
      $.type_expr,
    ),

    fn_param: $ => choice(
      seq('(', ')'),
      seq('(', optional('~'), $.pattern, ':', $.type_expr, ')'),
    ),

    // "let pat = e", "let pat : T = e" (parser extension), "let x : T" (.gomi).
    let_decl: $ => seq(
      'let',
      $.pattern,
      optional(seq(':', $.type_expr)),
      optional(seq('=', $.expr)),
    ),

    test_decl: $ => seq('test', $.string, $.block),

    // ------------------------------------------------------------------
    // 5. Expressions
    // ------------------------------------------------------------------

    expr: $ => choice($.control_expr, $.ascribed_expr),

    control_expr: $ => choice(
      $.if_expr,
      $.match_expr,
      $.for_expr,
      $.go_expr,
      $.select_expr,
      $.lambda,
    ),

    // prec.right prefers extending the innermost expression with ":" —
    // "x |> fn y -> e : T" ascribes e inside the lambda body (the body
    // extends as far right as possible).
    ascribed_expr: $ => prec.right(
      seq($.pipe_expr, optional(seq(':', $.type_expr))),
    ),

    // prec.right keeps a following "|> g" inside a lambda body that is
    // the pipe rhs — "x |> fn y -> e |> g" is "x |> (fn y -> (e |> g))".
    pipe_expr: $ => prec.right(seq(
      $.bool_or_expr,
      repeat(seq($.pipe_op, $.pipe_rhs)),
    )),

    pipe_op: $ => choice('|\u003e', '\u003c|', '|?\u003e', '\u003e\u003e', '\u003c\u003c'),

    pipe_rhs: $ => choice($.bool_or_expr, $.lambda),

    bool_or_expr: $ => seq(
      $.bool_and_expr,
      repeat(seq('||', $.bool_and_expr)),
    ),

    bool_and_expr: $ => seq(
      $.rel_expr,
      repeat(seq('&&', $.rel_expr)),
    ),

    rel_expr: $ => seq(
      $.add_expr,
      optional(seq($.rel_op, $.add_expr)),
    ),

    rel_op: $ => choice('==', '!=', '\u003c', '\u003c=', '\u003e', '\u003e='),

    add_expr: $ => seq(
      $.mul_expr,
      repeat(seq($.add_op, $.mul_expr)),
    ),

    add_op: $ => choice('+', '-'),

    mul_expr: $ => seq(
      $.unary_expr,
      repeat(seq($.mul_op, $.unary_expr)),
    ),

    mul_op: $ => choice('*', '/', '%'),

    unary_expr: $ => choice(
      seq('-', $.unary_expr),
      seq('try', $.unary_expr),
      seq('ignore', $.unary_expr),
      $.apply_expr,
    ),

    apply_expr: $ => seq(
      $.postfix_expr,
      repeat($.app_arg),
      repeat($.post_arg_selector),
    ),

    post_arg_selector: $ => seq('.', $.member_name, repeat($.app_arg)),

    // The "~x:" label colon binds greedily over an outer ascription
    // (grammar.ebnf app-arg note): "f ~x : T" is "f (~x: T)".  Giving the
    // colon form higher precedence makes the parser prefer shifting ":"
    // into the argument over reducing the punned form first.
    app_arg: $ => choice(
      $.arg_expr,
      prec(1, seq('~', $.ident, ':', $.arg_expr)),
      seq('~', $.ident),
    ),

    // The direct paren_expr branch wins over the atom path so that in
    // "f(x).M(y)" the ".M" belongs to post_arg_selector, not the arg.
    arg_expr: $ => choice(prec(1, $.paren_expr), $.simple_expr),

    simple_expr: $ => $.postfix_expr,

    postfix_expr: $ => prec.left(seq(
      $.atom,
      repeat(seq('.', $.member_name)),
    )),

    member_name: $ => choice($.ident, $.uident, ...KEYWORDS),

    atom: $ => choice(
      $.literal,
      $.ident,
      $.uident,
      '_',
      $.paren_expr,
      $.list_expr,
      $.brace_expr,
    ),

    // "(" ")" unit | "(e)" | "(e,)" | "(e, e, ...)" | "(..e)" | "(e, ..e)"
    paren_expr: $ => seq(
      '(',
      optional(choice(
        seq('..', $.expr),
        seq(
          $.expr,
          repeat(seq(',', $.expr)),
          optional(choice(',', seq(',', '..', $.expr))),
        ),
      )),
      ')',
    ),

    // The real parser (internal/syntax/parse_expr.go parseList) accepts
    // a "..expr" element in any comma position; mid-list spread is a
    // semantic error (E0505), not a parse error.
    list_expr: $ => seq(
      '[',
      optional(seq(
        $.list_elem,
        repeat(seq(',', $.list_elem)),
        optional(','),
      )),
      ']',
    ),

    list_elem: $ => choice(
      $.expr,
      seq('..', $.expr),
    ),

    brace_expr: $ => choice($.block, $.record_literal, $.record_update),

    block: $ => seq(
      '{',
      optional(seq(
        $.block_item,
        repeat(seq($._sep, $.block_item)),
        optional($._sep),
      )),
      '}',
    ),

    block_item: $ => choice($.let_decl, $.expr),

    record_literal: $ => seq(
      '{',
      $.field_init,
      repeat(seq(',', $.field_init)),
      optional(','),
      '}',
    ),

    record_update: $ => seq(
      '{',
      $.expr,
      'with',
      $.field_init,
      repeat(seq(',', $.field_init)),
      optional(','),
      '}',
    ),

    field_init: $ => seq($.ident, '=', $.expr),

    // ---- control expressions ------------------------------------------

    if_expr: $ => seq(
      'if', $.head_expr, $.block,
      optional(seq('else', $.block)),
    ),

    match_expr: $ => seq(
      'match', $.head_expr, '{',
      optional(seq(
        $.match_arm,
        repeat(seq($._sep, $.match_arm)),
        optional($._sep),
      )),
      '}',
    ),

    match_arm: $ => seq(
      $.pattern,
      optional(seq('if', $.expr)),
      '-\u003e',
      $.expr,
    ),

    for_expr: $ => seq('for', $.pattern, 'in', $.head_expr, $.block),

    go_expr: $ => seq('go', $.block),

    select_expr: $ => seq(
      'select', '{',
      optional(seq(
        $.select_arm,
        repeat(seq($._sep, $.select_arm)),
        optional($._sep),
      )),
      '}',
    ),

    select_arm: $ => choice($.recv_arm, $.send_arm, $.default_arm),

    recv_arm: $ => seq(
      choice($.ident, '_'),
      '\u003c-',
      $.expr,
      '-\u003e',
      $.expr,
    ),

    send_arm: $ => seq(
      'send',
      $.simple_expr,
      $.simple_expr,
      '-\u003e',
      $.expr,
    ),

    default_arm: $ => seq('default', '-\u003e', $.expr),

    lambda: $ => seq(
      'fn',
      $.lambda_param,
      repeat($.lambda_param),
      '-\u003e',
      $.expr,
    ),

    lambda_param: $ => choice($.ident, '_', seq('(', ')')),

    // ------------------------------------------------------------------
    // head_expr: expression grammar minus "{" as an atom starter
    // (grammar.ebnf §5).  Each head_* rule mirrors its counterpart with
    // brace_expr removed at the atom level; interior expressions keep the
    // full rules.
    // ------------------------------------------------------------------

    head_expr: $ => choice($.head_control_expr, $.head_ascribed_expr),

    head_control_expr: $ => choice(
      $.if_expr,
      $.match_expr,
      $.for_expr,
      $.go_expr,
      $.select_expr,
      $.head_lambda,
    ),

    head_lambda: $ => seq(
      'fn',
      $.lambda_param,
      repeat($.lambda_param),
      '-\u003e',
      $.head_expr,
    ),

    head_ascribed_expr: $ => prec.right(seq(
      $.head_pipe_expr,
      optional(seq(':', $.type_expr)),
    )),

    head_pipe_expr: $ => prec.right(seq(
      $.head_bool_or_expr,
      repeat(seq($.pipe_op, $.head_pipe_rhs)),
    )),

    head_pipe_rhs: $ => choice($.head_bool_or_expr, $.head_lambda),

    head_bool_or_expr: $ => seq(
      $.head_bool_and_expr,
      repeat(seq('||', $.head_bool_and_expr)),
    ),

    head_bool_and_expr: $ => seq(
      $.head_rel_expr,
      repeat(seq('&&', $.head_rel_expr)),
    ),

    head_rel_expr: $ => seq(
      $.head_add_expr,
      optional(seq($.rel_op, $.head_add_expr)),
    ),

    head_add_expr: $ => seq(
      $.head_mul_expr,
      repeat(seq($.add_op, $.head_mul_expr)),
    ),

    head_mul_expr: $ => seq(
      $.head_unary_expr,
      repeat(seq($.mul_op, $.head_unary_expr)),
    ),

    head_unary_expr: $ => choice(
      seq('-', $.head_unary_expr),
      seq('try', $.head_unary_expr),
      seq('ignore', $.head_unary_expr),
      $.head_apply_expr,
    ),

    head_apply_expr: $ => seq(
      $.head_postfix_expr,
      repeat($.head_app_arg),
      repeat($.head_post_arg_selector),
    ),

    head_post_arg_selector: $ => seq(
      '.',
      $.member_name,
      repeat($.head_app_arg),
    ),

    head_app_arg: $ => choice(
      $.head_arg_expr,
      prec(1, seq('~', $.ident, ':', $.head_arg_expr)),
      seq('~', $.ident),
    ),

    head_arg_expr: $ => choice(prec(1, $.paren_expr), $.head_simple_expr),

    head_simple_expr: $ => $.head_postfix_expr,

    head_postfix_expr: $ => prec.left(seq(
      $.head_atom,
      repeat(seq('.', $.member_name)),
    )),

    // Same as atom minus brace_expr.
    head_atom: $ => choice(
      $.literal,
      $.ident,
      $.uident,
      '_',
      $.paren_expr,
      $.list_expr,
    ),

    // ------------------------------------------------------------------
    // 6. Patterns
    // ------------------------------------------------------------------

    pattern: $ => seq($.or_pattern, optional(seq('as', $.ident))),

    or_pattern: $ => seq(
      $.ctor_pattern,
      repeat(seq('|', $.ctor_pattern)),
    ),

    ctor_pattern: $ => choice(
      seq($.ctor_name, repeat($.pat_arg)),
      $.pat_atom,
    ),

    ctor_name: $ => seq(
      optional(seq(
        choice($.ident, $.uident),
        repeat(seq('.', $.uident)),
        '.',
      )),
      $.uident,
    ),

    pat_arg: $ => choice(
      $.pat_atom,
      seq('~', $.ident, optional(prec(1, seq(':', $.pat_atom)))),
    ),

    pat_atom: $ => choice(
      '_',
      $.ident,
      $.pat_literal,
      seq('(', ')'),
      seq('(', $.pattern, ')'),
      $.tuple_pat,
      $.list_pat,
      $.brace_pat,
    ),

    pat_literal: $ => choice(
      $.int_literal,
      $.float_literal,
      $.char_literal,
      $.string,
      'true',
      'false',
      seq('-', choice($.int_literal, $.float_literal)),
    ),

    tuple_pat: $ => seq(
      '(',
      $.pattern,
      ',',
      $.pattern,
      repeat(seq(',', $.pattern)),
      optional(','),
      ')',
    ),

    list_pat: $ => choice(
      seq('[', ']'),
      seq('[', '..', choice($.ident, '_'), ']'),
      seq(
        '[',
        $.pattern,
        repeat(seq(',', $.pattern)),
        optional(','),
        ']',
      ),
      seq(
        '[',
        $.pattern,
        repeat(seq(',', $.pattern)),
        ',',
        '..',
        choice($.ident, '_'),
        ']',
      ),
    ),

    brace_pat: $ => seq(
      '{',
      optional(seq(
        $.pat_field,
        repeat(seq(',', $.pat_field)),
        optional(','),
      )),
      '}',
    ),

    pat_field: $ => choice(
      '..',
      seq($.ident, optional(seq('=', $.pattern))),
    ),

    // ------------------------------------------------------------------
    // 7. Types
    // ------------------------------------------------------------------

    type_expr: $ => $.arrow_type,

    arrow_type: $ => choice(
      seq('~', $.ident, ':', $.app_type, '-\u003e', $.arrow_type),
      seq($.app_type, optional(seq('-\u003e', $.arrow_type))),
    ),

    app_type: $ => prec.left(seq($.type_atom, repeat($.type_atom))),

    type_atom: $ => choice(
      seq($.uident, optional(seq('.', $.uident))),
      seq($.ident, '.', $.uident),
      $.ident,
      seq('(', ')'),
      seq('(', $.type_expr, ')'),
      $.tuple_type,
      $.record_type,
    ),

    tuple_type: $ => seq(
      '(',
      $.type_expr,
      ',',
      $.type_expr,
      repeat(seq(',', $.type_expr)),
      optional(','),
      ')',
    ),

    // ------------------------------------------------------------------
    // Lexical: literals, identifiers, comments
    // ------------------------------------------------------------------

    literal: $ => choice(
      $.int_literal,
      $.float_literal,
      $.char_literal,
      $.string,
      'true',
      'false',
    ),

    int_literal: _ => token(choice(
      /0[xX][0-9a-fA-F_]+/,
      /0[oO][0-7_]+/,
      /0[bB][01_]+/,
      /[0-9][0-9_]*/,
    )),

    float_literal: _ => token(choice(
      /[0-9][0-9_]*\.[0-9][0-9_]*([eE][+-]?[0-9]+)?/,
      /[0-9][0-9_]*[eE][+-]?[0-9]+/,
    )),

    char_literal: _ => token(seq(
      "'",
      repeat(choice(/[^'\\]/, /\\(.|u\{[0-9a-fA-F]+\})/)),
      "'",
    )),

    string: $ => seq(
      '"',
      repeat(choice(
        $.interpolation,
        $.escape_sequence,
        $.string_content,
        '$', // a literal '$' anywhere else it appears
      )),
      '"',
    ),

    // Runs that contain no '"' '\' or '$'.  '${' always becomes an
    // interpolation (the longer match); a '$' not starting one is the
    // anonymous token above.
    string_content: _ => token(prec(1, /[^"\\$]+/)),

    escape_sequence: _ => token(/\\(u\{[0-9a-fA-F]+\}|.)/),

    interpolation: $ => seq('${', $.expr, '}'),

    // "_" alone is the wildcard token, so ident never matches a bare "_".
    ident: _ => /([a-z][a-zA-Z0-9_]*|_[a-zA-Z0-9_]+)/,

    uident: _ => /[A-Z][a-zA-Z0-9_]*/,

    comment: _ => token(choice(
      /\/\/[^\n]*/,
      /\/\*([^*]|\*+[^*/])*\*+\//,
    )),
  },
});
