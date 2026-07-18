//go:build !cgo

package clipboard

import "errors"

// Image reports no image: reading the clipboard needs cgo, which was
// disabled for this build. Pasting text is unaffected — the terminal
// delivers it as key input without going through this package.
func Image() ([]byte, error) {
	return nil, nil
}

// WriteText reports that the clipboard is unavailable: writing needs cgo,
// which was disabled for this build.
func WriteText(string) error {
	return errors.New("clipboard unavailable in this build (no cgo)")
}
