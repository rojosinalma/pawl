package scaffold

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// GeneratedFile records the hash of every rule file as Pawl wrote it, so `pawl rules sync` can tell a file nobody
// edited (safe to bring up to date) from one the project changed (left alone). It is committed with the project.
const GeneratedFile = ".pawl/generated.json"

// generatedVersion is the record format this pawl reads and writes.
const generatedVersion = 1

// generated is the content of GeneratedFile: slash-separated paths relative to the root, mapped to "sha256:<hex>".
type generated struct {
	Version int               `json:"version"`
	Files   map[string]string `json:"files"`
}

// loadGenerated reads the record under root; a missing record is an empty one (a project from before records).
func loadGenerated(root string) (*generated, error) {
	raw, err := os.ReadFile(filepath.Join(root, GeneratedFile))
	if errors.Is(err, fs.ErrNotExist) {
		return &generated{Version: generatedVersion, Files: map[string]string{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", GeneratedFile, err)
	}
	var g generated
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, fmt.Errorf("%s: %w", GeneratedFile, err)
	}
	if g.Version != generatedVersion {
		return nil, fmt.Errorf("%s: version %d; this pawl reads version %d", GeneratedFile, g.Version, generatedVersion)
	}
	if g.Files == nil {
		g.Files = map[string]string{}
	}
	return &g, nil
}

// lookup returns the recorded hash of a path; ok is false when Pawl has no record of writing it.
func (g *generated) lookup(rel string) (string, bool) {
	h, ok := g.Files[rel]
	return h, ok
}

// record stores the hash of content as what Pawl wrote at rel.
func (g *generated) record(rel string, content []byte) {
	g.Files[rel] = hashOf(content)
}

// save writes the record when its content changed, through a temporary file and a rename so a reader never sees
// half a record. It reports whether the content changed; dry only reports.
func (g *generated) save(root string, dry bool) (bool, error) {
	raw, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return false, fmt.Errorf("encode %s: %w", GeneratedFile, err)
	}
	raw = append(raw, '\n')
	if have, err := os.ReadFile(filepath.Join(root, GeneratedFile)); err == nil && bytes.Equal(have, raw) {
		return false, nil
	}
	if dry {
		return true, nil
	}
	tmp := GeneratedFile + ".tmp"
	if err := writeFile(root, tmp, string(raw), 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", GeneratedFile, err)
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return false, fmt.Errorf("write %s: %w", GeneratedFile, err)
	}
	defer r.Close()
	if err := r.Rename(tmp, GeneratedFile); err != nil {
		return false, fmt.Errorf("write %s: %w", GeneratedFile, err)
	}
	return true, nil
}

// hashOf is the record's form of a file's bytes.
func hashOf(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}
