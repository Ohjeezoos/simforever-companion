package gui

import "errors"

// errNoWebView triggers the command-line fallback when WebView2 is unavailable.
var errNoWebView = errors.New("this machine has no WebView2 runtime, so the window cannot be drawn")

// ErrNoWindow reports whether a window could not be created for that reason.
func ErrNoWindow(err error) bool { return errors.Is(err, errNoWebView) }
