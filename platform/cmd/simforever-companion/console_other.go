//go:build !windows

package main

// Only Windows destroys the console when the process ends, so everywhere else
// there is nothing to hold open.
func holdWindowOpen() {}
