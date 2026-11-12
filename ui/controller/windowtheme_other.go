//go:build (!darwin && !windows) || ios

package controller

func setWindowDarkTheme(ptr uintptr, mode int) {}
