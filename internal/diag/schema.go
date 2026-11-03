package diag

// messageSchema is the JSON Schema of the spec section 12 message
// stream, emitted verbatim by `gom schema` (spec section 12, item 7).
// It describes a single message object: the stream is a sequence of
// these objects, one per line, always ending with kind "summary".
// "version" and the trailing number of "$id" bump on incompatible
// changes; additions that keep old consumers working (a new kind, a
// new optional field) do not require a bump.
//
// The "test" definition is provisional: gom test is not implemented
// yet, so its object is only constrained to carry kind "test".
const messageSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "gom:schema:message:1",
  "version": 1,
  "title": "gom compiler message",
  "description": "One object of the message stream gom commands emit (docs/spec.md section 12). The stream is JSON Lines: one of these objects per line, always ending with kind \"summary\".",
  "oneOf": [
    { "$ref": "#/$defs/diagnostic" },
    { "$ref": "#/$defs/summary" },
    { "$ref": "#/$defs/test" },
    { "$ref": "#/$defs/unformatted" },
    { "$ref": "#/$defs/applied" },
    { "$ref": "#/$defs/explanation" }
  ],
  "$defs": {
    "pos": {
      "type": "object",
      "description": "A source position: the 0-based UTF-8 byte offset is the source of truth; line (1-based) and col (1-based, Unicode code points) are derived views.",
      "required": ["line", "col", "offset"],
      "properties": {
        "line": { "type": "integer", "minimum": 1 },
        "col": { "type": "integer", "minimum": 1 },
        "offset": { "type": "integer", "minimum": 0 }
      }
    },
    "span": {
      "type": "object",
      "description": "A half-open byte range in one source file, both ends resolved.",
      "required": ["file", "start", "end"],
      "properties": {
        "file": { "type": "string" },
        "start": { "$ref": "#/$defs/pos" },
        "end": { "$ref": "#/$defs/pos" }
      }
    },
    "edit": {
      "type": "object",
      "description": "Replace the byte range by \"text\": an insertion when the span is empty, a deletion when the text is.",
      "required": ["file", "start", "end", "text"],
      "properties": {
        "file": { "type": "string" },
        "start": { "$ref": "#/$defs/pos" },
        "end": { "$ref": "#/$defs/pos" },
        "text": { "type": "string" }
      }
    },
    "fix": {
      "type": "object",
      "description": "A repair candidate attached to a diagnostic; \"applicability\" classifies how safely it applies.",
      "required": ["title", "applicability", "edits"],
      "properties": {
        "title": { "type": "string" },
        "applicability": { "enum": ["safe", "placeholder", "unsafe"] },
        "edits": {
          "type": "array",
          "items": { "$ref": "#/$defs/edit" }
        }
      }
    },
    "diagnostic": {
      "type": "object",
      "required": ["kind", "code", "severity", "message"],
      "properties": {
        "kind": { "const": "diagnostic" },
        "code": {
          "type": "string",
          "pattern": "^[EWN][0-9]{4}$",
          "description": "Stable identifier LCCNN: severity letter, two-digit category, two-digit serial. ` + "`gom explain <code>`" + ` describes it."
        },
        "severity": { "enum": ["note", "warning", "error"] },
        "message": { "type": "string", "description": "A single line of English; machine-readable detail goes in \"data\"." },
        "span": { "$ref": "#/$defs/span" },
        "source": { "type": "string", "description": "The tool that produced the diagnostic; \"go\" marks Go toolchain output mapped back to .gom (spec section 12 item 6). Omitted for gom's own diagnostics." },
        "data": { "type": "object", "description": "Structured detail; the keys are fixed per code." },
        "notes": {
          "type": "array",
          "items": { "type": "string" }
        },
        "fixes": {
          "type": "array",
          "items": { "$ref": "#/$defs/fix" }
        }
      }
    },
    "summary": {
      "type": "object",
      "description": "The aggregate object closing every stream; \"ok\" is true exactly when no error-severity diagnostic was emitted.",
      "required": ["kind", "ok", "errors", "warnings", "files", "outputs"],
      "properties": {
        "kind": { "const": "summary" },
        "ok": { "type": "boolean" },
        "errors": { "type": "integer", "minimum": 0 },
        "warnings": { "type": "integer", "minimum": 0 },
        "files": {
          "type": "array",
          "items": { "type": "string" },
          "description": "The .gom sources the command processed."
        },
        "outputs": {
          "type": "array",
          "items": { "type": "string" },
          "description": "The files the command produced or rewrote."
        }
      }
    },
    "test": {
      "type": "object",
      "description": "One test's outcome (gom test; provisional — the command is not implemented yet, the object is only constrained to carry kind \"test\").",
      "required": ["kind"],
      "properties": {
        "kind": { "const": "test" }
      }
    },
    "unformatted": {
      "type": "object",
      "description": "A file that needs formatting (gom fmt --check); \"diff\" is the unified diff toward gom fmt's output.",
      "required": ["kind", "file", "diff"],
      "properties": {
        "kind": { "const": "unformatted" },
        "file": { "type": "string" },
        "diff": { "type": "string" }
      }
    },
    "applied": {
      "type": "object",
      "description": "A fix gom fix applied (spec section 12); \"edits\" are resolved in the pre-edit sources, the same coordinate space the diagnostic used.",
      "required": ["kind", "code", "title", "edits"],
      "properties": {
        "kind": { "const": "applied" },
        "code": {
          "type": "string",
          "pattern": "^[EWN][0-9]{4}$",
          "description": "The code of the diagnostic the fix answered."
        },
        "title": { "type": "string", "description": "The fix's title as it appeared in \"fixes\"." },
        "edits": {
          "type": "array",
          "items": { "$ref": "#/$defs/edit" }
        }
      }
    },
    "explanation": {
      "type": "object",
      "description": "An error-code explanation (gom explain <code>) — the error-code table's entry for the code.",
      "required": ["kind", "code", "severity", "title", "phase", "explanation"],
      "properties": {
        "kind": { "const": "explanation" },
        "code": { "type": "string", "pattern": "^[EWN][0-9]{4}$" },
        "severity": { "enum": ["note", "warning", "error"] },
        "title": { "type": "string" },
        "phase": {
          "type": "string",
          "description": "The compiler component that emits the diagnostic."
        },
        "spec": { "type": "string", "description": "The spec.md section defining the rule." },
        "explanation": { "type": "string", "description": "Long-form Markdown prose: the rule, why it fires, how to fix the code." },
        "example": { "type": "string", "description": "A .gom fragment that triggers the diagnostic." },
        "retired": { "type": "boolean", "description": "True for codes no longer emitted; their numbers stay reserved." }
      }
    }
  }
}
`

// MessageSchema returns the JSON Schema of the message format
// (docs/spec.md section 12, item 7) as an indented JSON document
// ending in a newline — `gom schema` writes it to stdout verbatim.
// The schema describes one message object; a stream is a sequence
// of such objects terminated by a "summary".
func MessageSchema() []byte {
	return []byte(messageSchema + "\n")
}
