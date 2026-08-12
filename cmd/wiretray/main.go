package main

import (
	"flag"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	"github.com/AntonyI1/wiretray/tray"
)

func main() {
	confPath := flag.String("config", "", "tunnel config file (default: the sole .conf in the config dir)")
	noTray := flag.Bool("no-tray", false, "run headless without a tray icon")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(logDest(), nil))

	// The tray is the product on Windows; headless serves CI, servers,
	// and any OS where the tray has not been ported.
	if *noTray || runtime.GOOS != "windows" {
		if err := runHeadless(log, *confPath); err != nil {
			log.Error(err.Error())
			os.Exit(1)
		}
		return
	}

	tray.Run(tray.Options{ConfPath: *confPath, Log: log})
}

// logDest writes to stderr and a file under the config dir, so the tray
// build (which has no console) still leaves a trail. The file restarts
// once it passes 5MB; no rotation machinery.
func logDest() io.Writer {
	base, err := os.UserConfigDir()
	if err != nil {
		return os.Stderr
	}
	dir := filepath.Join(base, "wiretray")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return os.Stderr
	}

	path := filepath.Join(dir, "wiretray.log")
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if st, err := os.Stat(path); err == nil && st.Size() > 5<<20 {
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return os.Stderr
	}
	return io.MultiWriter(os.Stderr, f)
}
