package checks

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rojosinalma/pawl/internal/report"
	"github.com/rojosinalma/pawl/internal/stack"
)

const (
	forbiddenFile = ".pawl/forbidden.txt"
	// binarySniff is how much of a file is searched for a NUL byte to call it binary, as git does.
	binarySniff = 8 << 10
	// maxScanBytes bounds the text checks: larger files are generated or data, not source anyone edits.
	maxScanBytes = 4 << 20
	// minReasonWords is the shortest reason a suppression or waiver may carry.
	minReasonWords = 3
)

// lockfiles carry hashes and versions, never directives a person wrote.
var lockfiles = map[string]bool{
	"go.sum": true, "package-lock.json": true, "bun.lock": true, "bun.lockb": true, "yarn.lock": true,
	"pnpm-lock.yaml": true, "Gemfile.lock": true, "uv.lock": true, "poetry.lock": true, "pdm.lock": true,
	"Cargo.lock": true, "mise.lock": true, "requirements.lock": true,
}

// toolConfigs are the configurations pawl writes for a tool; they name suppression directives to configure them,
// which is not a suppression in the code.
var toolConfigs = map[string]bool{
	".golangci.yml": true, ".golangci.yaml": true, "biome.json": true, "biome.jsonc": true, "ruff.toml": true,
	".rubocop.yml": true, "pawl.toml": true,
}

// readOptional returns the file's content, or "" when it cannot be read; for files whose absence another check
// reports.
func readOptional(root, rel string) string {
	raw, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return ""
	}
	return string(raw)
}

// lineOf returns the 1-based line of the first line containing needle, or 0.
func lineOf(content, needle string) int {
	if content == "" || needle == "" {
		return 0
	}
	for i, l := range strings.Split(content, "\n") {
		if strings.Contains(l, needle) {
			return i + 1
		}
	}
	return 0
}

// readText returns the lines of a text file; ok is false for binary, oversized or vanished files.
func readText(root, rel string) (lines []string, ok bool, err error) {
	full := filepath.Join(root, rel)
	info, err := os.Stat(full)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", rel, err)
	}
	if !info.Mode().IsRegular() || info.Size() > maxScanBytes {
		return nil, false, nil
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", rel, err)
	}
	if bytes.IndexByte(raw[:min(len(raw), binarySniff)], 0) >= 0 {
		return nil, false, nil
	}
	text := strings.TrimSuffix(string(raw), "\n")
	if text == "" {
		return nil, true, nil
	}
	return strings.Split(text, "\n"), true, nil
}

// redact keeps the first three characters of a forbidden match so the report never repeats it.
func redact(s string) string {
	runes := []rune(s)
	if len(runes) > 3 {
		runes = runes[:3]
	}
	return string(runes) + "..."
}

// forbiddenPatterns reads .pawl/forbidden.txt; ok is false when the project has none. Invalid patterns are findings.
func (r *run) forbiddenPatterns() ([]*regexp.Regexp, bool) {
	raw, err := os.ReadFile(filepath.Join(r.p.Root, forbiddenFile))
	if err != nil {
		return nil, false
	}
	var out []*regexp.Regexp
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		re, err := regexp.Compile(line)
		if err != nil {
			r.add(report.Errorf("repo.private", forbiddenFile, i+1, "pattern %d is not a valid regular expression: %v", len(out)+1, err).
				WithFix("fix the pattern (Go regexp syntax)"))
			re = nil
		}
		out = append(out, re)
	}
	return out, true
}

// checkPrivate verifies repo.private: no candidate file matches a forbidden pattern.
func (r *run) checkPrivate(files []string) error {
	const rule = "repo.private"
	if !r.active(rule) {
		return nil
	}
	patterns, ok := r.forbiddenPatterns()
	if !ok {
		return nil
	}
	r.mark(rule)
	for _, f := range files {
		if f == forbiddenFile {
			continue
		}
		lines, ok, err := readText(r.p.Root, f)
		if err != nil {
			return err
		}
		if ok {
			r.add(forbiddenFindings(f, lines, patterns)...)
		}
	}
	return nil
}

func forbiddenFindings(file string, lines []string, patterns []*regexp.Regexp) []report.Finding {
	var out []report.Finding
	for n, line := range lines {
		for i, re := range patterns {
			if re == nil {
				continue
			}
			if m := re.FindString(line); m != "" {
				out = append(out, report.Errorf("repo.private", file, n+1, "matches forbidden pattern %d (%s)", i+1, redact(m)).
					WithFix("remove or rename it; the patterns are in "+forbiddenFile))
			}
		}
	}
	return out
}

// hit is one suppression directive in a file.
type hit struct {
	File   string
	Line   int
	Kind   string
	Reason string
}

func (h hit) offender() string { return fmt.Sprintf("%s:%d %s", h.File, h.Line, h.Kind) }

// scannable reports whether the suppression scan reads the file at all.
func scannable(f string) bool {
	base := path.Base(f)
	// documentation describes directives; it does not use them
	prose := strings.HasSuffix(base, ".md") || strings.HasSuffix(base, ".mdx") || strings.HasSuffix(base, ".adoc") || strings.HasSuffix(base, ".rst")
	return !prose && !strings.HasPrefix(f, ".pawl/") && !lockfiles[base] && !strings.HasSuffix(base, ".lock") && !toolConfigs[f]
}

// suppressionHits scans the files for every active adapter's directives, once per run.
func (r *run) suppressionHits(files []string) ([]hit, error) {
	if r.hitsDone {
		return r.hits, nil
	}
	var sups []stack.Suppression
	for _, a := range r.p.Adapters {
		sups = append(sups, a.Suppressions()...)
	}
	var out []hit
	for _, f := range files {
		if !scannable(f) {
			continue
		}
		hits, err := scanFile(r.p.Root, f, sups)
		if err != nil {
			return nil, err
		}
		out = append(out, hits...)
	}
	r.hits, r.hitsDone = out, true
	return out, nil
}

