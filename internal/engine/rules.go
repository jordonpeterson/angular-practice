// Scoped-rule sync (functionality map C2/C3): .cursor/rules/*.mdc and
// .claude/rules/*.md are two representations of one rule; Codex gets the rule
// lowered to a nested AGENTS.md (directory-prefix globs) or attached to the
// root AGENTS.md (anything else), with fidelity diagnostics either way.
package engine

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"agentsync/internal/markdown"
)

type ruleValue struct {
	globs  []string
	always bool
	body   string
}

func (v ruleValue) key() string {
	return strings.Join(v.globs, ",") + "\x00" + strconv.FormatBool(v.always) + "\x00" + v.body
}

type ruleState struct {
	name   string
	mdc    *ruleValue // nil when .cursor/rules/<name>.mdc is absent
	claude *ruleValue // nil when .claude/rules/<name>.md is absent
}

type plannedWrite struct {
	rule string
	path string
	data []byte
}

func scanRules(dir string) []*ruleState {
	m := map[string]*ruleState{}
	get := func(name string) *ruleState {
		if r, ok := m[name]; ok {
			return r
		}
		r := &ruleState{name: name}
		m[name] = r
		return r
	}
	scan := func(sub, ext string, parse func(string) ruleValue, set func(*ruleState, ruleValue)) {
		ents, err := os.ReadDir(filepath.Join(dir, sub))
		if err != nil {
			return
		}
		for _, e := range ents {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ext) {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, sub, e.Name()))
			if err != nil {
				continue
			}
			v := parse(string(data))
			set(get(strings.TrimSuffix(e.Name(), ext)), v)
		}
	}
	scan(".cursor/rules", ".mdc", parseMdc, func(r *ruleState, v ruleValue) { r.mdc = &v })
	scan(".claude/rules", ".md", parseClaudeRule, func(r *ruleState, v ruleValue) { r.claude = &v })

	var out []*ruleState
	for _, r := range m {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

func ruleNames(rules []*ruleState) map[string]bool {
	m := map[string]bool{}
	for _, r := range rules {
		m[r.name] = true
	}
	return m
}

// processRules resolves each rule 3-way and plans per-provider artifacts.
// Root-attached rules are appended to agentsOut (the rendered root AGENTS.md);
// agentsRuleSecs holds the authored sections previously attached there.
func processRules(dir string, rules []*ruleState, lk map[string]string, agentsOut *markdown.Doc, agentsRuleSecs map[string]markdown.Section) (diags []Diag, writes []plannedWrite, locks []lockBlock) {
	for _, r := range rules {
		winner, conflicted := resolveRule(r, lk)
		if conflicted {
			diags = append(diags, Diag{Rule: "conflict", Severity: "error", Block: r.name,
				Files: []string{".cursor/rules/" + r.name + ".mdc", ".claude/rules/" + r.name + ".md"}})
			continue
		}
		// Manual / Agent-Requested (no globs, not always-on): Cursor-only
		// activation, unrepresentable in v1 targets (C3).
		if len(winner.globs) == 0 && !winner.always {
			diags = append(diags, Diag{Rule: "no-cursor-only-activation", Severity: "warn", Block: r.name})
			continue
		}

		if r.mdc == nil || r.mdc.key() != winner.key() {
			writes = append(writes, plannedWrite{r.name, filepath.Join(dir, ".cursor/rules", r.name+".mdc"), synthMdc(winner)})
		}
		if r.claude == nil || r.claude.key() != winner.key() {
			writes = append(writes, plannedWrite{r.name, filepath.Join(dir, ".claude/rules", r.name+".md"), synthClaudeRule(winner)})
		}

		if prefix, ok := dirPrefix(winner.globs); ok && !winner.always {
			// Lossy-recoverable for Codex; invisible to OpenCode (below-cwd
			// files are never loaded there).
			writes = append(writes, plannedWrite{r.name, filepath.Join(dir, prefix, "AGENTS.md"), []byte(winner.body + "\n")})
			diags = append(diags,
				Diag{Rule: "codex-dir-nesting", Severity: "info", Block: r.name},
				Diag{Rule: "opencode-no-scoped-form", Severity: "warn", Block: r.name})
		} else {
			// Attach at the common ancestor: a section in root AGENTS.md.
			if sec, ok := agentsRuleSecs[r.name]; ok && strings.TrimSpace(markdown.Body(sec)) == winner.body {
				agentsOut.Sections = append(agentsOut.Sections, sec)
			} else {
				agentsOut.Sections = append(agentsOut.Sections, markdown.Section{Key: r.name, Raw: "## " + r.name + "\n\n" + winner.body})
			}
			if !winner.always {
				diags = append(diags, Diag{Rule: "portable-globs-only", Severity: "warn", Block: r.name})
			}
		}
		locks = append(locks, lockBlock{Key: "rule:" + r.name, Hash: hashVal(winner.key())})
	}
	return diags, writes, locks
}

// resolveRule is the 3-way merge for a rule's two representations. The Claude
// side cannot express alwaysApply, so equal globs+body with differing flags
// still conflict — acceptable until a fixture pins richer semantics.
func resolveRule(r *ruleState, lk map[string]string) (ruleValue, bool) {
	switch {
	case r.claude == nil:
		return *r.mdc, false
	case r.mdc == nil:
		return *r.claude, false
	}
	if r.mdc.key() == r.claude.key() {
		return *r.mdc, false
	}
	if base, ok := lk["rule:"+r.name]; ok {
		mChanged := hashVal(r.mdc.key()) != base
		cChanged := hashVal(r.claude.key()) != base
		if mChanged && !cChanged {
			return *r.mdc, false
		}
		if cChanged && !mChanged {
			return *r.claude, false
		}
	}
	return ruleValue{}, true
}

// dirPrefix reports whether globs reduce to a single clean directory prefix
// (e.g. "src/api/**" -> "src/api"), the only shape Codex nesting can express.
func dirPrefix(globs []string) (string, bool) {
	if len(globs) != 1 || !strings.HasSuffix(globs[0], "/**") {
		return "", false
	}
	p := strings.TrimSuffix(globs[0], "/**")
	if p == "" || strings.ContainsAny(p, "*?[{") {
		return "", false
	}
	return p, true
}

// --- parsing & synthesis ---

func parseMdc(s string) ruleValue {
	fm, body := splitFrontmatter(s)
	v := ruleValue{body: strings.TrimSpace(body)}
	for _, line := range fm {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "globs":
			for _, g := range strings.Split(val, ",") {
				if g = strings.Trim(strings.TrimSpace(g), `"`); g != "" {
					v.globs = append(v.globs, g)
				}
			}
		case "alwaysApply":
			v.always = strings.TrimSpace(val) == "true"
		}
	}
	return v
}

