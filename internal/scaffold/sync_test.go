package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rojosinalma/pawl/internal/manifest"
	"github.com/rojosinalma/pawl/internal/project"
	"github.com/rojosinalma/pawl/internal/testrepo"
)

func TestDecide(t *testing.T) {
	const pawl, other, older = "sha256:pawl", "sha256:other", "sha256:older"
	for _, c := range []struct {
		name string
		f    fileFacts
		want SyncState
	}{
		{"identical, recorded", fileFacts{rendered: pawl, disk: pawl, recorded: pawl}, SyncCurrent},
		{"identical, no record: adopted", fileFacts{rendered: pawl, disk: pawl}, SyncCurrent},
		{"identical, stale record", fileFacts{rendered: pawl, disk: pawl, recorded: older}, SyncCurrent},
		{"identical, forced", fileFacts{rendered: pawl, disk: pawl, isForced: true}, SyncCurrent},
		{"unedited older rendering", fileFacts{rendered: pawl, disk: older, recorded: older}, SyncUpdated},
		{"edited", fileFacts{rendered: pawl, disk: other, recorded: older}, SyncEdited},
		{"no record, differs", fileFacts{rendered: pawl, disk: other}, SyncEdited},
		{"edited pack file", fileFacts{rendered: pawl, disk: other, recorded: older, isPack: true}, SyncEdited},
		{"edited, forced", fileFacts{rendered: pawl, disk: other, recorded: older, isForced: true}, SyncUpdated},
		{"no record, forced", fileFacts{rendered: pawl, disk: other, isForced: true}, SyncUpdated},
		{"missing pack file", fileFacts{rendered: pawl, isPack: true}, SyncCreated},
		{"missing recorded pack file", fileFacts{rendered: pawl, recorded: older, isPack: true}, SyncCreated},
		{"removed tool configuration", fileFacts{rendered: pawl, recorded: older}, SyncRemoved},
		{"never written", fileFacts{rendered: pawl}, SyncAbsent},
		{"never written, forced", fileFacts{rendered: pawl, isForced: true}, SyncCreated},
		{"removed, forced", fileFacts{rendered: pawl, recorded: older, isForced: true}, SyncCreated},
	} {
		if got := decide(c.f); got != c.want {
			t.Errorf("%s: decide = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestInitRecordsTheRuleFilesItCreates(t *testing.T) {
	dir := goShellProject(t, true)
	testrepo.Write(t, dir, ".golangci.yml", "version: \"2\"\n")
	res := mustInit(t, dir, Options{DryRun: true})
	if !hasItem(res.Created, GeneratedFile) {
		t.Errorf("dry-run Created lacks %s: %v", GeneratedFile, res.Created)
	}
	if _, err := os.Stat(filepath.Join(dir, GeneratedFile)); err == nil {
		t.Fatal("a dry-run init wrote the record")
	}

	res = mustInit(t, dir, Options{})
	if !hasItem(res.Created, GeneratedFile) {
		t.Errorf("Created lacks %s: %v", GeneratedFile, res.Created)
	}
	checkRecordedConfigs(t, dir)
}

// checkRecordedConfigs asserts the record holds exactly the configurations init wrote (all but the kept
// .golangci.yml), in its stable layout.
func checkRecordedConfigs(t *testing.T, dir string) {
	t.Helper()
	rec := mustLoadRecord(t, dir)
	p := loadProject(t, dir)
	for _, c := range renderConfigs(p) {
		h, ok := rec.lookup(c.Path)
		if c.Path == ".golangci.yml" {
			if ok {
				t.Errorf("the kept %s is recorded", c.Path)
			}
			continue
		}
		if !ok || h != hashOf([]byte(readFile(t, dir, c.Path))) {
			t.Errorf("%s: recorded %q, ok %v", c.Path, h, ok)
		}
	}
	// every configuration but the kept one is recorded, so the count rules out anything else (Makefile, hook)
	if len(rec.Files) != len(renderConfigs(p))-1 {
		t.Errorf("record has %d files, want %d", len(rec.Files), len(renderConfigs(p))-1)
	}
	raw := readFile(t, dir, GeneratedFile)
	if !strings.HasPrefix(raw, "{\n  \"version\": 1,\n  \"files\": {\n    \".editorconfig\": \"sha256:") || !strings.HasSuffix(raw, "}\n") {
		t.Errorf("record layout:\n%s", raw)
	}
	if changed, err := rec.save(dir, false); err != nil || changed {
		t.Errorf("saving an unchanged record = %v, %v", changed, err)
	}
}

func TestSyncRulesKeepsWhatTheProjectEdited(t *testing.T) {
	dir, p := initProject(t)
	// .typos.toml as an older Pawl wrote it: different content, and its hash on record
	setRecorded(t, dir, ".typos.toml", "# older pawl\n")
	edited := readFile(t, dir, ".golangci.yml") + "# ours\n"
	testrepo.Write(t, dir, ".golangci.yml", edited)
	removeFile(t, dir, ".pawl/rules/timeouts.yml")
	removeFile(t, dir, ".jscpd.json")
	before := mustLoadRecord(t, dir)

	dry := mustSync(t, p, SyncOptions{DryRun: true})
	assertStates(t, dry, map[string]SyncState{".typos.toml": SyncUpdated, ".golangci.yml": SyncEdited, ".pawl/rules/timeouts.yml": SyncCreated, ".jscpd.json": SyncRemoved})
	if readFile(t, dir, ".typos.toml") != "# older pawl\n" || fileExists(dir, ".pawl/rules/timeouts.yml") {
		t.Error("a dry run wrote a rule file")
	}
	if after := mustLoadRecord(t, dir); after.Files[".typos.toml"] != before.Files[".typos.toml"] {
		t.Error("a dry run changed the record")
	}

	got := mustSync(t, p, SyncOptions{})
	assertStates(t, got, map[string]SyncState{".typos.toml": SyncUpdated, ".golangci.yml": SyncEdited, ".pawl/rules/timeouts.yml": SyncCreated, ".jscpd.json": SyncRemoved})
	rec := mustLoadRecord(t, dir)
	for _, f := range []string{".typos.toml", ".pawl/rules/timeouts.yml"} {
		if rec.Files[f] != hashOf([]byte(readFile(t, dir, f))) {
			t.Errorf("%s: record %s does not match the file", f, rec.Files[f])
		}
	}
	if readFile(t, dir, ".golangci.yml") != edited {
		t.Error("the edited .golangci.yml was changed")
	}
	if rec.Files[".golangci.yml"] != before.Files[".golangci.yml"] || rec.Files[".jscpd.json"] != before.Files[".jscpd.json"] {
		t.Error("the record of a kept file changed")
	}
	if fileExists(dir, ".jscpd.json") {
		t.Error("a removed tool configuration was recreated")
	}

	again := mustSync(t, p, SyncOptions{})
	assertStates(t, again, map[string]SyncState{".typos.toml": SyncCurrent, ".golangci.yml": SyncEdited, ".jscpd.json": SyncRemoved})

	forced := mustSync(t, p, SyncOptions{Force: []string{"./.golangci.yml", ".jscpd.json"}})
	assertStates(t, forced, map[string]SyncState{".golangci.yml": SyncUpdated, ".jscpd.json": SyncCreated})
	if rec := mustLoadRecord(t, dir); rec.Files[".golangci.yml"] != hashOf([]byte(readFile(t, dir, ".golangci.yml"))) || readFile(t, dir, ".golangci.yml") == edited {
		t.Error("--force did not take Pawl's version and record it")
	}
}

func TestSyncRulesAdoptsTheFilesOfAProjectWithoutARecord(t *testing.T) {
	dir, p := initProject(t)
	removeFile(t, dir, GeneratedFile)
	testrepo.Write(t, dir, ".editorconfig", "root = true\n")
	got := mustSync(t, p, SyncOptions{})
	assertStates(t, got, map[string]SyncState{".editorconfig": SyncEdited, ".typos.toml": SyncCurrent})
	rec := mustLoadRecord(t, dir)
	if _, ok := rec.lookup(".editorconfig"); ok {
		t.Error("a differing file was adopted into the record")
	}
	if len(rec.Files) != len(got)-1 {
		t.Errorf("record has %d files, want every identical one (%d)", len(rec.Files), len(got)-1)
	}
}

func TestSyncRulesRendersTheManifestsConfigModule(t *testing.T) {
	dir, _ := initProject(t)
	const rule = ".pawl/rules/config-owner.yml"
	m := readFile(t, dir, manifest.FileName)
	testrepo.Write(t, dir, manifest.FileName, strings.Replace(m, `config_module = ""`, `config_module = "internal/config"`, 1))
	got := mustSync(t, loadProject(t, dir), SyncOptions{})
	assertStates(t, got, map[string]SyncState{rule: SyncUpdated})
	assertContains(t, rule, readFile(t, dir, rule), `"internal/config/**"`)
}

func TestSyncRulesRejectsForeignPathsAndBadRecords(t *testing.T) {
	dir, p := initProject(t)
	_, err := SyncRules(p, SyncOptions{Force: []string{"Makefile"}})
	if err == nil || err.Error() != "--force Makefile: not a file Pawl generates" {
		t.Errorf("--force Makefile: %v", err)
	}
	for _, bad := range []string{"{not json", `{"version": 2, "files": {}}`} {
		testrepo.Write(t, dir, GeneratedFile, bad)
		if _, err := SyncRules(p, SyncOptions{}); err == nil || !strings.Contains(err.Error(), GeneratedFile) {
			t.Errorf("record %q: %v", bad, err)
		}
	}
}

// initProject is a Go and shell project after init, loaded.
func initProject(t *testing.T) (string, *project.Project) {
	t.Helper()
	dir := goShellProject(t, true)
	mustInit(t, dir, Options{})
	return dir, loadProject(t, dir)
}

func loadProject(t *testing.T, dir string) *project.Project {
	t.Helper()
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustSync(t *testing.T, p *project.Project, opts SyncOptions) []SyncedFile {
	t.Helper()
	got, err := SyncRules(p, opts)
	if err != nil {
		t.Fatalf("SyncRules(%+v): %v", opts, err)
	}
	return got
}

func mustLoadRecord(t *testing.T, dir string) *generated {
	t.Helper()
	rec, err := loadGenerated(dir)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

// setRecorded writes content at rel and records it, as an older Pawl that rendered it so would have.
func setRecorded(t *testing.T, dir, rel, content string) {
	t.Helper()
	testrepo.Write(t, dir, rel, content)
	rec := mustLoadRecord(t, dir)
	rec.record(rel, []byte(content))
	if _, err := rec.save(dir, false); err != nil {
		t.Fatal(err)
	}
}

// assertStates checks the named files; every other file must be current, or absent.
func assertStates(t *testing.T, got []SyncedFile, want map[string]SyncState) {
	t.Helper()
	for _, f := range got {
		w, ok := want[f.Path]
		if !ok {
			w = SyncCurrent
		}
		if f.State != w && (ok || f.State != SyncAbsent) {
			t.Errorf("%s: %s, want %s", f.Path, f.State, w)
		}
	}
}

func removeFile(t *testing.T, dir, rel string) {
	t.Helper()
	if err := os.Remove(filepath.Join(dir, rel)); err != nil {
		t.Fatal(err)
	}
}

func fileExists(dir, rel string) bool {
	_, err := os.Stat(filepath.Join(dir, rel))
	return err == nil
}

func TestInitExtendsAnExistingRecord(t *testing.T) {
	dir, _ := initProject(t)
	removeFile(t, dir, manifest.FileName)
	removeFile(t, dir, ".typos.toml")
	rec := mustLoadRecord(t, dir)
	delete(rec.Files, ".typos.toml")
	if _, err := rec.save(dir, false); err != nil {
		t.Fatal(err)
	}
	res := mustInit(t, dir, Options{})
	if !hasItem(res.Merged, GeneratedFile) {
		t.Errorf("Merged lacks %s: %v", GeneratedFile, res.Merged)
	}
	if _, ok := mustLoadRecord(t, dir).lookup(".typos.toml"); !ok {
		t.Error("the recreated .typos.toml is not recorded")
	}
}

func TestSyncReportsUnreadableFiles(t *testing.T) {
	dir, p := initProject(t)
	removeFile(t, dir, ".typos.toml")
	if err := os.Mkdir(filepath.Join(dir, ".typos.toml"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncRules(p, SyncOptions{}); err == nil || !strings.Contains(err.Error(), ".typos.toml") {
		t.Errorf("a directory in place of a rule file: %v", err)
	}
	removeFile(t, dir, GeneratedFile)
	if err := os.Mkdir(filepath.Join(dir, GeneratedFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncRules(p, SyncOptions{}); err == nil || !strings.Contains(err.Error(), GeneratedFile) {
		t.Errorf("an unreadable record: %v", err)
	}
	removeFile(t, dir, manifest.FileName)
	if _, err := Init(dir, Options{}); err == nil || !strings.Contains(err.Error(), GeneratedFile) {
		t.Errorf("init over an unreadable record: %v", err)
	}
}

func TestSyncAgentsDryRunWritesNothing(t *testing.T) {
	dir, p := initProject(t)
	testrepo.Write(t, dir, agentsFile, "# ours\n")
	if changed, err := SyncAgents(p, true); err != nil || !changed {
		t.Errorf("dry SyncAgents = %v, %v", changed, err)
	}
	if readFile(t, dir, agentsFile) != "# ours\n" {
		t.Error("a dry SyncAgents wrote AGENTS.md")
	}
}
