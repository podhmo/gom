;;; gom-mode.el --- Major mode for Gom  -*- lexical-binding: t; -*-

;; Author: podhmo
;; Keywords: gom languages
;; Version: 0.2.0
;; Package-Requires: ((emacs "27.1"))

;; This file is NOT part of GNU Emacs.

;;; Commentary:

;; `gom-mode' is a major mode for Gom source files (.gom) and interface
;; files (.gomi).  A .gomi uses the same tokens as a .gom and only holds
;; body-less declarations, so one mode serves both.
;;
;; When Emacs is built with tree-sitter and the Gom grammar
;; (tree-sitter-gom/ in the Gom repository) is installed, font-lock,
;; imenu and indentation run on `treesit' (Emacs 29+).  Otherwise the
;; mode falls back to the original regexp-based implementation, so the
;; grammar stays optional.
;;
;; To install the grammar with `treesit-install-language-grammar' (it
;; clones the Gom repository and builds the grammar in
;; tree-sitter-gom/):
;;
;;   (add-to-list 'treesit-language-source-alist
;;                '(gom "https://github.com/podhmo/gom" nil "tree-sitter-gom"))
;;   M-x treesit-install-language-grammar RET gom RET
;;
;; Features:
;;   - font-lock (tree-sitter or regexp)
;;   - indentation (2 spaces, tree-sitter rules or bracket depth +
;;     continuation lines; both follow `gom fmt' output)
;;   - imenu for fn / type / let / test / extern items
;;   - `gom-format-buffer' (gom fmt -)
;;   - Flymake backend `gom-flymake' (gom check DIR)
;;
;; Usage:
;;   (add-to-list 'load-path "/path/to/gom/editor/emacs")
;;   (require 'gom-mode)
;;   (add-hook 'gom-mode-hook #'flymake-mode)     ; optional

;;; Code:

(require 'json)
(require 'flymake)
(require 'treesit nil t)

(defgroup gom nil
  "Major mode for the Gom language."
  :group 'languages
  :prefix "gom-")

(defcustom gom-indent-offset 2
  "Indentation width for `gom-mode'.  `gom fmt' uses 2."
  :type 'integer
  :safe #'integerp
  :group 'gom)

(defcustom gom-command "gom"
  "Name or path of the gom executable."
  :type 'string
  :group 'gom)

(defcustom gom-use-tree-sitter t
  "Use the tree-sitter Gom grammar when it is available.
When non-nil and `treesit-ready-p' reports the `gom' grammar
installed, `gom-mode' highlights, indents and indexes via
tree-sitter.  When nil (or the grammar is missing) the mode falls
back to regexp font-lock, `gom-indent-line' and
`imenu-generic-expression'."
  :type 'boolean
  :group 'gom)

;;; Faces

(defface gom-doc-comment-face
  '((t :inherit font-lock-doc-face))
  "Face for /// doc comments."
  :group 'gom)

(defface gom-label-face
  '((t :inherit font-lock-constant-face))
  "Face for labeled arguments such as ~name."
  :group 'gom)

(defface gom-number-face
  '((t :inherit font-lock-constant-face))
  "Face for numeric literals."
  :group 'gom)

(defface gom-interpolation-face
  '((t :inherit font-lock-variable-name-face))
  "Face for ${...} inside string literals."
  :group 'gom)

;;; Syntax table

(defvar gom-mode-syntax-table
  (let ((table (make-syntax-table)))
    (modify-syntax-entry ?_  "_"      table)
    (modify-syntax-entry ?/  ". 124b" table)  ; // and /* */
    (modify-syntax-entry ?*  ". 23"   table)
    (modify-syntax-entry ?\n "> b"    table)
    (modify-syntax-entry ?\\ "\\"     table)
    (modify-syntax-entry ?\" "\""     table)
    (modify-syntax-entry ?\' "\""     table)  ; char literal; Gom has no primed idents
    (dolist (c '(?+ ?- ?= ?< ?> ?| ?& ?% ?~ ?: ?. ?@ ?? ?!))
      (modify-syntax-entry c "." table))
    table)
  "Syntax table for `gom-mode'.")

;;; Font-lock

(defconst gom-keywords
  '("as" "default" "else" "extern" "false" "fn" "for" "go" "if" "ignore"
    "import" "in" "let" "match" "package" "select" "send" "test" "true"
    "try" "type" "with")
  "Reserved words (docs/grammar.ebnf 2.2).")

(defconst gom-builtin-functions
  '("assert" "assert_eq" "todo" "not" "show" "compare")
  "Ordinary functions provided by the language (spec 1.1, 11.4).")

(defconst gom--not-after-dot "\\(?:^\\|[^.[:alnum:]_]\\)"
  "Regexp that matches the position before a name not preceded by `.'.
After `.' any keyword is a plain member name (`Chan.send').")

(defun gom--interpolation-matcher (limit)
  "Find the next ${...} inside a string before LIMIT."
  (let (found)
    (while (and (not found)
                (re-search-forward "\\${[^}\n]*}" limit t))
      (when (nth 3 (save-excursion (syntax-ppss (match-beginning 0))))
        (setq found t)))
    found))

(defun gom--doc-comment-matcher (limit)
  "Find the next /// comment before LIMIT."
  (let (found)
    (while (and (not found)
                (re-search-forward "///.*$" limit t))
      (when (nth 4 (save-excursion (syntax-ppss (+ 2 (match-beginning 0)))))
        (setq found t)))
    found))

(defconst gom-font-lock-keywords
  `(;; doc comments
    (gom--doc-comment-matcher 0 'gom-doc-comment-face t)
    ;; ${...} inside strings
    (gom--interpolation-matcher 0 'gom-interpolation-face t)
    ;; attributes: @test @tailrec
    ("@[[:alpha:]_][[:alnum:]_]*" . font-lock-preprocessor-face)
    ;; keywords (not after `.')
    (,(concat gom--not-after-dot "\\(" (regexp-opt gom-keywords 'words) "\\)")
     1 font-lock-keyword-face)
    ;; fn NAME (  — declarations and extern items; lambdas have no name
    ("\\_<fn[ \t]+\\([[:alpha:]_][[:alnum:]_]*\\)[ \t]*("
     1 font-lock-function-name-face)
    ;; type NAME
    ("\\_<type[ \t]+\\([[:upper:]][[:alnum:]_]*\\)" 1 font-lock-type-face)
    ;; let NAME
    ("\\_<let[ \t]+\\([[:lower:]_][[:alnum:]_]*\\)" 1 font-lock-variable-name-face)
    ;; package NAME / import [go] ALIAS "path"
    ("\\_<package[ \t]+\\([[:alpha:]_][[:alnum:]_]*\\)" 1 font-lock-constant-face)
    ("\\_<import[ \t]+\\(?:go[ \t]+\\)?\\([[:alpha:]_][[:alnum:]_]*\\)[ \t]+\""
     1 font-lock-constant-face)
    ;; labels: ~name
    ("~[[:lower:]_][[:alnum:]_]*" . 'gom-label-face)
    ;; builtin functions (not after `.')
    (,(concat gom--not-after-dot "\\(" (regexp-opt gom-builtin-functions 'symbols) "\\)")
     1 font-lock-builtin-face)
    ;; uppercase names: types, constructors, modules (not Go members after `.')
    (,(concat gom--not-after-dot "\\(\\_<[[:upper:]][[:alnum:]_]*\\_>\\)")
     1 font-lock-type-face)
    ;; numbers
    ("\\_<\\(?:0[xX][[:xdigit:]_]+\\|0[oO][0-7_]+\\|0[bB][01_]+\\|[0-9][0-9_]*\\(?:\\.[0-9][0-9_]*\\)?\\(?:[eE][-+]?[0-9]+\\)?\\)\\_>"
     . 'gom-number-face))
  "Font-lock keywords for `gom-mode'.")

;;; Indentation

(defconst gom--continuation-end-re
  (concat "\\(?:" (regexp-opt '("=" "->" "<-" "|>" "<|" "|?>" ">>" "<<"
                                "||" "&&" "==" "!=" "<" "<=" ">" ">="
                                "+" "-" "*" "/" "%" "|" ":"))
          "\\)[ \t]*$")
  "A line ending with one of these continues on the next line.")

(defconst gom--continuation-start-re
  (concat "[ \t]*\\(?:" (regexp-opt '("|>" "<|" "|?>" ">>" "<<" "||" "&&"
                                      "==" "!=" "<=" ">=" "+" "*" "%"
                                      "|"))
          "\\|-[^>]\\|<[^-|<=]\\|>[^>=]\\|/[^/*]\\)")
  "A line starting with one of these continues the previous line.")

(defun gom--line-code-end ()
  "Return the position after the last code character on the current line."
  (save-excursion
    (end-of-line)
    (let ((bol (line-beginning-position))
          (ppss (syntax-ppss)))
      (when (nth 4 ppss) (goto-char (nth 8 ppss)))
      (skip-chars-backward " \t" bol)
      (while (and (> (point) (1+ bol))
                  (eq (char-before) ?/)
                  (eq (char-before (1- (point))) ?*)
                  (nth 4 (syntax-ppss (1- (point)))))
        (forward-comment -1)
        (skip-chars-backward " \t" bol))
      (max (point) bol))))

(defun gom--previous-code-line ()
  "Move to the previous line that has code; return non-nil on success."
  (let (found)
    (while (and (not found) (zerop (forward-line -1)))
      (back-to-indentation)
      (unless (or (eolp)
                  (nth 4 (syntax-ppss))
                  (looking-at-p "//\\|/\\*"))
        (setq found t)))
    found))

(defun gom--next-code-line ()
  "Move to the next line that has code; return non-nil on success."
  (let (found)
    (while (and (not found) (zerop (forward-line 1)) (not (eobp)))
      (back-to-indentation)
      (unless (or (eolp)
                  (nth 4 (syntax-ppss))
                  (looking-at-p "//\\|/\\*"))
        (setq found t)))
    found))

(defun gom--line-ends-continued-p ()
  "Non-nil if the current line's code ends with a continuation operator."
  (let ((end (gom--line-code-end)))
    (save-excursion
      (goto-char end)
      (and (> end (line-beginning-position))
           (looking-back gom--continuation-end-re (line-beginning-position))
           ;; `|' alone ends a variant only in "| Ctor |" chains; treat as continued
           t))))

(defun gom--previous-continued-line (opener)
  "Return the start of the previous code line if the current line continues it.
The previous line must lie after OPENER at the same bracket depth."
  (save-excursion
    (back-to-indentation)
    (let ((depth (nth 0 (syntax-ppss)))
          (starts-continued (looking-at-p gom--continuation-start-re)))
      (when (and (gom--previous-code-line)
                 (or (null opener) (> (point) opener))
                 (= (nth 0 (syntax-ppss)) depth)
                 (or starts-continued (gom--line-ends-continued-p)))
        (point)))))

(defun gom--statement-start (opener)
  "Return the indentation column of the statement the current line belongs to.
OPENER bounds the search: lines before it belong to an outer level."
  (save-excursion
    (let (prev)
      (while (setq prev (gom--previous-continued-line opener))
        (goto-char prev)))
    (current-indentation)))

(defun gom--calculate-indentation ()
  "Return the column the current line should be indented to, or nil."
  (save-excursion
    (back-to-indentation)
    (let* ((ppss (syntax-ppss))
           (opener (nth 1 ppss))
           (closing (looking-at-p "[])}]"))
           (base (if opener
                     (save-excursion
                       (goto-char opener)
                       (+ (current-indentation)
                          (if closing 0 gom-indent-offset)))
                   0)))
      (cond
       ((or (nth 3 ppss) (nth 4 ppss)) nil)     ; inside string / block comment
       (closing base)
       ((looking-at-p "//\\|/\\*")
        ;; A comment line takes the indentation of the code it precedes
        ;; (`/// docs' before `| Ctor'), unless that code closes the block.
        (let ((next (save-excursion
                      (and (gom--next-code-line)
                           (eq (nth 1 (syntax-ppss)) opener)
                           (not (looking-at-p "[])}]"))
                           (gom--calculate-indentation)))))
          (or next base)))
       ((gom--previous-continued-line opener)
        (save-excursion
          (goto-char (gom--previous-continued-line opener))
          (+ (gom--statement-start opener) gom-indent-offset)))
       (t base)))))

(defun gom-indent-line ()
  "Indent the current line as Gom code."
  (interactive)
  (let ((col (gom--calculate-indentation)))
    (when col
      (if (<= (current-column) (current-indentation))
          (indent-line-to col)
        (save-excursion (indent-line-to col))))))

;;; Imenu

(defvar gom-imenu-generic-expression
  '(("Test" "^test[ \t]+\\(\"[^\"\n]*\"\\)" 1)
    ("Let" "^let[ \t]+\\([[:lower:]_][[:alnum:]_]*\\)" 1)
    ("Type" "^type[ \t]+\\([[:upper:]][[:alnum:]_]*\\)" 1)
    ("Extern" "^[ \t]+fn[ \t]+\\([[:alpha:]_][[:alnum:]_]*\\)[ \t]*(" 1)
    (nil "^fn[ \t]+\\([[:alpha:]_][[:alnum:]_]*\\)" 1))
  "Imenu entries for top-level Gom declarations.")

;;; Tree-sitter support

;; These definitions only run when the Gom grammar from
;; tree-sitter-gom/ is installed; `gom-use-tree-sitter' decides whether
;; the mode even asks.  Everything falls back to the regexp engine
;; otherwise (see `gom-mode' below).

(defun gom--treesit-available-p ()
  "Return non-nil when the mode should drive this buffer with tree-sitter."
  (and gom-use-tree-sitter
       (fboundp 'treesit-ready-p)
       (treesit-ready-p 'gom t)))

;; Queries and predicates for `treesit-font-lock-rules'.

(defun gom--treesit-member-child-p (node &rest _)
  "Return non-nil if NODE is a child of member_name.
After \".\" every name (including keywords such as `send' in
`Chan.send') is a plain member, so queries that fontify keywords,
builtin functions or UIdents use this predicate to skip them."
  (let ((parent (treesit-node-parent node)))
    (and parent (string= (treesit-node-type parent) "member_name"))))

(defun gom--treesit-not-member-child-p (node &rest _)
  "Return non-nil if NODE is not a member name after \".\"."
  (not (gom--treesit-member-child-p node)))

(defun gom--treesit-font-lock-settings ()
  "Return `treesit-font-lock-settings' for Gom."
  (treesit-font-lock-rules

   ;; -- level 1: comments, definitions ------------------------------

   :language 'gom
   :feature 'comment
   '(((comment) @gom-doc-comment-face
      (:match "\\`///" @gom-doc-comment-face)))
   :language 'gom
   :feature 'comment
   '((comment) @font-lock-comment-face)

   :language 'gom
   :feature 'definition
   '(;; fn NAME ( ... ) and extern items "fn NewBufferString ..."
     (fn_sig name: (ident) @font-lock-function-name-face)
     (extern_item name: (_) @font-lock-function-name-face)
     ;; type NAME
     (type_decl name: (uident) @font-lock-type-face)
     ;; package NAME / import [go] ALIAS "path"
     (package_decl (ident) @font-lock-constant-face)
     (import_decl (ident) @font-lock-constant-face)
     ;; let NAME = ... (a plain identifier pattern, like the regexp mode)
     (let_decl
      (pattern
       (or_pattern
        (ctor_pattern
         (pat_atom (ident) @font-lock-variable-name-face))))))

   ;; -- level 2: keywords, strings ----------------------------------

   :language 'gom
   :feature 'keyword
   `(([,@gom-keywords] @font-lock-keyword-face
      (:pred gom--treesit-not-member-child-p @font-lock-keyword-face)))

   :language 'gom
   :feature 'string
   '((string) @font-lock-string-face
     (char_literal) @font-lock-string-face)

   ;; ${...} replaces the string face inside the interpolation only.
   :language 'gom
   :feature 'string
   :override t
   '((interpolation) @gom-interpolation-face)

   ;; -- level 3: types, constants, numbers, labels, attributes ------

   ;; UIdent is a type, a constructor or a module — except after ".",
   ;; where it names a Go member (fmt.Println).
   :language 'gom
   :feature 'type
   '(((uident) @font-lock-type-face
      (:pred gom--treesit-not-member-child-p @font-lock-type-face)))

   :language 'gom
   :feature 'number
   '((int_literal) @gom-number-face
     (float_literal) @gom-number-face)

   ;; ~name labels: call arguments, constructor patterns, function
   ;; parameters and labelled arrow types.  Capture "~" and the name
   ;; separately so "~name" reads as one highlighted token.
   :language 'gom
   :feature 'label
   '((app_arg "~" @gom-label-face (ident) @gom-label-face)
     (pat_arg "~" @gom-label-face (ident) @gom-label-face)
     (arrow_type "~" @gom-label-face (ident) @gom-label-face)
     (fn_param
      "~" @gom-label-face
      (pattern
       (or_pattern
        (ctor_pattern
         (pat_atom (ident) @gom-label-face))))))

   :language 'gom
   :feature 'builtin
   `(((ident) @font-lock-builtin-face
      (:match
       ,(concat "\\`" (regexp-opt gom-builtin-functions) "\\'")
       @font-lock-builtin-face)
      (:pred gom--treesit-not-member-child-p @font-lock-builtin-face)))

   :language 'gom
   :feature 'attribute
   '((attribute) @font-lock-preprocessor-face)

   ;; -- level 4 (off by default): calls, operators, brackets ---------

   :language 'gom
   :feature 'function
   '((apply_expr
      (postfix_expr (atom (ident) @font-lock-function-call-face))
      (app_arg)))

   :language 'gom
   :feature 'operator
   '((pipe_op) @font-lock-operator-face
     (add_op) @font-lock-operator-face
     (mul_op) @font-lock-operator-face
     (rel_op) @font-lock-operator-face
     ["->" "<-" "=" ".." "||" "&&" "|" "~" ":" "@" "."]
     @font-lock-operator-face)

   :language 'gom
   :feature 'bracket
   '((["(" ")" "[" "]" "{" "}"]) @font-lock-bracket-face)

   :language 'gom
   :feature 'delimiter
   '((["," ";"]) @font-lock-delimiter-face)))

;; Indentation rules for `treesit-simple-indent-rules'.  The shapes
;; mirror `gom-indent-line': 2 columns per bracket level, "| Ctor"
;; variants one level under `type', operator-leading lines one level
;; under their statement, `else' aligned with its `if' statement.

(defun gom--treesit-continuation-node-p (node _parent _bol)
  "Non-nil if NODE is an operator token that may lead a continued line.
These are the line-start tokens of grammar.ebnf section 3 whose node
has a visible type (`||' and `&&' are anonymous children; `else' is
handled separately because it aligns with `if', not the statement)."
  (and node
       (member (treesit-node-type node)
               '("add_op" "mul_op" "rel_op" "pipe_op" "||" "&&"))))

(defun gom--treesit-continuation-offset (_node parent _bol)
  "Return the column offset for an operator-leading continuation line.
When the continued expression starts at the beginning of its own line
(`aaaaaaaa\\=' in `gom fmt' output) the operator aligns with it;
otherwise the operator indents one level under the line holding the
expression (a `let x = aaa\\=' continuation)."
  (if (save-excursion
        (goto-char (treesit-node-start parent))
        (looking-back "^[ \t]*" (line-beginning-position)))
      0
    gom-indent-offset))

(defvar gom--treesit-indent-rules
  `((gom
     ;; A closing bracket aligns with the line holding its opener.
     ((node-is "[])}]") parent-bol 0)

     ;; Variant lists: the first "|" line ends up with NODE = type_body
     ;; (the topmost node that starts at that "|"), later "|" lines and
     ;; comments between variants have variant_list as parent.
     ((n-p-gp "type_body" "type_decl" nil) parent gom-indent-offset)
     ((parent-is "variant_list") parent 0)

     ;; `else' aligns with the statement its `if' belongs to.
     ((node-is "else") standalone-parent 0)

     ;; A line led by a binary/pipe operator continues its statement.
     (gom--treesit-continuation-node-p
      parent-bol gom--treesit-continuation-offset)

     ;; A "|" continuing an or-pattern aligns with the pattern start.
     ((n-p-gp "|" "or_pattern" nil) parent-bol 0)

     ;; A pipe rhs written after a trailing "|>" indents one level.
     ((parent-is "pipe_expr") parent-bol gom-indent-offset)

     ;; Contents of constructs enclosed in "{" "(" "[".
     ((parent-is "block") parent-bol gom-indent-offset)
     ((parent-is "match_expr") parent-bol gom-indent-offset)
     ((parent-is "select_expr") parent-bol gom-indent-offset)
     ((parent-is "extern_decl") parent-bol gom-indent-offset)
     ((parent-is "record_literal") parent-bol gom-indent-offset)
     ((parent-is "record_update") parent-bol gom-indent-offset)
     ((parent-is "record_type") parent-bol gom-indent-offset)
     ((parent-is "brace_pat") parent-bol gom-indent-offset)
     ((parent-is "list_expr") parent-bol gom-indent-offset)
     ((parent-is "list_pat") parent-bol gom-indent-offset)
     ((parent-is "paren_expr") parent-bol gom-indent-offset)
     ((parent-is "tuple_pat") parent-bol gom-indent-offset)
     ((parent-is "tuple_type") parent-bol gom-indent-offset)
     ((parent-is "fn_param") parent-bol gom-indent-offset)
     ((parent-is "interpolation") parent-bol gom-indent-offset)

     ;; An expression moved to the line after "=" or "->".
     ((parent-is "let_decl") parent-bol gom-indent-offset)
     ((parent-is "fn_decl") parent-bol gom-indent-offset)
     ((parent-is "match_arm") parent-bol gom-indent-offset)
     ((parent-is "recv_arm") parent-bol gom-indent-offset)
     ((parent-is "send_arm") parent-bol gom-indent-offset)
     ((parent-is "test_decl") parent-bol gom-indent-offset)
     ((parent-is "if_expr") parent-bol gom-indent-offset)
     ((parent-is "for_expr") parent-bol gom-indent-offset)
     ((parent-is "fn_sig") parent-bol gom-indent-offset)

     ;; `@attr' sits on the declaration it annotates.
     ((node-is "attribute") parent-bol 0)

     ;; Everything at top level, then a last resort.
     ((parent-is "source_file") parent-bol 0)
     (catch-all parent-bol 0)))
  "Indent rules for `treesit-simple-indent-rules' in `gom-mode'.")

;; Imenu / defun names over the parse tree.

(defun gom--treesit-child-of-type (node type)
  "Return the first named child of NODE whose type is TYPE, or nil."
  (catch 'found
    (dolist (child (treesit-node-children node t))
      (when (string= (treesit-node-type child) type)
        (throw 'found child)))))

(defun gom--treesit-first-ident (node)
  "Return NODE when it is an ident, else descend into its first child."
  (when node
    (if (string= (treesit-node-type node) "ident")
        node
      (gom--treesit-first-ident (treesit-node-child node 0 t)))))

(defun gom--treesit-top-level-p (node)
  "Return non-nil when NODE is a direct child of the grammar root.
Used to tell a bare `fn_sig' (a top-level declaration in .gomi
files) from the `fn_sig' nested inside a .gom `fn_decl'."
  (let ((parent (treesit-node-parent node)))
    (and parent (string= (treesit-node-type parent) "source_file"))))

(defun gom--treesit-defun-p (node)
  "Return non-nil when NODE is a defun for `gom-mode'.
A `fn_sig' counts only at top level (a bare signature is a .gomi
declaration); inside a `fn_decl' the decl itself is the defun."
  (or (not (string= (treesit-node-type node) "fn_sig"))
      (gom--treesit-top-level-p node)))

(defun gom--treesit-defun-name (node)
  "Return the display name of a defun NODE for imenu and which-func."
  (pcase (treesit-node-type node)
    ;; A bare fn_sig is a top-level declaration in .gomi files; its
    ;; `name' field sits on the sig itself.
    ("fn_sig"
     (when-let* ((id (treesit-node-child-by-field-name node "name")))
       (treesit-node-text id)))
    ("fn_decl"
     (when-let* ((sig (gom--treesit-child-of-type node "fn_sig"))
                 (id (treesit-node-child-by-field-name sig "name")))
       (treesit-node-text id)))
    ("type_decl"
     (when-let* ((id (treesit-node-child-by-field-name node "name")))
       (treesit-node-text id)))
    ("extern_item"
     (when-let* ((id (treesit-node-child-by-field-name node "name")))
       (treesit-node-text id)))
    ("let_decl"
     (when-let* ((pat (gom--treesit-child-of-type node "pattern"))
                 (id (gom--treesit-first-ident pat)))
       (treesit-node-text id)))
    ("test_decl"
     (when-let* ((s (gom--treesit-child-of-type node "string")))
       (treesit-node-text s)))
    (_ nil)))

(defvar gom--treesit-imenu-settings
  '(("Test" "\\`test_decl\\'" nil nil)
    ("Let" "\\`let_decl\\'" nil nil)
    ("Type" "\\`type_decl\\'" nil nil)
    ("Extern" "\\`extern_item\\'" nil nil)
    (nil "\\`fn_decl\\'" nil nil)
    ;; Top-level signatures are the `fn' declarations of .gomi
    ;; files; the predicate excludes the fn_sig nested in fn_decl.
    (nil "\\`fn_sig\\'" gom--treesit-top-level-p nil))
  "Tree-sitter imenu settings for `gom-mode'.
Same categories as `gom-imenu-generic-expression'.")

(defun gom--treesit-setup ()
  "Configure tree-sitter font-lock, imenu and indentation for Gom.
Called from `gom-mode' when `gom--treesit-available-p' holds."
  (treesit-parser-create 'gom)
  (setq-local treesit-font-lock-settings
              (gom--treesit-font-lock-settings))
  (setq-local treesit-font-lock-feature-list
              '((comment definition)
                (keyword string)
                (type number label builtin attribute)
                (function operator bracket delimiter)))
  (setq-local treesit-simple-indent-rules gom--treesit-indent-rules)
  (setq-local treesit-defun-name-function #'gom--treesit-defun-name)
  ;; A bare fn_sig is a .gomi-level declaration and a defun; the
  ;; fn_sig nested inside a .gom fn_decl is filtered out by the
  ;; (REGEXP . PRED) cons form on Emacs 30+.  On Emacs 29, which
  ;; only accepts a plain regexp, a nested sig still counts as a
  ;; defun — harmless, since it starts at the same column as its
  ;; fn_decl.
  (let ((re "\\`\\(?:fn_decl\\|fn_sig\\|type_decl\\|let_decl\\|test_decl\\)\\'"))
    (setq-local treesit-defun-type-regexp
                (if (>= emacs-major-version 30)
                    (cons re #'gom--treesit-defun-p)
                  re)))
  (setq-local treesit-simple-imenu-settings gom--treesit-imenu-settings)
  (treesit-major-mode-setup))

;;; Formatting

;;;###autoload
(defun gom-format-buffer ()
  "Format the current buffer with `gom fmt -'.
On failure the buffer is left unchanged and the diagnostics are shown."
  (interactive)
  (unless (executable-find gom-command)
    (user-error "%s executable not found in PATH" gom-command))
  (let ((out (generate-new-buffer " *gom-fmt-output*"))
        (err (make-temp-file "gom-fmt-error-")))
    (unwind-protect
        (let ((code (call-process-region (point-min) (point-max) gom-command
                                         nil (list out err) nil "fmt" "-")))
          (if (zerop code)
              (progn
                (unless (zerop (compare-buffer-substrings
                                nil nil nil out nil nil))
                  (with-no-warnings (replace-buffer-contents out)))
                (message "gom fmt: formatted"))
            (let ((msgs (gom--fmt-error-messages err)))
              (message "gom fmt failed: %s"
                       (if msgs (mapconcat #'identity msgs "; ")
                         (format "exit %d" code))))))
      (kill-buffer out)
      (delete-file err))))

(defun gom--fmt-error-messages (file)
  "Return \"LINE:COL: message\" strings for diagnostics in FILE."
  (with-temp-buffer
    (insert-file-contents file)
    (let (msgs)
      (dolist (obj (gom--read-json-lines))
        (when (equal (alist-get 'kind obj) "diagnostic")
          (let-alist obj
            (push (format "%s:%s: %s" .span.start.line .span.start.col .message)
                  msgs))))
      (nreverse msgs))))

;;; Flymake

(defun gom--read-json-lines ()
  "Parse every JSON object line in the current buffer into alists."
  (let (objs)
    (save-excursion
      (goto-char (point-min))
      (while (not (eobp))
        (when (looking-at-p "{")
          (condition-case nil
              (push (let ((json-object-type 'alist)
                          (json-array-type 'list))
                      (json-read-from-string
                       (buffer-substring-no-properties
                        (point) (line-end-position))))
                    objs)
            (error nil)))
        (forward-line 1)))
    (nreverse objs)))

(defun gom--position (source line col)
  "Return the position of LINE / COL (both 1-based, COL in chars) in SOURCE."
  (with-current-buffer source
    (save-excursion
      (save-restriction
        (widen)
        (goto-char (point-min))
        (forward-line (1- line))
        (min (+ (point) (max 0 (1- col))) (line-end-position))))))

(defun gom-flymake--diagnostics (source)
  "Turn `gom check' output in the current buffer into Flymake diagnostics.
Only diagnostics for SOURCE's file are kept."
  (let ((file (expand-file-name (buffer-file-name source)))
        diags)
    (dolist (obj (gom--read-json-lines))
      (let-alist obj
        (when (and (equal .kind "diagnostic")
                   (stringp .span.file)
                   (equal (expand-file-name .span.file) file))
          (let* ((beg (gom--position source .span.start.line .span.start.col))
                 (end (gom--position source .span.end.line .span.end.col))
                 (end (if (<= end beg)
                          (with-current-buffer source
                            (save-excursion
                              (goto-char beg)
                              (if (eolp) (min (point-max) (1+ beg))
                                (gom--symbol-end-or-next beg))))
                        end)))
            (push (flymake-make-diagnostic
                   source beg end
                   (pcase .severity
                     ("error" :error)
                     ("warning" :warning)
                     (_ :note))
                   (if .code (format "%s %s" .code .message) .message))
                  diags)))))
    (nreverse diags)))

(defun gom--symbol-end-or-next (pos)
  "Return the end of the symbol at POS, or POS + 1."
  (save-excursion
    (goto-char pos)
    (if (> (skip-syntax-forward "w_") 0) (point) (1+ pos))))

(defvar-local gom-flymake--proc nil)

(defun gom-flymake (report-fn &rest _args)
  "Flymake backend running `gom check' on the buffer's directory.
`gom check' does not read stdin, so the saved file is checked.
REPORT-FN is Flymake's callback."
  (when (process-live-p gom-flymake--proc)
    (kill-process gom-flymake--proc))
  (let ((source (current-buffer)))
    (cond
     ((not (buffer-file-name))
      (funcall report-fn :panic :explanation "buffer has no file"))
     ((not (executable-find gom-command))
      (funcall report-fn :panic
               :explanation (format "%s not found" gom-command)))
     (t
      (let* ((dir (file-name-directory (expand-file-name (buffer-file-name))))
             (out (generate-new-buffer " *gom-flymake*"))
             (default-directory dir))
        (setq gom-flymake--proc
              (make-process
               :name "gom-flymake"
               :buffer out
               :command (list gom-command "check" dir)
               :noquery t
               :connection-type 'pipe
               :stderr (get-buffer-create " *gom-flymake-stderr*")
               :sentinel
               (lambda (p _event)
                 (unless (process-live-p p)
                   (unwind-protect
                       (when (and (buffer-live-p source)
                                  (eq p (buffer-local-value
                                         'gom-flymake--proc source)))
                         (if (memq (process-exit-status p) '(0 1))
                             (with-current-buffer out
                               (funcall report-fn
                                        (gom-flymake--diagnostics source)))
                           (funcall report-fn :panic
                                    :explanation
                                    (format "gom check exited with %d"
                                            (process-exit-status p)))))
                     (kill-buffer out)))))))))))

;;; Major mode

;;;###autoload
(define-derived-mode gom-mode prog-mode "Gom"
  "Major mode for Gom source (.gom) and interface (.gomi) files.

With the tree-sitter Gom grammar installed (tree-sitter-gom/ in the
Gom repository) font-lock, imenu and indentation use `treesit';
otherwise the mode falls back to regexp-based highlighting,
`gom-indent-line' and `imenu-generic-expression'.  See
`gom-use-tree-sitter'.

\\{gom-mode-map}"
  :group 'gom
  :syntax-table gom-mode-syntax-table
  (setq-local comment-start "// ")
  (setq-local comment-end "")
  (setq-local comment-start-skip "\\(?://+\\|/\\*+\\)[ \t]*")
  (setq-local indent-tabs-mode nil)
  (setq-local electric-indent-chars
              (append '(?\} ?\) ?\] ?|) electric-indent-chars))
  (if (gom--treesit-available-p)
      (gom--treesit-setup)
    (setq-local font-lock-defaults '(gom-font-lock-keywords))
    (setq-local indent-line-function #'gom-indent-line)
    (setq-local imenu-generic-expression gom-imenu-generic-expression))
  (add-hook 'flymake-diagnostic-functions #'gom-flymake nil t))

;;;###autoload
(add-to-list 'auto-mode-alist '("\\.gomi?\\'" . gom-mode))

(provide 'gom-mode)

;;; gom-mode.el ends here
