package config

import "testing"

func TestDefaultsAreEmpty(t *testing.T) {
	for _, name := range []string{"PAWL_BASE", "PAWL_FULL", "PAWL_VERBOSE", "PAWL_WORK_TYPE"} {
		t.Setenv(name, "")
	}
	if Base() != "" || Full() || Verbose() || WorkType() != "" {
		t.Errorf("defaults: %q %v %v %q", Base(), Full(), Verbose(), WorkType())
	}
}

func TestValuesAreRead(t *testing.T) {
	t.Setenv("PAWL_BASE", " origin/dev ")
	t.Setenv("PAWL_FULL", "1")
	t.Setenv("PAWL_VERBOSE", "1")
	t.Setenv("PAWL_WORK_TYPE", "bugfix\n")
	if Base() != "origin/dev" || !Full() || !Verbose() || WorkType() != "bugfix" {
		t.Errorf("values: %q %v %v %q", Base(), Full(), Verbose(), WorkType())
	}
	t.Setenv("PAWL_FULL", "true")
	if Full() {
		t.Error("only 1 turns the full run on")
	}
}
