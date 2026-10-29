package remote

import (
	"io/fs"
	"path/filepath"
	"slices"
)

// Paths locates the SOCKS5 rendered configuration.
type Paths struct {

	// SocksConf is the script `3proxy` is given. It carries a cleartext credential.
	SocksConf string

	// SocksDropIn is the unit drop-in that names the 3proxy binary when it is
	// not the one the unit assumes (socks_unit.go). Its content is not rendered
	// configuration and drift does not compare it: it is written by the apply,
	// once per box, naming whatever binary that box has.
	SocksDropIn string
}

// DefaultPaths is the on-disk layout for a real install.
func DefaultPaths() Paths { return RootedPaths("") }

// RootedPaths is the same layout relocated under root, for development against
// a scratch directory without root or systemd.
func RootedPaths(root string) Paths {
	return Paths{
		SocksConf:   filepath.Join(root, "/etc/open-linux-router/rendered/remote/3proxy.cfg"),
		SocksDropIn: filepath.Join(root, SocksDropInPath),
	}
}

// File is one rendered file.
type File struct {
	Path string
	Mode fs.FileMode
	Data []byte

	// Secret suppresses the contents on every surface that displays a file: the
	// plan diff, the drift report, the logs. The file is still written and
	// still compared byte for byte — only its *display* is withheld.
	Secret bool
}

// Rendered is the complete set of files a config produces, sorted by path so
// that drift detection compares like with like.
type Rendered struct {
	Files []File
}

// Get returns a file by path.
func (r Rendered) Get(path string) (File, bool) {
	i := slices.IndexFunc(r.Files, func(f File) bool { return f.Path == path })
	if i < 0 {
		return File{}, false
	}
	return r.Files[i], true
}

// Paths lists the rendered paths, in order.
func (r Rendered) Paths() []string {
	out := make([]string, len(r.Files))
	for i, f := range r.Files {
		out[i] = f.Path
	}
	return out
}
