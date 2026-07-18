// Package engine implements the bidirectional 3-way block merge from
// docs/design/tool-design.md §2: no file is privileged; agentsync.lock is the
// merge base; blocks are keyed by heading path; an edit on one side
// propagates, divergent edits conflict per the configured strategy.
package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"agentsync/internal/markdown"
)

type Diag struct {
	Rule     string   `json:"rule"`
	Severity string   `json:"severity,omitempty"`
	Block    string   `json:"block,omitempty"`
	Files    []string `json:"files,omitempty"`
}

type fileSpec struct{ id, path string }

// Managed files, in reporting order. Merge participants are files, not
// providers (design §2): AGENTS.md serves Codex, OpenCode, and Cursor.
var managed = []fileSpec{
	{id: "claude-md", path: "CLAUDE.md"},
	{id: "agents-md", path: "AGENTS.md"},
}

const absentSentinel = "\x00absent"

type docState struct {
	spec   fileSpec
	exists bool
	raw    []byte
	order  []string
	secs   map[string]markdown.Section
	vals   map[string]string
}

type verdict struct {
	val        string
	absent     bool
	conflicted bool
	changed    []string // paths of files whose block changed vs base
}

// side is one existing file's opinion about a block.
type side struct {
	doc *docState
	val string // absentSentinel when the file lacks the block
}

// Sync reconciles the managed files in dir. check=true verifies without
// writing. Returns the exit code and diagnostics.
func Sync(dir string, check bool) (int, []Diag) {
	cfg := loadConfig(dir)
	lk, lockOrder := loadLock(dir)

	var docs []*docState
	for _, spec := range managed {
		docs = append(docs, readDoc(dir, spec))
	}

	allKeys := mergeOrder(docs, lockOrder)
	verdicts := map[string]*verdict{}
	var diags []Diag
	for _, key := range allKeys {
		verdicts[key] = decide(key, docs, lk, cfg, &diags)
	}
	diags = append(diags, detectRenames(allKeys, verdicts, lk)...)

	if check {
		diags = append(diags, checkDiags(allKeys, verdicts, docs, lk)...)
		return exitCode(diags), diags
	}

	for _, doc := range docs {
		render(dir, doc, allKeys, verdicts)
	}
	writeLock(dir, allKeys, verdicts, lk)
	return exitCode(diags), diags
}

func readDoc(dir string, spec fileSpec) *docState {
	d := &docState{spec: spec, secs: map[string]markdown.Section{}, vals: map[string]string{}}
	data, err := os.ReadFile(filepath.Join(dir, spec.path))
	if err != nil {
		return d
	}
	d.exists = true
	d.raw = data
	for _, sec := range markdown.Parse(data).Sections {
		d.order = append(d.order, sec.Key)
		d.secs[sec.Key] = sec
		d.vals[sec.Key] = blockValue(sec, dir)
	}
	return d
}

// blockValue is the merge value of a section: imports expanded (so an edit to
// a linked doc is an edit to the block, design §4.1), comments stripped (they
// are file-local, §3.1), whitespace normalized.
func blockValue(sec markdown.Section, dir string) string {
	return markdown.StripComments(markdown.ExpandImports(markdown.Body(sec), dir))
}

