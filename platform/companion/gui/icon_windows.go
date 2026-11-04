//go:build windows

package gui

import (
	_ "embed"
	"encoding/binary"
	"unsafe"

	"golang.org/x/sys/windows"
)

//go:embed ui/icon.ico
var iconICO []byte

// setWindowIcon loads the embedded ICO at taskbar and window sizes.
func setWindowIcon(hwnd unsafe.Pointer) {
	if hwnd == nil || len(iconICO) < 6 {
		return
	}
	const (
		wmSetIcon = 0x0080
		iconSmall = 0
		iconBig   = 1
	)
	user32 := windows.NewLazySystemDLL("user32.dll")
	create := user32.NewProc("CreateIconFromResourceEx")
	send := user32.NewProc("SendMessageW")
	if create.Find() != nil || send.Find() != nil {
		return
	}
	for _, want := range []struct {
		px   int
		kind uintptr
	}{{16, iconSmall}, {32, iconBig}} {
		img := imageFromICO(iconICO, want.px)
		if img == nil {
			continue
		}
		h, _, _ := create.Call(
			uintptr(unsafe.Pointer(&img[0])), uintptr(len(img)),
			1,          // fIcon: an icon rather than a cursor
			0x00030000, // the version every .ico on disk is
			uintptr(want.px), uintptr(want.px), 0,
		)
		if h != 0 {
			send.Call(uintptr(hwnd), wmSetIcon, want.kind, h)
		}
	}
}

// imageFromICO returns the image closest to px from an .ico file's directory.
func imageFromICO(ico []byte, px int) []byte {
	if len(ico) < 6 {
		return nil
	}
	count := int(binary.LittleEndian.Uint16(ico[4:6]))
	best, bestGap := []byte(nil), 1<<30
	for i := 0; i < count; i++ {
		e := 6 + i*16
		if e+16 > len(ico) {
			break
		}
		w := int(ico[e])
		if w == 0 {
			w = 256
		}
		size := int(binary.LittleEndian.Uint32(ico[e+8 : e+12]))
		off := int(binary.LittleEndian.Uint32(ico[e+12 : e+16]))
		if off < 0 || size <= 0 || off+size > len(ico) {
			continue
		}
		gap := w - px
		if gap < 0 {
			gap = -gap
		}
		if gap < bestGap {
			best, bestGap = ico[off:off+size], gap
		}
	}
	return best
}
