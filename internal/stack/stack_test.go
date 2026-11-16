package stack

import (
	"reflect"
	"testing"
)

func TestMatch(t *testing.T) {
	cases := []struct {
		path  string
		globs []string
		want  bool
	}{
		{"main.go", []string{"**/*.go"}, true},
		{"internal/a/b.go", []string{"**/*.go"}, true},
		{"go.mod", []string{"**/*.go"}, false},
		{"a/b_test.go", []string{"**/*_test.go"}, true},
		{"a/b.go", []string{"**/*_test.go"}, false},
		{"test/x.sh", []string{"**/test/**"}, true},
		{"pkg/test/x.sh", []string{"**/test/**"}, true},
		{"contest/x.sh", []string{"**/test/**"}, false},
		{"pkg/tests/test_a.py", []string{"**/tests/*.py"}, true},
		{"pkg/tests/deep/test_a.py", []string{"**/tests/*.py"}, false},
		{"tests/a/b.rs", []string{"tests/**"}, true},
		{"src/tests.rs", []string{"tests/**"}, false},
		{"bin/run", []string{"bin/*"}, true},
		{"bin/sub/run", []string{"bin/*"}, false},
		{".github/workflows/ci.yml", []string{".github/workflows/*.yml"}, true},
		{"deploy/Dockerfile", []string{"**/Dockerfile"}, true},
		{"Dockerfile", []string{"**/Dockerfile"}, true},
		{"Dockerfile.dev", []string{"**/Dockerfile", "**/Dockerfile.*"}, true},
		{"x.go", nil, false},
	}
	for _, c := range cases {
		if got := Match(c.path, c.globs); got != c.want {
			t.Errorf("Match(%q, %v) = %v, want %v", c.path, c.globs, got, c.want)
		}
	}
}

func TestFilterAndHasAny(t *testing.T) {
	files := []string{"a.go", "b_test.go", "README.md", "cmd/x/main.go"}
	if got, want := Filter(files, []string{"**/*.go"}), []string{"a.go", "b_test.go", "cmd/x/main.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Filter = %v, want %v", got, want)
	}
	if got := Filter(files, []string{"**/*.rs"}); got != nil {
		t.Errorf("Filter without match = %v, want nil", got)
	}
	if !HasAny(files, []string{"**/*.md"}) || HasAny(files, []string{"**/*.py"}) {
		t.Error("HasAny reports the wrong answer")
	}
}

func TestExcluded(t *testing.T) {
	prefixes := []string{"vendor/", "third_party", ""}
	cases := map[string]bool{
		"vendor":            true,
		"vendor/a/b.go":     true,
		"vendored/a.go":     false,
		"third_party/x.c":   true,
		"src/vendor/a.go":   false,
		"third_party_extra": false,
	}
	for path, want := range cases {
		if got := Excluded(path, prefixes); got != want {
			t.Errorf("Excluded(%q) = %v, want %v", path, got, want)
		}
	}
	if Excluded("a.go", nil) {
		t.Error("no prefixes excluded a path")
	}
}
