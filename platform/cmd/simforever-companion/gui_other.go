//go:build !windows

package main

import "github.com/Ohjeezoos/simforever-companion/platform/companion"

// Only Windows has the window. Everywhere else this is a command, which is
// what the people running it there expect.
const guiAvailable = false

func runGUI(string, *companion.Config, []string) error { return nil }
