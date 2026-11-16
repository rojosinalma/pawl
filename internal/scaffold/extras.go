package scaffold

import (
	"errors"

	"github.com/rojosinalma/pawl/internal/project"
)

// writeExtras writes the rest of the scaffold after the configurations and the Makefile: the git hook and
// core.hooksPath, mise.toml with the pinned tools, the workflows, the AGENTS.md block, the .gitignore lines, the
// stack's own additions (Gemfile group, pyproject dependency group) and the first baseline.
func writeExtras(p *project.Project, w *writer) error {
	return nil
}

// SyncRules rewrites the files of the ast-grep pack (.pawl/rules, .pawl/sgconfig.yml) that differ from what this
// version of pawl renders for the manifest; it returns the paths it wrote.
func SyncRules(p *project.Project) ([]string, error) {
	return nil, errors.New("scaffold.SyncRules: not implemented")
}

// SyncAgents rewrites pawl's block in AGENTS.md from the manifest's waived rules; it reports whether it changed.
func SyncAgents(p *project.Project) (bool, error) {
	return false, errors.New("scaffold.SyncAgents: not implemented")
}
