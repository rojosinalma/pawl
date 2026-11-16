package greeter

import (
	"errors"
	"testing"
)

func TestGreet(t *testing.T) {
	cases := []struct {
		name  string
		style Style
		want  string
	}{
		{"Ada", Plain, "Hello, Ada."},
		{" Ada ", Loud, "HELLO, ADA!"},
	}
	for _, c := range cases {
		got, err := Greet(c.name, c.style)
		if err != nil {
			t.Fatalf("Greet(%q): %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("Greet(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestGreetEmpty(t *testing.T) {
	if _, err := Greet("  ", Plain); !errors.Is(err, ErrEmptyName) {
		t.Fatalf("Greet(blank) error = %v, want ErrEmptyName", err)
	}
}
