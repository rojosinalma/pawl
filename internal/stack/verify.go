package stack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"

	"github.com/rojosinalma/pawl/internal/report"
)

// Format of a configuration file.
type Format string

const (
	YAML  Format = "yaml"
	JSON  Format = "json"
	JSONC Format = "jsonc"
	TOML  Format = "toml"
)

// Config is a parsed configuration file.
type Config struct {
	Path string
	Data any
}

// LoadConfig parses root/path in the given format. Missing files return (nil, nil).
func LoadConfig(root, path string, format Format) (*Config, error) {
	raw, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var data any
	switch format {
	case YAML:
		err = yaml.Unmarshal(raw, &data)
		data = normalizeYAML(data)
	case JSON:
		err = json.Unmarshal(raw, &data)
	case JSONC:
		err = json.Unmarshal(StripJSONC(raw), &data)
	case TOML:
		var m map[string]any
		err = toml.Unmarshal(raw, &m)
		data = m
	default:
		return nil, fmt.Errorf("unknown config format %q", format)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &Config{Path: path, Data: data}, nil
}

// normalizeYAML turns map[any]any (older decoders) into map[string]any; yaml.v3 already does, so this is a guard.
func normalizeYAML(v any) any {
	switch t := v.(type) {
	case map[any]any:
		out := map[string]any{}
		for k, val := range t {
			out[fmt.Sprint(k)] = normalizeYAML(val)
		}
		return out
	case map[string]any:
		for k, val := range t {
			t[k] = normalizeYAML(val)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = normalizeYAML(val)
		}
		return t
	}
	return v
}

// StripJSONC removes // and /* */ comments and trailing commas from JSONC so encoding/json can parse it.
func StripJSONC(raw []byte) []byte {
	var out []byte
	inString := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case inString:
			out = append(out, c)
			if c == '\\' && i+1 < len(raw) {
				out = append(out, raw[i+1])
				i++
			} else if c == '"' {
				inString = false
			}
		case c == '"':
			inString = true
			out = append(out, c)
		case c == '/' && i+1 < len(raw) && raw[i+1] == '/':
			for i < len(raw) && raw[i] != '\n' {
				i++
			}
			out = append(out, '\n')
		case c == '/' && i+1 < len(raw) && raw[i+1] == '*':
			i += 2
			for i+1 < len(raw) && !(raw[i] == '*' && raw[i+1] == '/') {
				i++
			}
			i++
		default:
			out = append(out, c)
		}
	}
	// trailing commas before } or ]
	re := regexp.MustCompile(`,(\s*[}\]])`)
	return re.ReplaceAll(out, []byte("$1"))
}

