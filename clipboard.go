package main

import (
	"sync"

	"golang.design/x/clipboard"
)

var clipboardInit = sync.OnceValue(clipboard.Init)

// clipboardImage returns the system clipboard's image as PNG bytes, or nil
// if the clipboard holds no image.
func clipboardImage() ([]byte, error) {
	if err := clipboardInit(); err != nil {
		return nil, err
	}
	return clipboard.Read(clipboard.FmtImage), nil
}
