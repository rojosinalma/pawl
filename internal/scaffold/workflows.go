package scaffold

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/rojosinalma/pawl/internal/project"
)

// Action is a third-party GitHub Action pinned to a commit.
type Action struct {
	SHA string // full commit SHA
	Tag string // the release the SHA belongs to
}

// actions are the actions the workflow templates use, pinned to the commit of their latest release.
var actions = map[string]Action{
	"actions/checkout":   {SHA: "3d3c42e5aac5ba805825da76410c181273ba90b1", Tag: "v7.0.1"},
	"actions/setup-go":   {SHA: "b7ad1dad31e06c5925ef5d2fc7ad053ef454303e", Tag: "v7.0.0"},
	"jdx/mise-action":    {SHA: "236a21c3b91f5db6d4e174b348674a0f0caac06d", Tag: "v5.2.9"},
	"ruby/setup-ruby":    {SHA: "b3aaceaef7e0ee231b499e7b5f49c03fc1d32199", Tag: "v1.338.0"},
	"astral-sh/setup-uv": {SHA: "68559e99cc807c1eb7e9238fd43a15bba460eec2", Tag: "v10.3.3"},
	"oven-sh/setup-bun":  {SHA: "0c5077e51419868618aeaa5fe8019c62421857d6", Tag: "v2.2.0"},
}

// pawlRepo is where the workflows download pawl from.
const pawlRepo = "https://github.com/rojosinalma/pawl/releases"

var releaseVersion = regexp.MustCompile(`^v?(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)$`)

// workflowData is what the workflow templates render from.
type workflowData struct {
	DefaultBranch  string
	PawlURL        string
	Go             bool
	Ruby           bool
	UV             bool
	Bun            bool
	BunVersionFile string
	Rust           bool
	CheckTimeout   int // minutes
	NightlyTimeout int // minutes
}

var workflowFiles = []string{"check.yml", "nightly.yml", "review.yml"}

// writeWorkflows writes the three ring workflows when they are missing.
func writeWorkflows(p *project.Project, w *writer) error {
	t, err := template.New("workflows").Delims("[[", "]]").Funcs(template.FuncMap{"uses": uses}).ParseFS(templates, "templates/*.yml.tmpl")
	if err != nil {
		return fmt.Errorf("workflow templates: %w", err)
	}
	data := newWorkflowData(p, w)
	for _, name := range workflowFiles {
		var b bytes.Buffer
		if err := t.ExecuteTemplate(&b, name+".tmpl", data); err != nil {
			return fmt.Errorf("render %s: %w", name, err)
		}
		if err := w.create(".github/workflows/"+name, b.String(), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}

func newWorkflowData(p *project.Project, w *writer) workflowData {
	d := workflowData{
		DefaultBranch:  strings.TrimPrefix(p.Manifest.Base, "origin/"),
		PawlURL:        pawlURL(w.version),
		Go:             hasStack(p, "go") && w.exists("go.mod"),
		Ruby:           hasStack(p, "ruby"),
		UV:             hasStack(p, "python") && p.Params.Facts["uv"] == "true",
		Bun:            hasStack(p, "typescript") && p.Params.Facts["runtime"] == "bun",
		Rust:           hasStack(p, "rust"),
		CheckTimeout:   timeoutMinutes(p, "check"),
		NightlyTimeout: timeoutMinutes(p, "nightly"),
	}
	if d.DefaultBranch == "" {
		d.DefaultBranch = "main"
	}
	if d.Bun && w.exists(".bun-version") {
		d.BunVersionFile = ".bun-version"
	}
	return d
}

// pawlURL is the release tarball of this pawl version, or the latest release for a development build.
func pawlURL(version string) string {
	m := releaseVersion.FindStringSubmatch(version)
	if m == nil {
		return pawlRepo + "/latest/download/pawl_linux_amd64.tar.gz"
	}
	return fmt.Sprintf("%s/download/v%s/pawl_%s_linux_amd64.tar.gz", pawlRepo, m[1], m[1])
}

// timeoutMinutes is twice the ring's limit: the budget excludes setup, the job timeout only stops a hung run.
func timeoutMinutes(p *project.Project, ring string) int {
	_, limit, err := p.Manifest.Rings[ring].Durations()
	if err != nil || limit <= 0 {
		return 60
	}
	return int(2 * limit / time.Minute)
}

// uses renders a pinned action reference: owner/repo@sha # tag.
func uses(name string) (string, error) {
	a, ok := actions[name]
	if !ok {
		return "", fmt.Errorf("action %s is not pinned in scaffold.actions", name)
	}
	return fmt.Sprintf("%s@%s # %s", name, a.SHA, a.Tag), nil
}
