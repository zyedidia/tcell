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

	"github.com/mattn/go-runewidth"
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

type tCell struct {
	ch	[]rune
	dirty	bool
	width	uint8
	style	Style
}

// tScreen represents a screen backed by a terminfo implementation.
type tScreen struct {
	ti       *Terminfo
	w        int
	h        int
	in       *os.File
	out      *os.File
	curstyle Style
	style	 Style
	evch     chan Event
	sigwinch chan os.Signal
	quit     chan struct{}
	indoneq  chan struct{}
	keys     map[Key][]byte
	cx       int
	cy       int
	mouse    []byte
	cells	 []tCell
	clear	 bool
	cursorx  int
	cursory  int
	tiosp    *termiosPrivate

	sync.Mutex
}

func (t *tScreen) Init() error {
	t.evch = make(chan Event, 2)
	t.indoneq = make(chan struct{})
	if e := t.termioInit(); e != nil {
		return e
	}

	out := t.out
	ti := t.ti

	io.WriteString(out, ti.EnterCA)
	io.WriteString(out, ti.EnterKeypad)
	io.WriteString(out, ti.HideCursor)
	io.WriteString(out, ti.Clear)

	t.quit = make(chan struct{})
	t.cx = -1
	t.cy = -1
	t.style = StyleDefault

	t.cells = make([]tCell, t.w * t.h)
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
	out := t.out
	if t.quit != nil {
		close(t.quit)
	}
	io.WriteString(out, ti.ShowCursor)
	io.WriteString(out, ti.AttrOff)
	io.WriteString(out, ti.Clear)
	io.WriteString(out, ti.ExitCA)
	io.WriteString(out, ti.ExitKeypad)
	io.WriteString(out, ti.ExitMouse)

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
	for i := range t.cells {
		t.cells[i].ch = nil
		t.cells[i].style = t.style
		t.cells[i].dirty = true
	}
	t.Unlock()
}

func (t *tScreen) sortRunes(ch ...rune) ([]rune, uint8) {
	var mainc rune
	var width uint8
	var compc []rune

	width = 1
	mainc = ' '
	for _, r := range ch {
		if r < ' ' {
			// skip over non-printable control characters
			continue
		}
		switch runewidth.RuneWidth(r) {
		case 1:
			mainc = r
			width = 1
		case 2:
			mainc = r
			width = 2
		case 0:
			compc = append(compc, r)
		}
	}

	return append([]rune{mainc}, compc...), width
}	

func (t *tScreen) SetCell(x, y int, style Style, ch ...rune) {

	r, width := t.sortRunes(ch...)

	t.Lock()
	if x < 0 || y < 0 || x >= t.w || y >= t.h {
		t.Unlock()
		return
	}
	cell := &t.cells[(y*t.w)+x]
	match := true
	if len(r) != len(cell.ch) || style != cell.style {
		match = false
	} else {
		for i := range r {
			if r[i] != cell.ch[i] {
				match = false
				break
			}
		}
	}
	if match {
		t.Unlock()
		return
	}

	cell.style = style
	cell.ch = r
	cell.width = width
	cell.dirty = true

	t.Unlock()
}

func (t *tScreen) drawCell(x, y int, cell *tCell) {
	// XXX: this would be a place to check for hazeltine not being able
	// to display ~, or possibly non-UTF-8 locales, etc.

	ti := t.ti

	if t.cy != y || t.cx != x {
		io.WriteString(t.out, ti.TGoto(x, y))
	}
	style := cell.style
	if style == StyleDefault {
		style = t.style
	}
	if style != t.curstyle {
		fg, bg, attrs := style.Decompose()

		io.WriteString(t.out, ti.AttrOff)
		if attrs&AttrBold != 0 {
			io.WriteString(t.out, ti.Bold)
		}
		if attrs&AttrUnderline != 0 {
			io.WriteString(t.out, ti.Underline)
		}
		if attrs&AttrReverse != 0 {
			io.WriteString(t.out, ti.Reverse)
		}
		if attrs&AttrBlink != 0 {
			io.WriteString(t.out, ti.Blink)
		}
		if attrs&AttrDim != 0 {
			io.WriteString(t.out, ti.Dim)
		}
		if fg != ColorDefault {
			c := int(fg) - 1
			io.WriteString(t.out, ti.TParm(ti.SetFg, c))
		}
		if bg != ColorDefault {
			c := int(bg) - 1
			io.WriteString(t.out, ti.TParm(ti.SetBg, c))
		}
		t.curstyle = style
	}
	// now emit runes - taking care to not overrun width with a
	// wide character, and to ensure that we emit exactly one regular
	// character followed up by any residual combing characters

	width := int(cell.width)
	ch := cell.ch
	str := " "
	if width == 2 && x >= t.w-1 {
		// too wide to fit; emit space instead
		width = 1
	} else if len(ch) == 0 {
		width = 1
	} else {
		str = string(ch)
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
		io.WriteString(t.out, t.ti.HideCursor)
		return
	}
	if t.cx != x || t.cy != y {
		io.WriteString(t.out, t.ti.TGoto(x, y))
	}
	io.WriteString(t.out, t.ti.ShowCursor)
	t.cx = x
	t.cy = y
}

func (t *tScreen) Show() {
	t.Lock()
	t.resize()
	t.draw()
	t.Unlock()
}

func (t *tScreen) clearScreen() {
	io.WriteString(t.out, t.ti.Clear)
	t.clear = false
}

func (t *tScreen) hideCursor() {
	// does not update cursor position
	io.WriteString(t.out, t.ti.HideCursor)
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
			if !cell.dirty {
				continue
			}
			t.drawCell(col, row, cell)
			cell.dirty = false
		}
	}

	// restore the cursor
	t.showCursor()
}

func (t *tScreen) EnableMouse() {
	if len(t.mouse) != 0 {
		io.WriteString(t.out, t.ti.EnterMouse)
	}
}

func (t *tScreen) DisableMouse() {
	if len(t.mouse) != 0 {
		io.WriteString(t.out, t.ti.ExitMouse)
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

			newc := make([]tCell, w*h)
			for row := 0; row < h && row < t.h; row++ {
				for col := 0; col < w && col < t.w; col++ {
					newc[(row*w)+col] = t.cells[(row*t.w)+col]
					newc[(row*w)+col].dirty = true
				}
			}
			t.cells = newc
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
					btns = Button1
				case 1:
					btns = Button2
				case 2:
					btns = Button3
				case 3:
					btns = 0
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
	for i := range t.cells {
		t.cells[i].dirty = true
	}
	t.draw()
	t.Unlock()
}
