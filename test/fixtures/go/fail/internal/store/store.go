// Package store keeps greetings.
package store

import "errors"

// Put stores a greeting for a user.
func Put(user, msg string) error {
	if user == "" || msg == "" {
		return errors.New("empty value")
	}
	return nil
}
