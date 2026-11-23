//go:build !linux && !darwin && !freebsd && !netbsd && !openbsd && !windows

package termx

func size() (cols, rows int, ok bool) { return 0, 0, false }

func makeRaw() (func(), error) { return func() {}, nil }

func enableVT() error { return nil }
