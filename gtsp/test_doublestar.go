package main

import (
	"fmt"
	"github.com/bmatcuk/doublestar/v4"
	"path/filepath"
)

func main() {
	absPath, _ := filepath.Abs(".")
	pattern := filepath.Join(absPath, "**/*.go")
	opts := []doublestar.GlobOption{doublestar.WithFilesOnly()}
	matches, err := doublestar.FilepathGlob(pattern, opts...)
	fmt.Println("Error:", err)
	for i, m := range matches {
		if i < 5 {
			fmt.Println(m)
		}
	}
}
