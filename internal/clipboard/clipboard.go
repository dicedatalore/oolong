//go:build cgo

// Package clipboard reads image attachments from the system clipboard.
package clipboard

import (
	"sync"

	xclipboard "golang.design/x/clipboard"
)

var initOnce = sync.OnceValue(xclipboard.Init)

// Supported reports whether this build includes clipboard image support.
func Supported() bool { return true }

// Image returns the system clipboard's image as PNG bytes, or nil if the
// clipboard holds no image.
func Image() ([]byte, error) {
	if err := initOnce(); err != nil {
		return nil, err
	}
	return xclipboard.Read(xclipboard.FmtImage), nil
}
