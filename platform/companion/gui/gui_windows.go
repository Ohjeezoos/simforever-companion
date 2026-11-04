//go:build windows

// Windows interface using Edge WebView2 through the existing Go binding.
package gui

import (
	_ "embed"
	"encoding/json"
	"sync"
	"unsafe"

	"github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

// darkTitleBar enables dark window chrome on supported Windows versions.
func darkTitleBar(hwnd unsafe.Pointer) {
	if hwnd == nil {
		return
	}
	const useImmersiveDarkMode = 20
	on := int32(1)
	dwm := windows.NewLazySystemDLL("dwmapi.dll")
	set := dwm.NewProc("DwmSetWindowAttribute")
	if set.Find() != nil {
		return
	}
	set.Call(uintptr(hwnd), useImmersiveDarkMode,
		uintptr(unsafe.Pointer(&on)), unsafe.Sizeof(on))
}

//go:embed ui/app.html
var page string

// Window is the companion's window. Every method may be called from any
// goroutine; each one hands the work to the thread that owns the window.
type Window struct {
	w    webview2.WebView
	once sync.Once

	// OnAgree is called when the player accepts the notice.
	OnAgree func()
	// OnClose is called once, when the window is going away.
	OnClose func()
}

// New opens the window. It must be called from the goroutine holding the main
// thread, and Run does not return until the window closes.
func New(title, version string) (*Window, error) {
	w := &Window{}
	view := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug: false,
		WindowOptions: webview2.WindowOptions{
			Title:  title,
			Width:  700,
			Height: 560,
			IconId: 2,
		},
	})
	if view == nil {
		return nil, errNoWebView
	}
	w.w = view
	darkTitleBar(view.Window())
	setWindowIcon(view.Window())
	view.Bind("agree", func() {
		if w.OnAgree != nil {
			w.OnAgree()
		}
	})
	view.Bind("quit", func() { w.Close() })
	view.SetHtml(page)
	w.call("setVersion", version)
	return w, nil
}

// Run shows the window and blocks until it closes.
func (w *Window) Run() {
	w.w.Run()
	if w.OnClose != nil {
		w.once.Do(w.OnClose)
	}
}

// Close asks the window to shut, from anywhere.
func (w *Window) Close() {
	if w == nil || w.w == nil {
		return
	}
	if w.OnClose != nil {
		w.once.Do(w.OnClose)
	}
	w.w.Dispatch(func() { w.w.Terminate() })
}

// Append adds a line to the activity list.
func (w *Window) Append(line string) { w.call("line", line) }

// Quiet adds a line that is worth reading but is not an event.
func (w *Window) Quiet(line string) { w.call2("line", line, "quiet") }

// Notice replaces the activity list with something meant to be read.
func (w *Window) Notice(text string) { w.call("notice", text) }

// State updates the headline, detail text and status ring.
func (w *Window) State(headline, sub, kind string) { w.call3("state", headline, sub, kind) }

// Note is the small line in the corner, for something standing rather than
// happening.
func (w *Window) Note(text string) { w.call("note", text) }

// ShowAgree shows or hides the button that accepts the notice.
func (w *Window) ShowAgree(on bool) { w.call("showAgree", on) }

func (w *Window) call(fn string, arg any) {
	if w == nil || w.w == nil {
		return
	}
	a, err := json.Marshal(arg)
	if err != nil {
		return
	}
	w.w.Dispatch(func() { w.w.Eval(fn + "(" + string(a) + ")") })
}

func (w *Window) call3(fn string, a1, a2, a3 any) {
	if w == nil || w.w == nil {
		return
	}
	x, e1 := json.Marshal(a1)
	y, e2 := json.Marshal(a2)
	z, e3 := json.Marshal(a3)
	if e1 != nil || e2 != nil || e3 != nil {
		return
	}
	w.w.Dispatch(func() { w.w.Eval(fn + "(" + string(x) + "," + string(y) + "," + string(z) + ")") })
}

func (w *Window) call2(fn string, a1, a2 any) {
	if w == nil || w.w == nil {
		return
	}
	x, err1 := json.Marshal(a1)
	y, err2 := json.Marshal(a2)
	if err1 != nil || err2 != nil {
		return
	}
	w.w.Dispatch(func() { w.w.Eval(fn + "(" + string(x) + "," + string(y) + ")") })
}
