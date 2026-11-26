package iface

import (
	"slices"
	"strings"

	"github.com/podhmo/gom/internal/syntax"
)

// privateClaim is one `//gom:private <declaration>` claim from a
// prior interface. The declaration is stored as written; its
// canonical form is compared against regenerated candidates.
type privateClaim struct {
	name  string
	canon string
	text  string
}

// scanPrivate reads file-level private claims. Unlike member
// directives they cannot attach to the hidden declaration — the
// declaration is absent — so any top-level comment line may carry one.
func scanPrivate(src []byte) map[string]privateClaim {
	lines := directiveLines(src, "//gom:private ")
	if len(lines) == 0 {
		return nil
	}
	out := map[string]privateClaim{}
	for _, line := range lines {
		text := strings.TrimSpace(strings.TrimPrefix(line, "//gom:private "))
		if text == "" {
			continue
		}
		f, _ := syntax.ParseInterface([]byte("package p\n\n"+text+"\n"), "<private>.gomi")
		if f == nil || len(f.Decls) != 1 {
			continue
		}
		var name string
		switch d := f.Decls[0].(type) {
		case *syntax.FnDecl:
			if d.Name != nil {
				name = d.Name.Name
			}
		case *syntax.LetDecl:
			names := map[string]bool{}
			bindNames(d.Pat, names)
			if len(names) == 1 {
				for n := range names {
					name = n
				}
			}
		case *syntax.TypeDecl:
			if d.Name != nil {
				name = d.Name.Name
			}
		case *syntax.ImportDecl, *syntax.TestDecl, *syntax.ExternDecl:
		default:
			panic("unreachable")
		}
		if name == "" {
			continue
		}
		out[name] = privateClaim{name: name, canon: canonicalPrivateText(text, importAliases(src)), text: text}
	}
	return out
}

// directiveLines returns exact-prefix line comments outside block
// comments. It deliberately does not apply attachment rules: private
// claims name a member that is not present to attach to.
func directiveLines(src []byte, prefix string) []string {
	var out []string
	inBlock := false
	for _, raw := range strings.Split(string(src), "\n") {
		inside := inBlock
		inBlock = updateBlockState(raw, inBlock)
		line := strings.TrimSpace(raw)
		if !inside && strings.HasPrefix(line, prefix) {
			out = append(out, line)
		}
	}
	return out
}

// scanAbstract reads attached `//gom:abstract <name>` claims. A claim
// is meaningful only on a prior abstract type declaration; attaching
// it to a concrete declaration cannot widen the implementation's
// hidden representation.
func scanAbstract(src []byte) map[string]bool {
	counts := priorAbstractTypes(src)
	if len(counts) == 0 {
		return nil
	}
	out := map[string]bool{}
	for _, line := range attachedDirectivePairs(src, "//gom:abstract ") {
		fields := strings.Fields(strings.TrimPrefix(line.text, "//gom:abstract "))
		target := strings.Fields(stripInlineBlockComments(line.target))
		if len(fields) == 1 && len(target) >= 2 && target[0] == "type" && target[1] == fields[0] {
			if _, ok := counts[fields[0]]; ok {
				out[fields[0]] = true
			}
		}
	}
	return out
}

// priorAbstractTypes parses abstract type declarations in a prior
// interface and returns their type-parameter counts. Regeneration may
// carry an abstract claim only while that header shape is unchanged.
func priorAbstractTypes(src []byte) map[string]int {
	if len(src) == 0 {
		return nil
	}
	f, _ := syntax.ParseInterface(src, "<prior>.gomi")
	if f == nil {
		return nil
	}
	out := map[string]int{}
	for _, decl := range f.Decls {
		d, ok := decl.(*syntax.TypeDecl)
		if !ok || d.Name == nil {
			continue
		}
		if _, ok := d.Body.(*syntax.AbstractBody); ok {
			out[d.Name.Name] = len(d.Params)
		}
	}
	return out
}

// importAliases maps the interface's rendered import qualifiers to
// their canonical paths. Import paths, not collision-dependent alias
// numbering, are the stable identity private signatures compare.
func importAliases(src []byte) map[string]string {
	f, _ := syntax.ParseInterface(src, "<prior>.gomi")
	if f == nil {
		return nil
	}
	out := map[string]string{}
	for _, decl := range f.Decls {
		im, ok := decl.(*syntax.ImportDecl)
		if !ok {
			continue
		}
		alias := pathBase(im.Path)
		if im.Name != nil && im.Name.Name != "" {
			alias = im.Name.Name
		}
		out[alias] = canonicalIfaceImportPath(im.Path)
	}
	return out
}

// canonicalIfaceImportPath unfolds the toolchain stdlib short names
// for signature comparison. It is hand-mirrored from gomimport's
// stdlib table because iface sits below the import loader.
func canonicalIfaceImportPath(path string) string {
	switch path {
	case "list", "option", "result", "cmp":
		return "github.com/podhmo/gom/stdlib/" + path
	default:
		return path
	}
}

// stripInlineBlockComments removes complete block comments from one
// declaration-start line for directive-target recognition. The parser
// itself sees through them; the textual scanner must too.
func stripInlineBlockComments(line string) string {
	for {
		start := strings.Index(line, "/*")
		if start < 0 {
			return line
		}
		end := strings.Index(line[start+2:], "*/")
		if end < 0 {
			return line[:start]
		}
		line = line[:start] + " " + line[start+2+end+2:]
	}
}

func canonicalPrivateText(text string, aliases map[string]string) string {
	return normalizeQualifiers(canonicalSig(text), aliases)
}

// normalizeQualifiers rewrites qualifier prefixes to their import
// paths inside an already canonical signature. The rewriting is only
// a comparison key, not surface syntax, so path characters need no
// escaping.
func normalizeQualifiers(sig string, aliases map[string]string) string {
	keys := make([]string, 0, len(aliases))
	for alias := range aliases {
		keys = append(keys, alias)
	}
	slices.SortFunc(keys, func(a, b string) int { return len(b) - len(a) })

	var out strings.Builder
	inRaw, inString, escaped := false, false, false
	for i := 0; i < len(sig); {
		c := sig[i]
		if inRaw {
			out.WriteByte(c)
			if c == '`' {
				inRaw = false
			}
			i++
			continue
		}
		if inString {
			out.WriteByte(c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			i++
			continue
		}
		if c == '`' {
			inRaw = true
			out.WriteByte(c)
			i++
			continue
		}
		if c == '"' {
			inString = true
			out.WriteByte(c)
			i++
			continue
		}
		if i == 0 || !isIdentByte(sig[i-1]) {
			matched := false
			for _, alias := range keys {
				needle := alias + "."
				if strings.HasPrefix(sig[i:], needle) {
					out.WriteByte('<')
					out.WriteString(aliases[alias])
					out.WriteString(">.")
					i += len(needle)
					matched = true
					break
				}
			}
			if matched {
				continue
			}
		}
		out.WriteByte(c)
		i++
	}
	return out.String()
}

func isIdentByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