func parseClaudeRule(s string) ruleValue {
	fm, body := splitFrontmatter(s)
	v := ruleValue{body: strings.TrimSpace(body)}
	inPaths := false
	for _, line := range fm {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "paths:"):
			inPaths = true
		case inPaths && strings.HasPrefix(t, "- "):
			v.globs = append(v.globs, strings.Trim(strings.TrimPrefix(t, "- "), `"`))
		default:
			inPaths = false
		}
	}
	return v
}

func splitFrontmatter(s string) (fm []string, body string) {
	lines := strings.Split(s, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, s
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return lines[1:i], strings.Join(lines[i+1:], "\n")
		}
	}
	return nil, s
}

func synthMdc(v ruleValue) []byte {
	var b strings.Builder
	b.WriteString("---\nglobs: " + strings.Join(v.globs, ",") + "\nalwaysApply: " + strconv.FormatBool(v.always) + "\n---\n")
	b.WriteString(v.body + "\n")
	return []byte(b.String())
}

func synthClaudeRule(v ruleValue) []byte {
	var b strings.Builder
	if len(v.globs) > 0 {
		b.WriteString("---\npaths:\n")
		for _, g := range v.globs {
			b.WriteString("  - \"" + g + "\"\n")
		}
		b.WriteString("---\n")
	}
	b.WriteString(v.body + "\n")
	return []byte(b.String())
}
