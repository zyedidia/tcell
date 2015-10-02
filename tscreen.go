// Copyright 2015 The TCell Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use file except in compliance with the License.
// You may obtain a copy of the license at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tcell

import (
	"bytes"
	"io"
	"os"
	"strconv"
	"sync"
	"unicode/utf8"
)

func NewTerminfoScreen() (Screen, error) {
	ti, e := LookupTerminfo(os.Getenv("TERM"))
	if e != nil {
		return nil, e
	}
	t := &tScreen{ti: ti}

	t.keys = make(map[Key][]byte)
	if len(ti.Mouse) > 0 {
		t.mouse = []byte(ti.Mouse)
	}
	t.prepareKeys()
	t.w = ti.Columns
	t.h = ti.Lines
	t.sigwinch = make(chan os.Signal, 1)
	// environment overrides
	if i, _ := strconv.Atoi(os.Getenv("LINES")); i != 0 {
		t.h = i
	}
	if i, _ := strconv.Atoi(os.Getenv("COLUMNS")); i != 0 {
		t.w = i
	}

	return t, nil
}

// tScreen represents a screen backed by a terminfo implementation.
type tScreen struct {
	ti       *Terminfo
	w        int
	h        int
	in       *os.File
	out      *os.File
	curstyle Style
	style    Style
	evch     chan Event
	sigwinch chan os.Signal
	quit     chan struct{}
	indoneq  chan struct{}
	keys     map[Key][]byte
	cx       int
	cy       int
	mouse    []byte
	cells    []Cell
	clear    bool
	cursorx  int
	cursory  int
	tiosp    *termiosPrivate
	baud	 int
	wasbtn	 bool

	sync.Mutex
}

func (t *tScreen) Init() error {
	t.evch = make(chan Event, 2)
	t.indoneq = make(chan struct{})
	if e := t.termioInit(); e != nil {
		return e
	}

	ti := t.ti

	t.TPuts(ti.EnterCA)
	t.TPuts(ti.EnterKeypad)
	t.TPuts(ti.HideCursor)
	t.TPuts(ti.Clear)

	t.quit = make(chan struct{})
	t.cx = -1
	t.cy = -1
	t.style = StyleDefault

	t.cells = ResizeCells(nil, 0, 0, t.w, t.h)
	t.cursorx = -1
	t.cursory = -1
	go t.inputLoop()

	return nil
}

func (t *tScreen) prepareKey(key Key, val string) {
	if val != "" {
		t.keys[key] = []byte(val)
	}
}

func (t *tScreen) prepareKeys() {
	ti := t.ti
	t.prepareKey(KeyBackspace, ti.KeyBackspace)
	t.prepareKey(KeyF1, ti.KeyF1)
	t.prepareKey(KeyF2, ti.KeyF2)
	t.prepareKey(KeyF3, ti.KeyF3)
	t.prepareKey(KeyF4, ti.KeyF4)
	t.prepareKey(KeyF5, ti.KeyF5)
	t.prepareKey(KeyF6, ti.KeyF6)
	t.prepareKey(KeyF7, ti.KeyF7)
	t.prepareKey(KeyF8, ti.KeyF8)
	t.prepareKey(KeyF9, ti.KeyF9)
	t.prepareKey(KeyF10, ti.KeyF10)
	t.prepareKey(KeyF11, ti.KeyF11)
	t.prepareKey(KeyF12, ti.KeyF12)
	t.prepareKey(KeyF13, ti.KeyF13)
	t.prepareKey(KeyF14, ti.KeyF14)
	t.prepareKey(KeyF15, ti.KeyF15)
	t.prepareKey(KeyF16, ti.KeyF16)
	t.prepareKey(KeyF17, ti.KeyF17)
	t.prepareKey(KeyF18, ti.KeyF18)
	t.prepareKey(KeyF19, ti.KeyF19)
	t.prepareKey(KeyF20, ti.KeyF20)
	t.prepareKey(KeyInsert, ti.KeyInsert)
	t.prepareKey(KeyDelete, ti.KeyDelete)
	t.prepareKey(KeyHome, ti.KeyHome)
	t.prepareKey(KeyEnd, ti.KeyEnd)
	t.prepareKey(KeyUp, ti.KeyUp)
	t.prepareKey(KeyDown, ti.KeyDown)
	t.prepareKey(KeyLeft, ti.KeyLeft)
	t.prepareKey(KeyRight, ti.KeyRight)
	t.prepareKey(KeyPgUp, ti.KeyPgUp)
	t.prepareKey(KeyPgDn, ti.KeyPgDn)
	t.prepareKey(KeyHelp, ti.KeyHelp)
}

