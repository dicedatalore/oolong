//go:build !cgo

package clipboard

// Supported reports whether this build includes clipboard image support.
func Supported() bool { return false }

// Image reports no image: reading the clipboard needs cgo, which was
// disabled for this build. Pasting text is unaffected — the terminal
// delivers it as key input without going through this package.
func Image() ([]byte, error) {
	return nil, nil
}