func scanFile(root, f string, sups []stack.Suppression) ([]hit, error) {
	var mine []stack.Suppression
	for _, s := range sups {
		if stack.Match(f, s.Globs) {
			mine = append(mine, s)
		}
	}
	if len(mine) == 0 {
		return nil, nil
	}
	lines, ok, err := readText(root, f)
	if err != nil || !ok {
		return nil, err
	}
	var out []hit
	for n, line := range lines {
		for _, s := range mine {
			for _, m := range s.Pattern.FindAllStringSubmatch(line, -1) {
				out = append(out, hit{File: f, Line: n + 1, Kind: s.Kind, Reason: group(s.Pattern, m, "reason")})
			}
		}
	}
	return out, nil
}

func group(re *regexp.Regexp, m []string, name string) string {
	if i := re.SubexpIndex(name); i >= 0 && i < len(m) {
		return strings.TrimSpace(m[i])
	}
	return ""
}

// checkSuppressionReasons verifies the reason half of escape.suppressions: every directive explains itself.
func (r *run) checkSuppressionReasons(files []string) error {
	const rule = "escape.suppressions"
	if !r.active(rule) {
		return nil
	}
	r.mark(rule)
	hits, err := r.suppressionHits(files)
	if err != nil {
		return err
	}
	for _, h := range hits {
		if len(strings.Fields(h.Reason)) >= minReasonWords {
			continue
		}
		r.add(report.Errorf(rule, h.File, h.Line, "suppression without a reason (%s)", h.Kind).
			WithFix(fmt.Sprintf("say why after the directive, at least %d words: %s", minReasonWords, reasonSyntax(h.Kind))))
	}
	return nil
}

// reasonSyntax shows where the reason goes for each directive the adapters count. The directive names are split
// so this file does not count as a suppression itself.
func reasonSyntax(kind string) string {
	syntax := map[string]string{
		"nolint":             "//" + "nolint:errcheck // <reason>",
		"nosec":              "#" + "nosec G104 -- <reason>",
		"lint:ignore":        "//" + "lint:ignore SA1019 <reason>",
		"ast-grep-ignore":    "ast-grep" + "-ignore: rule-id -- <reason>",
		"typos-ignore":       "typos" + ": ignore -- <reason>",
		"gitleaks-allow":     "gitleaks" + ":allow -- <reason>",
		"shellcheck-disable": "# shellcheck " + "disable=SC2086 -- <reason>",
		"hadolint-ignore":    "# hadolint " + "ignore=DL3008 -- <reason>",
		"zizmor-ignore":      "# zizmor" + ": ignore[rule] -- <reason>",
		"biome-ignore":       "// biome" + "-ignore lint/group/rule: <reason>",
		"ts-ignore":          "// @ts" + "-ignore -- <reason>",
		"ts-expect-error":    "// @ts" + "-expect-error -- <reason>",
		"eslint-disable":     "// eslint" + "-disable-next-line rule -- <reason>",
		"oxlint-disable":     "// oxlint" + "-disable-next-line rule -- <reason>",
		"as-any":             "value as " + "any // <reason>",
		"noqa":               "# " + "noqa: E501 -- <reason>",
		"type-ignore":        "# type" + ": ignore[code] -- <reason>",
		"pyright-ignore":     "# pyright" + ": ignore[rule] -- <reason>",
		"rubocop:disable":    "# rubocop" + ":disable Cop/Name -- <reason>",
		"steep:ignore":       "# steep" + ":ignore <reason>",
		"allow":              "#[" + "allow(lint)] // <reason>",
		"expect":             "#[" + "expect(lint, reason = \"<reason>\")]",
		"unsafe":             "un" + "safe { // SAFETY: <reason>",
	}
	if s, ok := syntax[kind]; ok {
		return s
	}
	return "<directive> -- <reason>"
}

// oversized returns the source files longer than the file_lines budget.
func (r *run) oversized() ([]string, error) {
	limit := r.p.Manifest.Budgets.FileLines
	if limit <= 0 {
		return nil, nil // the manifest validation reports the budget
	}
	var out []string
	for _, f := range r.p.SourceFiles() {
		lines, ok, err := readText(r.p.Root, f)
		if err != nil {
			return nil, err
		}
		if ok && len(lines) > limit {
			out = append(out, f)
		}
	}
	return out, nil
}

var (
	// todoRe finds a TODO-style marker after a comment opener. The words are joined at run time so this file
	// carries no marker itself.
	todoRe = regexp.MustCompile(`(//|#|/\*|^\s*\*|--|;)[^\n]*\b(` + strings.Join([]string{"TO" + "DO", "FIX" + "ME", "X" + "XX", "HA" + "CK"}, "|") + `)\b`)
	// linkRe is what makes a marker tracked: a URL or an issue number.
	linkRe = regexp.MustCompile(`https?://\S+|(^|[\s(\[])#\d+\b`)
)

// unlinkedTodos returns the markers in non-test source files without a URL or issue reference on their line.
func (r *run) unlinkedTodos() ([]string, error) {
	var out []string
	for _, f := range r.p.SourceFiles() {
		if r.p.IsTest(f) {
			continue
		}
		lines, ok, err := readText(r.p.Root, f)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		for n, line := range lines {
			if todoRe.MatchString(line) && !linkRe.MatchString(line) {
				out = append(out, fmt.Sprintf("%s:%d", f, n+1))
			}
		}
	}
	return out, nil
}
