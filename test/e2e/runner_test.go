// Package e2e runs the spec/ fixtures against the built agentsync binary.
//
// Contract per fixture directory spec/<id>/ (docs/design/tool-design.md §7):
//
//	base/      GIVEN  — last-synced tree; the runner runs `sync` here to
//	                    generate agentsync.lock (absent => first run, no lock)
//	edit/      WHEN   — overlay applied over base; edit/_delete lists files to
//	                    remove (one path per line)
//	cmd        WHEN   — commands to run, one per line (default: "sync")
//	expected/  THEN   — exact resulting tree (agentsync.lock is not compared)
//	report.json THEN  — {"exit": N, "diagnostics": [...]} of the LAST command
//	expected-stdout.txt THEN (optional) — exact stdout of the last command
//
// The runner is hermetic: it drives only the agentsync binary, never real
// agent CLIs (those belong to the conformance probe, design §9.4).
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type diagnostic struct {
	Rule     string   `json:"rule"`
	Severity string   `json:"severity,omitempty"`
	Block    string   `json:"block,omitempty"`
	Files    []string `json:"files,omitempty"`
}

type report struct {
	Exit        int          `json:"exit"`
	Diagnostics []diagnostic `json:"diagnostics"`
}

func TestSpec(t *testing.T) {
	root := repoRoot(t)
	bin := buildBinary(t, root)

	specDir := filepath.Join(root, "spec")
	entries, err := os.ReadDir(specDir)
	if err != nil {
		t.Fatalf("read spec dir: %v", err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fixture := filepath.Join(specDir, e.Name())
		t.Run(e.Name(), func(t *testing.T) {
			runFixture(t, bin, fixture)
		})
	}
}

func runFixture(t *testing.T, bin, fixture string) {
	work := t.TempDir()

	// The fixture's config applies to setup and to the command under test.
	if data, err := os.ReadFile(filepath.Join(fixture, "agentsync.toml")); err == nil {
		writeFile(t, filepath.Join(work, "agentsync.toml"), data)
	}

	// GIVEN: copy base and let the tool produce the lock.
	base := filepath.Join(fixture, "base")
	if dirExists(base) {
		copyTree(t, base, work)
		// Setup sync; exit code deliberately ignored (stub tolerated).
		runTool(t, bin, work, "", []string{"sync"})
	}

	// WHEN: apply the edit overlay.
	edit := filepath.Join(fixture, "edit")
	if dirExists(edit) {
		applyOverlay(t, edit, work)
	}

	// WHEN: run the command(s); the last one is asserted.
	reportPath := filepath.Join(t.TempDir(), "report.json")
	var lastExit int
	var lastStdout string
	for _, args := range commands(t, fixture) {
		lastExit, lastStdout = runTool(t, bin, work, reportPath, args)
	}

	// THEN: exit code.
	want := loadReport(t, filepath.Join(fixture, "report.json"))
	if lastExit != want.Exit {
		t.Errorf("exit code: got %d, want %d", lastExit, want.Exit)
	}

	// THEN: tree equality (agentsync.lock excluded — tool-internal).
	diffTrees(t, filepath.Join(fixture, "expected"), work)

	// THEN: diagnostics (structural, order-insensitive).
	got := loadOptionalReport(reportPath)
	if !equalDiagnostics(got.Diagnostics, want.Diagnostics) {
		t.Errorf("diagnostics:\n  got  %s\n  want %s",
			mustJSON(got.Diagnostics), mustJSON(want.Diagnostics))
	}

	// THEN: stdout golden, when the fixture pins one.
	if golden, err := os.ReadFile(filepath.Join(fixture, "expected-stdout.txt")); err == nil {
		if lastStdout != string(golden) {
			t.Errorf("stdout:\n--- got ---\n%s\n--- want ---\n%s", lastStdout, golden)
		}
	}
}

// commands reads the fixture's cmd file (one command per line); default: sync.
func commands(t *testing.T, fixture string) [][]string {
	data, err := os.ReadFile(filepath.Join(fixture, "cmd"))
	if err != nil {
		return [][]string{{"sync"}}
	}
	var cmds [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if fields := strings.Fields(line); len(fields) > 0 {
			cmds = append(cmds, fields)
		}
	}
	if len(cmds) == 0 {
		t.Fatalf("cmd file present but empty")
	}
	return cmds
}

func runTool(t *testing.T, bin, dir, reportPath string, args []string) (int, string) {
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	if reportPath != "" {
		cmd.Env = append(cmd.Env, "AGENTSYNC_REPORT="+reportPath)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit := 0
	if ee, ok := err.(*exec.ExitError); ok {
		exit = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	return exit, stdout.String()
}

func loadReport(t *testing.T, path string) report {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("fixture missing report.json: %v", err)
	}
	var r report
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return r
}

// loadOptionalReport tolerates a missing/empty report file (stub phase).
func loadOptionalReport(path string) report {
	data, err := os.ReadFile(path)
	if err != nil {
		return report{}
	}
	var r report
	_ = json.Unmarshal(data, &r)
	return r
}

func diffTrees(t *testing.T, expected, got string) {
	// Every expected file must exist with exact bytes.
	seen := map[string]bool{}
	walk(t, expected, func(rel string, data []byte) {
		seen[rel] = true
		gotData, err := os.ReadFile(filepath.Join(got, rel))
		if err != nil {
			t.Errorf("missing file %s", rel)
			return
		}
		if !bytes.Equal(gotData, data) {
			t.Errorf("file %s:\n--- got ---\n%s\n--- want ---\n%s", rel, gotData, data)
		}
	})
	// No unexpected extras (lock is tool-internal; config is runner-provided).
	walk(t, got, func(rel string, _ []byte) {
		if rel == "agentsync.lock" || rel == "agentsync.toml" {
			return
		}
		if !seen[rel] {
			t.Errorf("unexpected file %s", rel)
		}
	})
}

func walk(t *testing.T, root string, fn func(rel string, data []byte)) {
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fn(filepath.ToSlash(rel), data)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("walk %s: %v", root, err)
	}
}

func copyTree(t *testing.T, src, dst string) {
	walk(t, src, func(rel string, data []byte) {
		writeFile(t, filepath.Join(dst, rel), data)
	})
}

func applyOverlay(t *testing.T, edit, work string) {
	// Deletions first.
	if data, err := os.ReadFile(filepath.Join(edit, "_delete")); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				if err := os.Remove(filepath.Join(work, line)); err != nil {
					t.Fatalf("delete %s: %v", line, err)
				}
			}
		}
	}
	walk(t, edit, func(rel string, data []byte) {
		if rel == "_delete" {
			return
		}
		writeFile(t, filepath.Join(work, rel), data)
	})
}

func writeFile(t *testing.T, path string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func equalDiagnostics(a, b []diagnostic) bool {
	if len(a) != len(b) {
		return false
	}
	key := func(d diagnostic) string {
		return d.Rule + "|" + d.Severity + "|" + d.Block + "|" + strings.Join(d.Files, ",")
	}
	as, bs := make([]string, len(a)), make([]string, len(b))
	for i, d := range a {
		as[i] = key(d)
	}
	for i, d := range b {
		bs[i] = key(d)
	}
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func repoRoot(t *testing.T) string {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func buildBinary(t *testing.T, root string) string {
	bin := filepath.Join(t.TempDir(), "agentsync")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/agentsync")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

var _ = fmt.Sprintf // keep fmt import if unused paths change