// mergeOrder picks the block ordering: the first existing file whose section
// sequence diverges from the lock defines the new structure; remaining known
// keys are appended so deletions still get verdicts.
func mergeOrder(docs []*docState, lockOrder []string) []string {
	var chosen []string
	for _, d := range docs {
		if d.exists && !equalStrings(d.order, lockOrder) {
			chosen = d.order
			break
		}
	}
	if chosen == nil {
		if lockOrder != nil {
			chosen = lockOrder
		} else {
			for _, d := range docs {
				if d.exists {
					chosen = d.order
					break
				}
			}
		}
	}
	seen := map[string]bool{}
	var keys []string
	add := func(k string) {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for _, k := range chosen {
		add(k)
	}
	for _, k := range lockOrder {
		add(k)
	}
	for _, d := range docs {
		for _, k := range d.order {
			add(k)
		}
	}
	return keys
}

func decide(key string, docs []*docState, lk map[string]string, cfg Config, diags *[]Diag) *verdict {
	var sides []side
	for _, d := range docs {
		if !d.exists {
			continue // a missing file has no opinion; it is a creation target
		}
		val, ok := d.vals[key]
		if !ok {
			val = absentSentinel
		}
		sides = append(sides, side{d, val})
	}

	var changed []side
	if lk == nil {
		// First run: every distinct present value is an opinion (design §2).
		if len(distinctVals(sides)) <= 1 {
			return adopt(sides)
		}
		changed = sides
	} else {
		baseHash, hasBase := lk[key]
		if !hasBase {
			baseHash = hashVal(absentSentinel)
		}
		for _, s := range sides {
			if hashVal(s.val) != baseHash {
				changed = append(changed, s)
			}
		}
		if len(changed) == 0 {
			return adopt(sides)
		}
		// Deletion is an opinion here (unlike first-run absence), so count
		// the sentinel as a distinct changed value.
		if len(distinctValsAll(changed)) == 1 {
			v := changed[0].val
			return &verdict{val: v, absent: v == absentSentinel, changed: paths(changed)}
		}
	}

	// Divergent edits: resolve per strategy (design §2.1).
	if cfg.Strategy == "priority" {
		for _, id := range cfg.Priority {
			for _, s := range changed {
				if s.doc.spec.id == id {
					*diags = append(*diags, Diag{Rule: "conflict-resolved", Severity: "info", Block: key})
					return &verdict{val: s.val, absent: s.val == absentSentinel, changed: paths(changed)}
				}
			}
		}
	}
	*diags = append(*diags, Diag{Rule: "conflict", Severity: "error", Block: key, Files: paths(changed)})
	return &verdict{conflicted: true, changed: paths(changed)}
}

func adopt(sides []side) *verdict {
	for _, s := range sides {
		if s.val != absentSentinel {
			return &verdict{val: s.val}
		}
	}
	return &verdict{absent: true}
}

// detectRenames pairs a deleted key with an added key carrying the same
// content and reports it (design §3.1: rename = delete + add, matched).
func detectRenames(keys []string, verdicts map[string]*verdict, lk map[string]string) []Diag {
	if lk == nil {
		return nil
	}
	var diags []Diag
	for _, added := range keys {
		v := verdicts[added]
		if _, inBase := lk[added]; inBase || v.absent || v.conflicted || len(v.changed) == 0 {
			continue
		}
		for _, deleted := range keys {
			dv := verdicts[deleted]
			baseHash, inBase := lk[deleted]
			if inBase && dv.absent && baseHash == hashVal(v.val) {
				diags = append(diags, Diag{Rule: "rename-detected", Severity: "info", Block: added})
				break
			}
		}
	}
	return diags
}

func checkDiags(keys []string, verdicts map[string]*verdict, docs []*docState, lk map[string]string) []Diag {
	var diags []Diag
	for _, key := range keys {
		v := verdicts[key]
		if v.conflicted || len(v.changed) == 0 {
			continue // conflicts already reported; unchanged blocks are clean
		}
		outdated := false
		for _, d := range docs {
			if !d.exists {
				outdated = true
				continue
			}
			val, ok := d.vals[key]
			if v.absent {
				if ok {
					outdated = true
				}
			} else if !ok || val != v.val {
				outdated = true
			}
		}
		if outdated {
			diags = append(diags, Diag{Rule: "unpropagated-edits", Severity: "error", Block: key, Files: v.changed})
		}
	}
	return diags
}

func render(dir string, doc *docState, keys []string, verdicts map[string]*verdict) {
	var out markdown.Doc
	for _, key := range keys {
		v := verdicts[key]
		if v.conflicted {
			if sec, ok := doc.secs[key]; ok {
				out.Sections = append(out.Sections, sec)
			}
			continue
		}
		if v.absent {
			continue
		}
		if val, ok := doc.vals[key]; ok && val == v.val {
			out.Sections = append(out.Sections, doc.secs[key])
			continue
		}
		out.Sections = append(out.Sections, synthesize(doc, key, v.val))
	}
	data := markdown.Serialize(out)
	if data == nil {
		return
	}
	if !doc.exists || !bytes.Equal(data, doc.raw) {
		_ = os.WriteFile(filepath.Join(dir, doc.spec.path), data, 0o644)
	}
}

// synthesize builds a section from a merged value: @-tokens are escaped for
// Claude (C6), and the file's own comments are preserved in place (C5).
func synthesize(doc *docState, key, val string) markdown.Section {
	content := val
	if doc.spec.id == "claude-md" {
		content = markdown.EscapeAtTokens(content)
	}
	if old, ok := doc.secs[key]; ok {
		for _, c := range markdown.Comments(markdown.Body(old)) {
			content += "\n\n" + c
		}
	}
	raw := content
	if key != "" {
		raw = "## " + key + "\n\n" + content
	}
	return markdown.Section{Key: key, Raw: raw}
}

// --- lockfile ---

type lockBlock struct {
	Key  string `json:"key"`
	Hash string `json:"hash"`
}

type lockFile struct {
	Version int         `json:"version"`
	Blocks  []lockBlock `json:"blocks"`
}

func loadLock(dir string) (map[string]string, []string) {
	data, err := os.ReadFile(filepath.Join(dir, "agentsync.lock"))
	if err != nil {
		return nil, nil
	}
	var lf lockFile
	if json.Unmarshal(data, &lf) != nil {
		return nil, nil
	}
	m := map[string]string{}
	var order []string
	for _, b := range lf.Blocks {
		m[b.Key] = b.Hash
		order = append(order, b.Key)
	}
	return m, order
}

func writeLock(dir string, keys []string, verdicts map[string]*verdict, old map[string]string) {
	lf := lockFile{Version: 1}
	for _, key := range keys {
		v := verdicts[key]
		switch {
		case v.conflicted:
			if h, ok := old[key]; ok {
				lf.Blocks = append(lf.Blocks, lockBlock{key, h}) // conflict persists
			}
		case !v.absent:
			lf.Blocks = append(lf.Blocks, lockBlock{key, hashVal(v.val)})
		}
	}
	data, _ := json.MarshalIndent(lf, "", "  ")
	data = append(data, '\n')
	path := filepath.Join(dir, "agentsync.lock")
	if cur, err := os.ReadFile(path); err == nil && bytes.Equal(cur, data) {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// --- config ---

type Config struct {
	Strategy string
	Priority []string
}

// loadConfig reads the [conflict] table from agentsync.toml. Minimal
// hand-rolled subset for now; swap for go-toml/v2 when the config grows
// (implementation-options §3).
func loadConfig(dir string) Config {
	cfg := Config{Strategy: "fail"}
	data, err := os.ReadFile(filepath.Join(dir, "agentsync.toml"))
	if err != nil {
		return cfg
	}
	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		if section != "conflict" {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		switch key {
		case "strategy":
			cfg.Strategy = strings.Trim(val, `"`)
		case "priority":
			for _, item := range strings.Split(strings.Trim(val, "[]"), ",") {
				if item = strings.Trim(strings.TrimSpace(item), `"`); item != "" {
					cfg.Priority = append(cfg.Priority, item)
				}
			}
		}
	}
	return cfg
}

// --- helpers ---

func distinctVals(sides []side) map[string]bool {
	m := map[string]bool{}
	for _, s := range sides {
		if s.val != absentSentinel {
			m[s.val] = true
		}
	}
	return m
}

func distinctValsAll(sides []side) map[string]bool {
	m := map[string]bool{}
	for _, s := range sides {
		m[s.val] = true
	}
	return m
}

func paths(sides []side) []string {
	var out []string
	for _, s := range sides {
		out = append(out, s.doc.spec.path)
	}
	return out
}

func hashVal(v string) string {
	if v == absentSentinel {
		return "absent"
	}
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

func exitCode(diags []Diag) int {
	for _, d := range diags {
		if d.Severity == "error" {
			return 1
		}
	}
	return 0
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
