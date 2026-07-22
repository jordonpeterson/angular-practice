// Package markdown is the purpose-built structural scanner from
// docs/design/implementation-options.md §2: it segments a context file into
// heading-keyed sections while preserving authored bytes verbatim, and offers
// the small set of content transforms the engine needs (import expansion,
// comment extraction, @-token escaping). It is not a CommonMark parser.
package markdown

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Section is a raw slice of a document. Key "" is the preamble (everything
// before the first H2, typically the H1 title); otherwise Key is the H2 text.
type Section struct {
	Key string
	Raw string // authored bytes, trailing whitespace trimmed
}

type Doc struct {
	Sections []Section
}

// Parse splits data into preamble + H2-delimited sections. Headings inside
// fenced code blocks do not split.
func Parse(data []byte) Doc {
	lines := strings.Split(string(data), "\n")
	var doc Doc
	var cur []string
	key := ""
	inFence := false
	flush := func() {
		raw := strings.TrimRight(strings.Join(cur, "\n"), " \t\n")
		if raw != "" || key != "" {
			doc.Sections = append(doc.Sections, Section{Key: key, Raw: raw})
		}
		cur = nil
	}
	started := false
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
		}
		if !inFence && strings.HasPrefix(line, "## ") {
			if started || len(cur) > 0 || key == "" {
				flush()
			}
			started = true
			key = strings.TrimSpace(strings.TrimPrefix(line, "## "))
		}
		cur = append(cur, line)
	}
	flush()
	return doc
}

// Serialize renders sections canonically: blocks joined by one blank line,
// single trailing newline (determinism rules, design §4.3).
func Serialize(doc Doc) []byte {
	var parts []string
	for _, s := range doc.Sections {
		if raw := strings.TrimRight(s.Raw, " \t\n"); raw != "" {
			parts = append(parts, raw)
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return []byte(strings.Join(parts, "\n\n") + "\n")
}

// Body returns a keyed section's content without its heading line.
func Body(s Section) string {
	if s.Key == "" {
		return s.Raw
	}
	if i := strings.IndexByte(s.Raw, '\n'); i >= 0 {
		return strings.TrimSpace(s.Raw[i+1:])
	}
	return ""
}

var (
	importLine = regexp.MustCompile(`^@(\S+)$`)
	comment    = regexp.MustCompile(`(?s)<!--.*?-->`)
	blankRuns  = regexp.MustCompile(`\n{3,}`)
	atToken    = regexp.MustCompile("(^|[^`\\w])(@[\\w][\\w./~-]*)")
)

// ExpandImports resolves Claude-style whole-line `@path` imports relative to
// dir, up to 4 hops (functionality map C1). Unresolvable imports are left as
// literal text. Lines inside code fences are never treated as imports.
func ExpandImports(s, dir string) string {
	return expand(s, dir, 4)
}

func expand(s, dir string, depth int) string {
	if depth == 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	inFence := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		m := importLine.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, m[1]))
		if err != nil {
			continue
		}
		imported := strings.TrimRight(string(data), "\n")
		lines[i] = expand(imported, filepath.Dir(filepath.Join(dir, m[1])), depth-1)
	}
	return strings.Join(lines, "\n")
}

// StripComments removes block HTML comments and normalizes the blank lines
// they leave behind. Used for block-content hashing: comments are file-local
// metadata and never participate in the merge (design §3.1).
func StripComments(s string) string {
	s = comment.ReplaceAllString(s, "")
	s = blankRuns.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// Comments returns the block HTML comments of s, in order.
func Comments(s string) []string {
	return comment.FindAllString(s, -1)
}

// EscapeAtTokens backtick-wraps bare @tokens so Claude Code does not treat
// them as imports (functionality map C6). Applied only to content synthesized
// into a Claude file — authored text is never rewritten.
func EscapeAtTokens(s string) string {
	return atToken.ReplaceAllString(s, "$1`$2`")
}
