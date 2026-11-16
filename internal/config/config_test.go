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

func TestInsideMakeFollowsMakeLevel(t *testing.T) {
	t.Setenv("MAKELEVEL", "")
	if InsideMake() {
		t.Error("an empty MAKELEVEL is outside make")
	}
	t.Setenv("MAKELEVEL", "1")
	if !InsideMake() {
		t.Error("MAKELEVEL=1 is inside make")
	}
}

func TestProcessEnvironment(t *testing.T) {
	t.Setenv("PATH", "/a:/b")
	t.Setenv("HOME", "/home/someone")
	if Path() != "/a:/b" {
		t.Errorf("Path = %q", Path())
	}
	if home, err := Home(); err != nil || home != "/home/someone" {
		t.Errorf("Home = %q, %v", home, err)
	}
	found := false
	for _, kv := range Environ() {
		found = found || kv == "PATH=/a:/b"
	}
	if !found {
		t.Error("Environ lacks PATH")
	}
}

func TestHomeUnset(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := Home(); err == nil {
		t.Error("want an error without HOME")
	}
}
