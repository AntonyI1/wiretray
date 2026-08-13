//go:build !windows

package main

import "os"

func captureStderr(*os.File) {}
