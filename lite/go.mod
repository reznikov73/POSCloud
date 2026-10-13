module poscloud-lite

go 1.20

require (
	golang.org/x/sys v0.13.0
	poscloud-engine v0.0.0
)

require github.com/fsnotify/fsnotify v1.9.0 // indirect

replace poscloud-engine => ../engine
