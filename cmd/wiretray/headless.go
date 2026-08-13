package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/AntonyI1/wiretray/config"
	"github.com/AntonyI1/wiretray/engine"
	"github.com/AntonyI1/wiretray/proxy"
)

const handshakeTimeout = 15 * time.Second

func runHeadless(log *slog.Logger, confPath string) error {
	if confPath == "" {
		p, err := defaultConf()
		if err != nil {
			return err
		}
		confPath = p
	}

	cfg, err := config.Parse(confPath)
	if err != nil {
		return err
	}

	tn, err := engine.Start(cfg, log)
	if err != nil {
		return err
	}
	defer tn.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), handshakeTimeout)
	err = tn.AwaitHandshake(ctx)
	cancel()
	if err != nil {
		return err
	}
	log.Info("handshake complete")

	// The listener starts only now, after the tunnel is live, and dies
	// with it: a stopped tunnel means a refused port, never a fallback
	// onto normal routing.
	srv := proxy.New(proxy.NetstackBackend(tn.Net()), log)
	if err := srv.Listen(cfg.Bind); err != nil {
		return err
	}

	watchCtx, stopWatch := context.WithCancel(context.Background())
	defer stopWatch()
	go tn.Watch(watchCtx)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		log.Info("shutting down")
		srv.Close()
	}()

	return srv.Serve()
}

// defaultConf finds the config to run when -config is not given: the
// single .conf under the user config dir, with a helpful error otherwise.
func defaultConf() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	confDir := filepath.Join(dir, "wiretray", "configs")
	matches, err := filepath.Glob(filepath.Join(confDir, "*.conf"))
	if err != nil {
		return "", err
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no configs found, put one in %s", confDir)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("multiple configs in %s, pick one with -config", confDir)
	}
}
