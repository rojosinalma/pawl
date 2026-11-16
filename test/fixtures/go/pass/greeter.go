// Package greeter builds greetings for named people.
package greeter

import (
	"errors"
	"strings"
)

// ErrEmptyName is returned when the name is blank.
var ErrEmptyName = errors.New("name is empty")

// Style selects the tone of a greeting.
type Style int

// The greeting styles.
const (
	Plain Style = iota
	Loud
)

// Greet returns the greeting for name in the given style.
func Greet(name string, style Style) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrEmptyName
	}
	switch style {
	case Plain:
		return "Hello, " + name + ".", nil
	case Loud:
		return "HELLO, " + strings.ToUpper(name) + "!", nil
	}
	return "", errors.New("unknown style")
}
