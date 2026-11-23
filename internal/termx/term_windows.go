//go:build windows

package termx

import (
	"syscall"
	"unsafe"
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode = kernel32.NewProc("SetConsoleMode")
	procGetCSBI        = kernel32.NewProc("GetConsoleScreenBufferInfo")
	procSetOutputCP    = kernel32.NewProc("SetConsoleOutputCP")
)

const (
	enableProcessedInput  = 0x0001
	enableLineInput       = 0x0002
	enableEchoInput       = 0x0004
	enableVTInput         = 0x0200
	enableProcessedOutput = 0x0001
	enableVTProcessing    = 0x0004
)

type coord struct{ X, Y int16 }
type smallRect struct{ Left, Top, Right, Bottom int16 }
type consoleScreenBufferInfo struct {
	Size              coord
	CursorPosition    coord
	Attributes        uint16
	Window            smallRect
	MaximumWindowSize coord
}

func getMode(h syscall.Handle) (uint32, error) {
	var m uint32
	r, _, e := procGetConsoleMode.Call(uintptr(h), uintptr(unsafe.Pointer(&m)))
	if r == 0 {
		return 0, e
	}
	return m, nil
}

func setMode(h syscall.Handle, m uint32) error {
	r, _, e := procSetConsoleMode.Call(uintptr(h), uintptr(m))
	if r == 0 {
		return e
	}
	return nil
}

func size() (cols, rows int, ok bool) {
	var info consoleScreenBufferInfo
	r, _, _ := procGetCSBI.Call(uintptr(syscall.Stdout), uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 0, 0, false
	}
	c := int(info.Window.Right-info.Window.Left) + 1
	rw := int(info.Window.Bottom-info.Window.Top) + 1
	return c, rw, c > 0 && rw > 0
}

func enableVT() error {
	_, _, _ = procSetOutputCP.Call(65001) // UTF-8
	m, err := getMode(syscall.Stdout)
	if err != nil {
		return err
	}
	return setMode(syscall.Stdout, m|enableProcessedOutput|enableVTProcessing)
}

func makeRaw() (func(), error) {
	h := syscall.Stdin
	old, err := getMode(h)
	if err != nil {
		return nil, err
	}
	nm := old&^(enableProcessedInput|enableLineInput|enableEchoInput) | enableVTInput
	if err := setMode(h, nm); err != nil {
		return nil, err
	}
	return func() { _ = setMode(h, old) }, nil
}
