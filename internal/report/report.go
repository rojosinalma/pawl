// Package report holds pawl's findings and renders them as text, JSON, SARIF or GitHub annotations.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Severity of a finding: errors fail the run, warnings are printed.
type Severity string

const (
	Error   Severity = "error"
	Warning Severity = "warning"
	Note    Severity = "note"
)

// Finding is one problem: where, which rule, what, and how to fix it.
type Finding struct {
	RuleID   string   `json:"rule"`
	Severity Severity `json:"severity"`
	File     string   `json:"file,omitempty"`
	Line     int      `json:"line,omitempty"`
	Message  string   `json:"message"`
	Fix      string   `json:"fix,omitempty"`
}

// Errorf builds an error finding.
func Errorf(rule, file string, line int, format string, args ...any) Finding {
	return Finding{RuleID: rule, Severity: Error, File: file, Line: line, Message: fmt.Sprintf(format, args...)}
}

// Warnf builds a warning finding.
func Warnf(rule, file string, line int, format string, args ...any) Finding {
	return Finding{RuleID: rule, Severity: Warning, File: file, Line: line, Message: fmt.Sprintf(format, args...)}
}

// WithFix attaches the command or edit that resolves the finding.
func (f Finding) WithFix(fix string) Finding {
	f.Fix = fix
	return f
}

// Report is the outcome of a run.
type Report struct {
	Tool     string    `json:"tool"`
	Version  string    `json:"version"`
	Ring     string    `json:"ring,omitempty"`
	Findings []Finding `json:"findings"`
	Checked  []string  `json:"checked"`
	Skipped  []Skipped `json:"skipped,omitempty"`
}

// Skipped records a check that did not run and why.
type Skipped struct {
	RuleID string `json:"rule"`
	Reason string `json:"reason"`
}

// Add appends findings.
func (r *Report) Add(f ...Finding) { r.Findings = append(r.Findings, f...) }

// Skip records a skipped check.
func (r *Report) Skip(rule, reason string) { r.Skipped = append(r.Skipped, Skipped{rule, reason}) }

// Failed reports whether any finding is an error.
func (r *Report) Failed() bool {
	for _, f := range r.Findings {
		if f.Severity == Error {
			return true
		}
	}
	return false
}

// Errors counts error findings.
func (r *Report) Errors() int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == Error {
			n++
		}
	}
	return n
}

// Sort orders findings by rule, file, line.
func (r *Report) Sort() {
	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if a.RuleID != b.RuleID {
			return a.RuleID < b.RuleID
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
}

// Format names an output format.
type Format string

const (
	Text   Format = "text"
	JSON   Format = "json"
	SARIF  Format = "sarif"
	GitHub Format = "github"
)

// ParseFormat validates a format name.
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case Text, JSON, SARIF, GitHub:
		return Format(s), nil
	}
	return "", fmt.Errorf("unknown format %q (text, json, sarif, github)", s)
}

// Write renders the report.
func (r *Report) Write(w io.Writer, format Format) error {
	r.Sort()
	switch format {
	case JSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	case SARIF:
		return writeSARIF(w, r)
	case GitHub:
		for _, f := range r.Findings {
			level := "error"
			if f.Severity != Error {
				level = "warning"
			}
			loc := ""
			if f.File != "" {
				loc = fmt.Sprintf(" file=%s", f.File)
				if f.Line > 0 {
					loc += fmt.Sprintf(",line=%d", f.Line)
				}
			}
			msg := f.Message
			if f.Fix != "" {
				msg += " Fix: " + f.Fix
			}
			fmt.Fprintf(w, "::%s%s,title=%s::%s\n", level, loc, f.RuleID, escapeGH(msg))
		}
		return nil
	default:
		return writeText(w, r)
	}
}

func writeText(w io.Writer, r *Report) error {
	for _, f := range r.Findings {
		loc := f.File
		if f.Line > 0 {
			loc = fmt.Sprintf("%s:%d", f.File, f.Line)
		}
		if loc != "" {
			loc += ": "
		}
		marker := ""
		if f.Severity != Error {
			marker = string(f.Severity) + ": "
		}
		fmt.Fprintf(w, "%s%s%s: %s\n", loc, marker, f.RuleID, f.Message)
		if f.Fix != "" {
			fmt.Fprintf(w, "    fix: %s\n", f.Fix)
		}
	}
	for _, s := range r.Skipped {
		fmt.Fprintf(w, "skipped: %s: %s\n", s.RuleID, s.Reason)
	}
	errs := r.Errors()
	warns := len(r.Findings) - errs
	switch {
	case errs == 0 && warns == 0:
		fmt.Fprintf(w, "pawl: %d checks, no findings\n", len(r.Checked))
	case errs == 0:
		fmt.Fprintf(w, "pawl: %d checks, %d warnings\n", len(r.Checked), warns)
	default:
		fmt.Fprintf(w, "pawl: %d checks, %d errors, %d warnings\n", len(r.Checked), errs, warns)
	}
	return nil
}

func escapeGH(s string) string {
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, "\r", "%0D")
	s = strings.ReplaceAll(s, "\n", "%0A")
	return s
}

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string            `json:"id"`
	ShortDescription map[string]string `json:"shortDescription"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   map[string]any  `json:"message"`
	Locations []sarifLocation `json:"locations,omitempty"`
}

type sarifLocation struct {
	PhysicalLocation struct {
		ArtifactLocation struct {
			URI string `json:"uri"`
		} `json:"artifactLocation"`
		Region *struct {
			StartLine int `json:"startLine"`
		} `json:"region,omitempty"`
	} `json:"physicalLocation"`
}

func writeSARIF(w io.Writer, r *Report) error {
	rules := map[string]bool{}
	run := sarifRun{Tool: sarifTool{Driver: sarifDriver{Name: r.Tool, Version: r.Version, InformationURI: "https://github.com/rojosinalma/pawl"}}}
	for _, f := range r.Findings {
		if !rules[f.RuleID] {
			rules[f.RuleID] = true
			run.Tool.Driver.Rules = append(run.Tool.Driver.Rules, sarifRule{ID: f.RuleID, ShortDescription: map[string]string{"text": f.RuleID}})
		}
		level := "error"
		if f.Severity == Warning {
			level = "warning"
		} else if f.Severity == Note {
			level = "note"
		}
		msg := f.Message
		if f.Fix != "" {
			msg += " Fix: " + f.Fix
		}
		res := sarifResult{RuleID: f.RuleID, Level: level, Message: map[string]any{"text": msg}}
		if f.File != "" {
			var loc sarifLocation
			loc.PhysicalLocation.ArtifactLocation.URI = f.File
			if f.Line > 0 {
				loc.PhysicalLocation.Region = &struct {
					StartLine int `json:"startLine"`
				}{f.Line}
			}
			res.Locations = []sarifLocation{loc}
		}
		run.Results = append(run.Results, res)
	}
	if run.Results == nil {
		run.Results = []sarifResult{}
	}
	log := sarifLog{Schema: "https://json.schemastore.org/sarif-2.1.0.json", Version: "2.1.0", Runs: []sarifRun{run}}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
}
