package termx

import (
	"bufio"
	"io"
)

// Key is one decoded key press. Printable keys set Rune; special keys set
// Name ("up", "down", "left", "right", "esc", "enter", "tab", "ctrl-c").
type Key struct {
	Rune rune
	Name string
}

// ReadKeys decodes r (a raw-mode terminal) into key presses on the returned
// channel, which is closed when r ends.
func ReadKeys(r io.Reader) <-chan Key {
	ch := make(chan Key, 16)
	go func() {
		defer close(ch)
		parseKeys(r, func(k Key) { ch <- k })
	}()
	return ch
}

func parseKeys(r io.Reader, emit func(Key)) {
	rd := bufio.NewReader(r)
	for {
		b, err := rd.ReadByte()
		if err != nil {
			return
		}
		switch b {
		case 3:
			emit(Key{Name: "ctrl-c"})
		case 9:
			emit(Key{Name: "tab"})
		case 10, 13:
			emit(Key{Name: "enter"})
		case 0x1b:
			if rd.Buffered() == 0 {
				emit(Key{Name: "esc"})
				continue
			}
			next, err := rd.ReadByte()
			if err != nil {
				return
			}
			if next != '[' && next != 'O' {
				emit(Key{Name: "esc"})
				_ = rd.UnreadByte()
				continue
			}
			// CSI / SS3: parameters then a final byte in 0x40..0x7e.
			for {
				c, err := rd.ReadByte()
				if err != nil {
					return
				}
				if c >= 0x40 && c <= 0x7e {
					switch c {
					case 'A':
						emit(Key{Name: "up"})
					case 'B':
						emit(Key{Name: "down"})
					case 'C':
						emit(Key{Name: "right"})
					case 'D':
						emit(Key{Name: "left"})
					}
					break
				}
			}
		default:
			if b < 0x80 {
				emit(Key{Rune: rune(b)})
				continue
			}
			_ = rd.UnreadByte()
			r, _, err := rd.ReadRune()
			if err != nil {
				return
			}
			emit(Key{Rune: r})
		}
	}
}
