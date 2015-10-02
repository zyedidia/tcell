//+build ignore

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

// boxes just displays random colored boxes on your terminal screen.
// Press ESC to exit the program.
package main

import (
	"fmt"
	"os"

	"github.com/gdamore/tcell"
)

func emitStr(s tcell.Screen, x, y int, str string) {
	for _, c := range str {
		s.SetCell(x, y, tcell.StyleDefault, c)
		x++
	}
}


func drawBox(s tcell.Screen, x1, y1, x2, y2 int, style tcell.Style, r rune) {
	if y2 < y1 {
		y1, y2 = y2, y1
	}
	if x2 < x1 {
		x1, x2 = x2, x1
	}

	for row := y1; row <= y2; row++ {
		for col := x1; col <= x2; col++ {
			s.SetCell(col, row, style, r)
		}
	}
}

func drawSelect(s tcell.Screen, x1, y1, x2, y2 int, sel bool) {

	if y2 < y1 {
		y1, y2 = y2, y1
	}
	if x2 < x1 {
		x1, x2 = x2, x1
	}
	for row := y1; row < y2; row++ {
		for col := x1; col < x2; col++ {
			if cp := s.GetCell(col, row); cp != nil {
				st := cp.Style

				_, bg, _ := st.Decompose()

				if sel {
					bg += tcell.Color(2)
				} else {
					bg -= tcell.Color(2)
				}
				st = st.Background(bg)
				cp.Style = st
				s.PutCell(col, row, cp)
			}
		}
	}
}

// This program just shows simple mouse and keyboard events.  Press ESC to
// exit.
func main() {
	s, e := tcell.NewScreen()
	if e != nil {
		fmt.Printf("oops: %v", e)
	}
	s.Init()
	s.EnableMouse()
	s.Clear()

	posfmt := "Mouse: %d, %d  "
	btnfmt := "Buttons: %-20s"


	mx, my := -1, -1
	ox, oy := 0, 0
	bx, by := -1, -1
	w, h := s.Size()
	lbutton := tcell.ButtonNone
	lchar := '*'
	bstr := ""

	for {
		emitStr(s, 1, 1, "Press ESC to exit, C to clear.")
		emitStr(s, 1, 2, fmt.Sprintf(posfmt, mx, my))
		emitStr(s, 1, 3, fmt.Sprintf(btnfmt, bstr))
		s.Show()
		bstr = ""
		ev := s.PollEvent()
		st := tcell.StyleDefault.Background(tcell.ColorBrightRed)
		up := tcell.StyleDefault.
			Background(tcell.ColorBrightBlue).
			Foreground(tcell.ColorBrightGreen)
		w, h = s.Size()
		switch ev := ev.(type) {
		case *tcell.EventResize:
			s.Sync()
			s.SetCell(w-1, h-1, st, 'R')
		case *tcell.EventKey:
			s.SetCell(w-2, h-2, st, ev.Rune())
			s.SetCell(w-1, h-1, st, 'K')
			if ev.Key() == tcell.KeyEscape {
				s.Fini()
				os.Exit(0)
			} else if ev.Rune() == 'C' || ev.Rune() == 'c' {
				s.Clear()
			}
		case *tcell.EventMouse:
			x, y := ev.Position()
			button := ev.Buttons()
			for i := uint(0); i < 8; i++ {
				if int(button) & (1 << i) != 0 {
					bstr += fmt.Sprintf(" Button%d", i+1)
				}
			}
			if button & tcell.WheelUp != 0 {
				bstr += " WheelUp"
			}
			if button & tcell.WheelDown != 0 {
				bstr += " WheelDown"
			}
			if button & tcell.WheelLeft != 0 {
				bstr += " WheelLeft"
			}
			if button & tcell.WheelRight != 0 {
				bstr += " WheelRight"
			}
			// Only buttons, not wheel events
			button &= tcell.ButtonMask(0xff)
			ch := '*'
			if ox >= 0 && bx >= 0 && oy >= 0 && by >= 0 {
				drawSelect(s, ox, oy, bx, by, false)
			}
			switch ev.Buttons() {
			case tcell.ButtonNone:
				if lbutton != tcell.ButtonNone {
					bg := tcell.Color((lchar - '0')*2+1)
					drawBox(s, ox, oy, x, y,
						up.Background(bg),
						lchar)
					ox, oy = -1, -1
					bx, by = -1, -1
				}
			case tcell.Button1:
				ch = '1'
			case tcell.Button2:
				ch = '2'
			case tcell.Button3:
				ch = '3'
			case tcell.Button4:
				ch = '4'
			case tcell.Button5:
				ch = '5'
			case tcell.Button6:
				ch = '6'
			case tcell.Button7:
				ch = '7'
			case tcell.Button8:
				ch = '8'
			default:
				ch = '*'

			}
			//s.SetCell(x, y, st, ch)
			if lbutton == tcell.ButtonNone {
				ox, oy = x, y
			}
			if ox >= 0 && oy >= 0 {
				bx, by = x, y
			}
			if ox >= 0 && oy >= 0 {
				drawSelect(s, ox, oy, bx, by, true)
			}
			lbutton = button
			lchar = ch
			s.SetCell(w-1, h-1, st, 'M')
			mx, my = x, y
		default:
			s.SetCell(w-1, h-1, st, 'X')
		}
	}
}
