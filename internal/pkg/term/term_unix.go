//go:build unix

package term

import "golang.org/x/term"

func termReadPassword() ([]byte, error) {
	return term.ReadPassword(0)
}
