package scaffold

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/stack"
)

// SyncState is what `pawl rules sync` did, or would do, with one rule file.
type SyncState string

// The states of a rule file. Only SyncUpdated and SyncCreated write it.
const (
	SyncCurrent SyncState = "current" // the file equals Pawl's rendering
	SyncUpdated SyncState = "updated" // nobody edited it; Pawl's rendering replaced it
	SyncCreated SyncState = "created" // a missing file of the .pawl/ pack (or a forced one) was written
	SyncEdited  SyncState = "edited"  // the project changed it, or Pawl has no record of writing it: kept
	SyncRemoved SyncState = "removed" // Pawl wrote it and the project deleted it: left deleted
	SyncAbsent  SyncState = "absent"  // never written here; init decides which tools a project gets, not sync
)

// packPrefix is the directory of the ast-grep pack: Pawl's own files, recreated when missing.
const packPrefix = ".pawl/"

// SyncedFile is the outcome for one rule file.
type SyncedFile struct {
	Path  string
	State SyncState
}

// SyncOptions of a `pawl rules sync` run.
type SyncOptions struct {
	DryRun bool     // decide and report, write nothing
	Force  []string // paths, relative to the root, that take Pawl's rendering whatever their state
}

// fileFacts are the inputs of the decision for one rule file: hashes as hashOf returns them, "" when absent.
type fileFacts struct {
	rendered string // Pawl's rendering for the project
	disk     string // the file on disk; "" when it does not exist
	recorded string // the record's hash; "" when Pawl has no record of writing it
	isPack   bool   // the file belongs to the .pawl/ pack
	isForced bool   // named with --force
}

// decide is the whole-file rule: Pawl replaces a file only when it is the one Pawl recorded writing, so one edit
// makes the file the project's.
func decide(f fileFacts) SyncState {
	switch {
	case f.disk == f.rendered:
		return SyncCurrent
	case f.disk == "" && (f.isPack || f.isForced):
		return SyncCreated
	case f.disk == "" && f.recorded != "":
		return SyncRemoved
	case f.disk == "":
		return SyncAbsent
	case f.isForced || f.recorded == f.disk:
		return SyncUpdated
	}
	return SyncEdited
}

// SyncRules brings the rule files of every active adapter (the tool configurations and the .pawl/ pack, rendered
// from the manifest) up to date where nobody edited them, and keeps the record of what Pawl wrote current. It
// returns every rendered file with its state, sorted by path.
func SyncRules(p *project.Project, opts SyncOptions) ([]SyncedFile, error) {
	rec, err := loadGenerated(p.Root)
	if err != nil {
		return nil, err
	}
	configs := renderConfigs(p)
	forced, err := forcedPaths(opts.Force, configs)
	if err != nil {
		return nil, err
	}
	out := make([]SyncedFile, 0, len(configs))
	for _, c := range configs {
		state, err := syncFile(p.Root, c, rec, forced[c.Path], opts.DryRun)
		if err != nil {
			return nil, err
		}
		out = append(out, SyncedFile{Path: c.Path, State: state})
	}
	if _, err := rec.save(p.Root, opts.DryRun); err != nil {
		return nil, err
	}
	return out, nil
}

// syncFile decides one file, writes Pawl's rendering when the decision says so, and updates the record in memory.
func syncFile(root string, c stack.ConfigFile, rec *generated, isForced, dry bool) (SyncState, error) {
	disk, err := diskHash(root, c.Path)
	if err != nil {
		return "", err
	}
	recorded, _ := rec.lookup(c.Path)
	state := decide(fileFacts{
		rendered: hashOf([]byte(c.Content)),
		disk:     disk,
		recorded: recorded,
		isPack:   strings.HasPrefix(c.Path, packPrefix),
		isForced: isForced,
	})
	switch state {
	case SyncUpdated, SyncCreated:
		if !dry {
			if err := writeFile(root, c.Path, c.Content, configMode(c)); err != nil {
				return "", fmt.Errorf("sync rules: %w", err)
			}
		}
		rec.record(c.Path, []byte(c.Content))
	case SyncCurrent:
		// an identical file is Pawl's whether or not it was recorded: this is how a project from before the
		// record gets one
		rec.record(c.Path, []byte(c.Content))
	case SyncEdited, SyncRemoved, SyncAbsent:
		// the project's file (or its absence) stands, and so does what the record says Pawl once wrote
	}
	return state, nil
}

// renderConfigs returns the configurations of every active adapter, sorted by path.
func renderConfigs(p *project.Project) []stack.ConfigFile {
	var out []stack.ConfigFile
	for _, a := range p.Adapters {
		out = append(out, a.Configs(p.Params)...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// forcedPaths checks that every forced path is one of the rendered files; the error names only the bad path.
func forcedPaths(paths []string, configs []stack.ConfigFile) (map[string]bool, error) {
	known := map[string]bool{}
	for _, c := range configs {
		known[c.Path] = true
	}
	out := map[string]bool{}
	for _, raw := range paths {
		rel := filepath.ToSlash(filepath.Clean(raw))
		if !known[rel] {
			return nil, fmt.Errorf("--force %s: not a file Pawl generates", raw)
		}
		out[rel] = true
	}
	return out, nil
}

// diskHash returns the hash of a file under the root, or "" when it does not exist.
func diskHash(root, rel string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(root, rel))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", rel, err)
	}
	return hashOf(raw), nil
}

// SyncAgents rewrites pawl's block in AGENTS.md from the manifest's waived rules; it reports whether it changed
// (dry: whether it would).
func SyncAgents(p *project.Project, dry bool) (bool, error) {
	path := filepath.Join(p.Root, agentsFile)
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("read %s: %w", agentsFile, err)
	}
	content, err := agentsContent(p, string(raw), err == nil)
	if err != nil {
		return false, err
	}
	if content == string(raw) {
		return false, nil
	}
	if dry {
		return true, nil
	}
	if err := writeFile(p.Root, agentsFile, content, 0o644); err != nil {
		return false, fmt.Errorf("sync agents: %w", err)
	}
	return true, nil
}
