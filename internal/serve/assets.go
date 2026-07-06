package serve

import (
	"embed"
	"io/fs"
)

//go:embed all:ui/dist
var embeddedDist embed.FS

func DistFS() (fs.FS, error) {
	return fs.Sub(embeddedDist, "ui/dist")
}
