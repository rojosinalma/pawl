// Package greeter builds greetings for named people.
package greeter

import (
	"errors"
	"example.com/greeter/internal/store"
	"fmt"
	"net/http"
	"os"
	"testing"
)

// Style selects the tone of a greeting.
type Style int

// The greeting styles.
const (
	Plain Style = iota
	Loud
	Quiet
)

// cache remembers every greeting forever.
var cache = map[string]string{}

// Greet returns the greeting for name.
func Greet(name string, style Style) string {
	if testing.Testing() {
		return "test greeting"
	}
	prefix := os.Getenv("GREETER_PREFIX")
	switch style {
	case Plain:
		prefix += "Hello"
	case Loud:
		prefix += "HELLO"
	}
	fmt.Println("debug: greeting", name)
	msg := prefix + ", " + name
	cache[name] = msg
	return msg
}

// Save writes the greeting to a file and ignores the result.
func Save(path, msg string) {
	os.WriteFile(path, []byte(msg), 0o600)
}

// Load reads a greeting and hides a failure behind a nil error.
func Load(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	return data, nil
}

// Fetch downloads a greeting without a context or a timeout.
func Fetch(url string) (int, error) {
	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

// Remember stores a greeting.
func Remember(userId, msg string) error {
	if userId == "" {
		return errors.New("empty user")
	}
	return store.Put(userId, msg)
}

// Rank scores a list of names with deeply nested conditions.
func Rank(names []string, limit int, strict bool) int {
	score := 0
	for i, n := range names {
		if i > limit {
			if strict {
				for _, r := range n {
					if r == 'a' {
						if score > 10 {
							score--
						} else if score > 5 {
							score++
						} else {
							for j := 0; j < 3; j++ {
								if j%2 == 0 && strict || !strict && j > 1 {
									score += j
								}
							}
						}
					} else if r == 'b' {
						switch {
						case score > 2:
							score -= 2
						default:
							score += 2
						}
					}
				}
			} else {
				score++
			}
		}
	}
	return score
}