func (t *tScreen) Fini() {
	ti := t.ti
	if t.quit != nil {
		close(t.quit)
	}
	t.TPuts(ti.ShowCursor)
	t.TPuts(ti.AttrOff)
	t.TPuts(ti.Clear)
	t.TPuts(ti.ExitCA)
	t.TPuts(ti.ExitKeypad)
	t.TPuts(ti.ExitMouse)

	t.w = 0
	t.h = 0
	t.cells = nil
	t.curstyle = Style(-1)
	t.clear = false
	t.termioFini()
}

func (t *tScreen) SetStyle(style Style) {
	t.Lock()
	t.style = style
	t.Unlock()
}

func (t *tScreen) Clear() {

	t.Lock()
	ClearCells(t.cells, t.style)
	t.Unlock()
}

func (t *tScreen) SetCell(x, y int, style Style, ch ...rune) {

	t.Lock()
	if x < 0 || y < 0 || x >= t.w || y >= t.h {
		t.Unlock()
		return
	}
	cell := &t.cells[(y*t.w)+x]
	cell.SetCell(ch, style)
	t.Unlock()
}

func (t *tScreen) PutCell(x, y int, cell *Cell) {
	t.Lock()
	if x < 0 || y < 0 || x >= t.w || y >= t.h {
		t.Unlock()
		return
	}
	cp := &t.cells[(y*t.w)+x]
	cp.PutStyle(cell.Style)
	cp.PutChars(cell.Ch)
	t.Unlock()
}

func (t *tScreen) GetCell(x, y int) *Cell {
	t.Lock()
	if x < 0 || y < 0 || x >= t.w || y >= t.h {
		t.Unlock()
		return nil
	}
	cell := t.cells[(y*t.w)+x]
	t.Unlock()
	return &cell
}

func (t *tScreen) drawCell(x, y int, cell *Cell) {
	// XXX: this would be a place to check for hazeltine not being able
	// to display ~, or possibly non-UTF-8 locales, etc.

	ti := t.ti

	if t.cy != y || t.cx != x {
		t.TPuts(ti.TGoto(x, y))
	}
	style := cell.Style
	if style == StyleDefault {
		style = t.style
	}
	if style != t.curstyle {
		fg, bg, attrs := style.Decompose()

		t.TPuts(ti.AttrOff)
		if attrs&AttrBold != 0 {
			t.TPuts(ti.Bold)
		}
		if attrs&AttrUnderline != 0 {
			t.TPuts(ti.Underline)
		}
		if attrs&AttrReverse != 0 {
			t.TPuts(ti.Reverse)
		}
		if attrs&AttrBlink != 0 {
			t.TPuts(ti.Blink)
		}
		if attrs&AttrDim != 0 {
			t.TPuts(ti.Dim)
		}
		if fg != ColorDefault {
			c := int(fg) - 1
			t.TPuts(ti.TParm(ti.SetFg, c))
		}
		if bg != ColorDefault {
			c := int(bg) - 1
			t.TPuts(ti.TParm(ti.SetBg, c))
		}
		t.curstyle = style
	}
	// now emit runes - taking care to not overrun width with a
	// wide character, and to ensure that we emit exactly one regular
	// character followed up by any residual combing characters

	width := int(cell.Width)
	var str string
	if len(cell.Ch) == 0 {
		str = " "
	} else {
		str = string(cell.Ch)
	}
	if width == 2 && x >= t.w-1 {
		// too wide to fit; emit space instead
		width = 1
		str = " "
	}
	io.WriteString(t.out, str)
	t.cy = y
	t.cx = x + width
}