// Get returns the value at a dotted path ("linter.rules.suspicious.noConsole"); list indexes are numeric segments.
// Keys containing dots are written with a leading '/' separator form: "a/b.c" means key "b.c" under "a".
func (c *Config) Get(path string) (any, bool) {
	if c == nil {
		return nil, false
	}
	cur := c.Data
	for _, seg := range splitPath(path) {
		switch t := cur.(type) {
		case map[string]any:
			v, ok := t[seg]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(t) {
				return nil, false
			}
			cur = t[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

func splitPath(path string) []string {
	if strings.Contains(path, "/") {
		return strings.Split(path, "/")
	}
	return strings.Split(path, ".")
}

// Contains reports whether the list at path contains item (as a string).
func (c *Config) Contains(path, item string) bool {
	v, ok := c.Get(path)
	if !ok {
		return false
	}
	list, ok := v.([]any)
	if !ok {
		return false
	}
	for _, x := range list {
		if fmt.Sprint(x) == item {
			return true
		}
	}
	return false
}

// Verify builds verifiers from small declarative checks.
type Verify struct {
	Root   string
	File   string
	Format Format
	RuleID string
	Via    string
}

func (v Verify) load() (*Config, *report.Finding) {
	cfg, err := LoadConfig(v.Root, v.File, v.Format)
	if err != nil {
		f := report.Errorf(v.RuleID, v.File, 0, "cannot parse: %v", err)
		return nil, &f
	}
	if cfg == nil {
		f := report.Errorf(v.RuleID, v.File, 0, "missing; %s needs it for %s", v.Via, v.RuleID).WithFix("pawl init writes a default, or waive the rule in pawl.toml")
		return nil, &f
	}
	return cfg, nil
}

// NotEqual reports when the value at path equals one of the forbidden values (the ways to turn a rule off).
func NotEqual(file string, format Format, path string, forbidden ...any) func(Verify) []report.Finding {
	return func(v Verify) []report.Finding {
		v.File, v.Format = file, format
		cfg, f := v.load()
		if f != nil {
			return []report.Finding{*f}
		}
		val, ok := cfg.Get(path)
		if !ok {
			return nil
		}
		for _, bad := range forbidden {
			if fmt.Sprint(val) == fmt.Sprint(bad) {
				return []report.Finding{report.Errorf(v.RuleID, file, 0, "%s is %v, which turns %s off", path, val, v.Via).WithFix("re-enable it, or waive the rule in pawl.toml with a reason")}
			}
		}
		return nil
	}
}

// Equal reports when the value at path is missing or differs from want.
func Equal(file string, format Format, path string, want any) func(Verify) []report.Finding {
	return func(v Verify) []report.Finding {
		v.File, v.Format = file, format
		cfg, f := v.load()
		if f != nil {
			return []report.Finding{*f}
		}
		val, ok := cfg.Get(path)
		if !ok || fmt.Sprint(val) != fmt.Sprint(want) {
			return []report.Finding{report.Errorf(v.RuleID, file, 0, "%s must be %v for %s (found %v)", path, want, v.Via, val).WithFix("set it, or waive the rule in pawl.toml with a reason")}
		}
		return nil
	}
}

// Has reports when the list at path lacks item, unless the value at absentOK path makes it implied.
func Has(file string, format Format, path, item string) func(Verify) []report.Finding {
	return func(v Verify) []report.Finding {
		v.File, v.Format = file, format
		cfg, f := v.load()
		if f != nil {
			return []report.Finding{*f}
		}
		if !cfg.Contains(path, item) {
			return []report.Finding{report.Errorf(v.RuleID, file, 0, "%s must list %q for %s", path, item, v.Via).WithFix("add it, or waive the rule in pawl.toml with a reason")}
		}
		return nil
	}
}

// Lacks reports when the list at path contains item (a disabled rule, for example).
func Lacks(file string, format Format, path, item string) func(Verify) []report.Finding {
	return func(v Verify) []report.Finding {
		v.File, v.Format = file, format
		cfg, f := v.load()
		if f != nil {
			return []report.Finding{*f}
		}
		if cfg.Contains(path, item) {
			return []report.Finding{report.Errorf(v.RuleID, file, 0, "%s lists %q, which turns %s off", path, item, v.Via).WithFix("remove it, or waive the rule in pawl.toml with a reason")}
		}
		return nil
	}
}

// Matches reports when the file does not contain the regular expression (for configs without structure).
func Matches(file string, re *regexp.Regexp, want string) func(Verify) []report.Finding {
	return func(v Verify) []report.Finding {
		raw, err := os.ReadFile(filepath.Join(v.Root, file))
		if err != nil {
			return []report.Finding{report.Errorf(v.RuleID, file, 0, "missing; %s needs it for %s", v.Via, v.RuleID).WithFix("pawl init writes a default, or waive the rule in pawl.toml")}
		}
		if !re.Match(raw) {
			return []report.Finding{report.Errorf(v.RuleID, file, 0, "must contain %s for %s", want, v.Via).WithFix("add it, or waive the rule in pawl.toml with a reason")}
		}
		return nil
	}
}

// NotMatches reports when the file contains the regular expression (a way to turn a rule off).
func NotMatches(file string, re *regexp.Regexp, what string) func(Verify) []report.Finding {
	return func(v Verify) []report.Finding {
		raw, err := os.ReadFile(filepath.Join(v.Root, file))
		if err != nil {
			return nil
		}
		if re.Match(raw) {
			return []report.Finding{report.Errorf(v.RuleID, file, 0, "contains %s, which turns %s off", what, v.Via).WithFix("remove it, or waive the rule in pawl.toml with a reason")}
		}
		return nil
	}
}

// Exists reports when the file is missing.
func Exists(file string) func(Verify) []report.Finding {
	return func(v Verify) []report.Finding {
		if _, err := os.Stat(filepath.Join(v.Root, file)); err != nil {
			return []report.Finding{report.Errorf(v.RuleID, file, 0, "missing; %s needs it for %s", v.Via, v.RuleID).WithFix("pawl init writes a default, or waive the rule in pawl.toml")}
		}
		return nil
	}
}

// V builds a Verifier from a rule id, the tool it names and one or more declarative checks.
func V(rule, via string, checks ...func(Verify) []report.Finding) Verifier {
	return Verifier{RuleID: rule, Via: via, Check: func(root string) []report.Finding {
		var out []report.Finding
		for _, c := range checks {
			out = append(out, c(Verify{Root: root, RuleID: rule, Via: via})...)
		}
		return out
	}}
}
