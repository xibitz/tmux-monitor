package webassets

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var content embed.FS

func Files() fs.FS {
	files, err := fs.Sub(content, "dist")
	if err != nil {
		panic(err)
	}
	return files
}