func (t *tScreen) ShowCursor(x, y int) {
	t.Lock()
	t.cursorx = x
	t.cursory = y
	t.Unlock()
}

func (t *tScreen) HideCursor() {
	t.ShowCursor(-1, -1)
}

func (t *tScreen) showCursor() {

	x, y := t.cursorx, t.cursory
	if x < 0 || y < 0 || x >= t.w || y >= t.h {
		t.TPuts(t.ti.HideCursor)
		return
	}
	if t.cx != x || t.cy != y {
		t.TPuts(t.ti.TGoto(x, y))
	}
	t.TPuts(t.ti.ShowCursor)
	t.cx = x
	t.cy = y
}

func (t *tScreen) TPuts(s string) {
	t.ti.TPuts(t.out, s, t.baud)
}

func (t *tScreen) Show() {
	t.Lock()
	t.resize()
	t.draw()
	t.Unlock()
}

func (t *tScreen) clearScreen() {
	t.TPuts(t.ti.Clear)
	t.clear = false
}

func (t *tScreen) hideCursor() {
	// does not update cursor position
	t.TPuts(t.ti.HideCursor)
}

func (t *tScreen) draw() {
	// clobber cursor position, because we're gonna change it all
	t.cx = -1
	t.cy = -1

	// hide the cursor while we move stuff around
	t.hideCursor()

	if t.clear {
		t.clearScreen()
	}

	for row := 0; row < t.h; row++ {
		for col := 0; col < t.w; col++ {
			cell := &t.cells[(row*t.w)+col]
			if !cell.Dirty {
				continue
			}
			t.drawCell(col, row, cell)
			cell.Dirty = false
		}
	}

	// restore the cursor
	t.showCursor()
}

func (t *tScreen) EnableMouse() {
	if len(t.mouse) != 0 {
		t.TPuts(t.ti.EnterMouse)
	}
}

func (t *tScreen) DisableMouse() {
	if len(t.mouse) != 0 {
		t.TPuts(t.ti.ExitMouse)
	}
}

func (t *tScreen) Size() (int, int) {
	t.Lock()
	w, h := t.w, t.h
	t.Unlock()
	return w, h
}

func (t *tScreen) resize() {
	var ev Event
	if w, h, e := t.getWinSize(); e == nil {
		if w != t.w || h != t.h {
			ev = NewEventResize(w, h)
			t.cx = -1
			t.cy = -1

			t.cells = ResizeCells(t.cells, t.w, t.h, w, h)
			t.w = w
			t.h = h
		}
	}
	if ev != nil {
		t.PostEvent(ev)
	}
}

func (t *tScreen) Colors() int {
	// this doesn't change, no need for lock
	return t.ti.Colors
}

func (t *tScreen) PollEvent() Event {
	select {
	case <-t.quit:
		return nil
	case ev := <-t.evch:
		return ev
	}
}

func (t *tScreen) PostEvent(ev Event) {
	t.evch <- ev
}

