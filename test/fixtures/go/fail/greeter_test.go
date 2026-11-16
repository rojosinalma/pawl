package greeter

import "testing"

func TestGreet(t *testing.T) {
	if Greet("Ada", Plain) == "" {
		t.Fatal("empty greeting")
	}
}

func TestLoud(t *testing.T) {
	t.Skip()
	if Greet("Ada", Loud) == "" {
		t.Fatal("empty greeting")
	}
}
