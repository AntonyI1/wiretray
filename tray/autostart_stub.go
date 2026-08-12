//go:build !windows

package tray

import "errors"

func autostartEnabled() bool { return false }

func setAutostart(bool) error { return errors.New("start at login is Windows only") }