func (t *tScreen) scanInput(buf *bytes.Buffer, expire bool) {

	for {
		b := buf.Bytes()
		if len(b) == 0 {
			buf.Reset()
			return
		}
		if b[0] >= ' ' && b[0] <= 0x7F {
			// printable ASCII easy to deal with -- no encodings
			buf.ReadByte()
			ev := NewEventKey(KeyRune, rune(b[0]), ModNone)
			t.PostEvent(ev)
			continue
		}
		// We assume that the first character of any terminal escape
		// sequence will be in ASCII -- most often (by far) it is ESC.
		if b[0] >= 0x80 && utf8.FullRune(b) {
			r, _, e := buf.ReadRune()
			if e == nil {
				ev := NewEventKey(KeyRune, r, ModNone)
				t.PostEvent(ev)
				continue
			}
		}
		// Now check the codes we know about
		partials := 0
		matched := false
		for k, esc := range t.keys {
			if bytes.HasPrefix(b, esc) {
				// matched
				var r rune
				if len(esc) == 1 {
					r = rune(b[0])
				}
				ev := NewEventKey(k, r, ModNone)
				t.PostEvent(ev)
				matched = true
				for i := 0; i < len(esc); i++ {
					buf.ReadByte()
				}
				break
			}
			if bytes.HasPrefix(esc, b) {
				partials++
			}
		}

		// Mouse events are special, as they carry parameters
		if !matched && len(t.mouse) != 0 &&
			bytes.HasPrefix(b, t.mouse) {

			if len(b) >= len(t.mouse)+3 {
				// mouse record
				b = b[len(t.mouse):]
				btns := ButtonNone
				mod := ModNone
				switch b[0] & 3 {
				case 0:
					// Sometimes mouse button presses get
					// conflated with wheel events.  So
					// only count as a wheel event if it
					// occurs in isolation.
					if b[0] & 64 != 0 && !t.wasbtn {
						btns = WheelUp
					} else {
						btns = Button1
						t.wasbtn = true
					}
				case 1:
					if b[0] & 64 != 0 && !t.wasbtn {
						btns = WheelDown
					} else {
						btns = Button2
						t.wasbtn = true
					}
				case 2:
					btns = Button3
					t.wasbtn = true
				case 3:
					btns = 0
					t.wasbtn = false
				}
				if b[0]&4 != 0 {
					mod |= ModShift
				}
				if b[0]&8 != 0 {
					mod |= ModMeta
				}
				if b[0]&16 != 0 {
					mod |= ModCtrl
				}
				x := int(b[1]) - 33
				y := int(b[2]) - 33
				for i := 0; i < len(t.mouse)+3; i++ {
					buf.ReadByte()
				}
				matched = true
				// We've seen cases where the x or y coordinates
				// are off screen, normally when click dragging.
				// Clip them to the window.

				if x > t.w-1 {
					x = t.w-1
				}
				if y > t.h-1 {
					y = t.h-1
				}
				if x < 0 {
					x = 0
				}
				if y < 0 {
					y = 0
				}
				ev := NewEventMouse(x, y, btns, mod)
				t.PostEvent(ev)
				continue
			} else {
				partials++
			}

		} else {
			partials++
		}

		// if we expired, we implicitly fail matches
		if expire {
			partials = 0
		}
		// If we had no partial matches, just send first character as
		// a rune.  Others might still work.
		if partials == 0 && !matched {
			ev := NewEventKey(KeyRune, rune(b[0]), ModNone)
			t.PostEvent(ev)
			buf.ReadByte()
		}

		if partials > 0 {
			// We had one or more partial matches, wait for more
			// data.
			return
		}
	}
}

func (t *tScreen) inputLoop() {
	buf := &bytes.Buffer{}
	chunk := make([]byte, 128)
	for {
		select {
		case <-t.quit:
			close(t.indoneq)
			return
		case <-t.sigwinch:
			t.Lock()
			t.resize()
			t.Unlock()
			continue
		default:
		}
		n, e := t.in.Read(chunk)
		switch e {
		case io.EOF:
			// If we timeout waiting for more bytes, then it's
			// time to give up on it.  Even at 300 baud it takes
			// less than 0.5 ms to transmit a whole byte.
			if buf.Len() > 0 {
				t.scanInput(buf, true)
			}
			continue
		case nil:
		default:
			close(t.indoneq)
			return
		}
		buf.Write(chunk[:n])
		// Now we need to parse the input buffer for events
		t.scanInput(buf, false)
	}
}

func (t *tScreen) Sync() {
	t.Lock()
	t.resize()
	t.clear = true
	InvalidateCells(t.cells)
	t.draw()
	t.Unlock()
}
