package tray

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"fyne.io/systray"

	"github.com/AntonyI1/wiretray/config"
	"github.com/AntonyI1/wiretray/engine"
	"github.com/AntonyI1/wiretray/proxy"
)

const (
	handshakeTimeout = 15 * time.Second
	refreshEvery     = 5 * time.Second
	staleNoteAfter   = 3 * time.Minute
)

// Options configures the tray app.
type Options struct {
	ConfPath string // optional explicit config; empty means picker plus saved choice
	Log      *slog.Logger
}

type appState int

const (
	stateDisconnected appState = iota
	stateConnecting
	stateConnected
	stateError
)

// session is one live tunnel plus its proxy and watcher.
type session struct {
	tn        *engine.Tunnel
	srv       *proxy.Server
	stopWatch context.CancelFunc
}

type confItem struct {
	item *systray.MenuItem
	path string
}

type app struct {
	log     *slog.Logger
	dir     string // config dir root (state.json lives here)
	confDir string // dir/configs
	conf    string // selected config path
	st      appState
	sess    *session

	mStatus *systray.MenuItem
	mToggle *systray.MenuItem
	mAuto   *systray.MenuItem
	confs   []confItem
}

// Run blocks for the lifetime of the tray app.
func Run(o Options) {
	base, err := os.UserConfigDir()
	if err != nil {
		o.Log.Error("no user config dir: " + err.Error())
		base = "."
	}
	a := &app{
		log:     o.Log,
		dir:     filepath.Join(base, "wiretray"),
		confDir: filepath.Join(base, "wiretray", "configs"),
	}
	if err := os.MkdirAll(a.confDir, 0o700); err != nil {
		o.Log.Error("create config dir: " + err.Error())
	}

	a.conf = o.ConfPath
	if a.conf == "" {
		a.conf = loadSelected(a.dir)
	}

	systray.Run(a.ready, func() {})
}

func (a *app) ready() {
	systray.SetTitle("WireTray")

	a.mStatus = systray.AddMenuItem("Disconnected", "")
	a.mStatus.Disable()
	a.mToggle = systray.AddMenuItem("Connect", "Start the tunnel")
	systray.AddSeparator()

	pickCh := make(chan string)
	mTunnels := systray.AddMenuItem("Tunnel", "Choose a config")
	paths, err := filepath.Glob(filepath.Join(a.confDir, "*.conf"))
	if err != nil {
		a.log.Error("list configs: " + err.Error())
	}
	if a.conf == "" && len(paths) == 1 {
		a.conf = paths[0]
	}
	for _, p := range paths {
		item := mTunnels.AddSubMenuItemCheckbox(filepath.Base(p), "", p == a.conf)
		a.confs = append(a.confs, confItem{item: item, path: p})
		go func(it *systray.MenuItem, path string) {
			for range it.ClickedCh {
				pickCh <- path
			}
		}(item, p)
	}

	mFolder := systray.AddMenuItem("Open config folder", "")
	a.mAuto = systray.AddMenuItemCheckbox("Start at login", "", autostartEnabled())
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "")

	if len(paths) == 0 {
		a.setState(stateDisconnected, "No configs found, open the config folder")
		a.mToggle.Disable()
	} else {
		a.setState(stateDisconnected, "Disconnected")
	}

	go a.loop(pickCh, mFolder, mQuit)
}

// loop is the single goroutine that owns all state. Every menu click,
// connect result, and ticker beat arrives here as a channel message, so
// there is nothing to lock.
func (a *app) loop(pickCh chan string, mFolder, mQuit *systray.MenuItem) {
	results := make(chan connectResult)
	tick := time.NewTicker(refreshEvery)
	defer tick.Stop()

	for {
		select {
		case <-a.mToggle.ClickedCh:
			switch a.st {
			case stateConnected:
				a.disconnect()
			case stateDisconnected, stateError:
				a.beginConnect(results)
			}
			// clicks while connecting are ignored

		case r := <-results:
			a.finishConnect(r)

		case p := <-pickCh:
			a.selectConf(p)

		case <-mFolder.ClickedCh:
			openFolder(a.confDir)

		case <-a.mAuto.ClickedCh:
			a.toggleAutostart()

		case <-tick.C:
			a.refreshStatus()

		case <-mQuit.ClickedCh:
			a.disconnect()
			systray.Quit()
			return
		}
	}
}

type connectResult struct {
	sess *session
	err  error
}

func (a *app) beginConnect(results chan<- connectResult) {
	if a.conf == "" {
		a.setState(stateError, "No config selected")
		return
	}
	a.setState(stateConnecting, "Connecting to "+filepath.Base(a.conf))
	a.mToggle.Disable()

	conf, log := a.conf, a.log
	go func() {
		sess, err := connect(conf, log)
		results <- connectResult{sess: sess, err: err}
	}()
}

// connect runs off the event loop: it can take up to handshakeTimeout.
func connect(confPath string, log *slog.Logger) (*session, error) {
	cfg, err := config.Parse(confPath)
	if err != nil {
		return nil, err
	}

	tn, err := engine.Start(cfg, log)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), handshakeTimeout)
	err = tn.AwaitHandshake(ctx)
	cancel()
	if err != nil {
		tn.Stop()
		return nil, err
	}

	srv := proxy.New(tn.Net(), log)
	if err := srv.Listen(cfg.Bind); err != nil {
		tn.Stop()
		return nil, err
	}
	go func() {
		if err := srv.Serve(); err != nil {
			log.Error("proxy: " + err.Error())
		}
	}()

	watchCtx, stopWatch := context.WithCancel(context.Background())
	go tn.Watch(watchCtx)

	return &session{tn: tn, srv: srv, stopWatch: stopWatch}, nil
}

func (a *app) finishConnect(r connectResult) {
	a.mToggle.Enable()
	if r.err != nil {
		a.setState(stateError, r.err.Error())
		return
	}
	a.sess = r.sess
	a.setState(stateConnected, "Connected via "+filepath.Base(a.conf))
}

func (a *app) disconnect() {
	if a.sess == nil {
		return
	}
	a.sess.stopWatch()
	a.sess.srv.Close()
	a.sess.tn.Stop()
	a.sess = nil
	a.setState(stateDisconnected, "Disconnected")
}

func (a *app) selectConf(path string) {
	if a.st == stateConnected || a.st == stateConnecting {
		a.disconnect()
	}
	a.conf = path
	for _, c := range a.confs {
		if c.path == path {
			c.item.Check()
		} else {
			c.item.Uncheck()
		}
	}
	saveSelected(a.dir, path)
	a.setState(stateDisconnected, "Selected "+filepath.Base(path))
}

func (a *app) refreshStatus() {
	if a.st != stateConnected || a.sess == nil {
		return
	}
	st, err := a.sess.tn.Status()
	if err != nil {
		return
	}
	age := time.Since(st.LastHandshake).Round(time.Second)
	note := fmt.Sprintf("Connected, handshake %s ago", age)
	if age > staleNoteAfter {
		note = fmt.Sprintf("Connected, handshake stale (%s), recovering", age)
	}
	a.mStatus.SetTitle(note)
	systray.SetTooltip("WireTray: " + note)
}

func (a *app) toggleAutostart() {
	enable := !autostartEnabled()
	if err := setAutostart(enable); err != nil {
		a.log.Error("autostart: " + err.Error())
		return
	}
	if enable {
		a.mAuto.Check()
	} else {
		a.mAuto.Uncheck()
	}
}

func (a *app) setState(st appState, note string) {
	a.st = st
	icons := map[appState][]byte{
		stateDisconnected: iconDisconnected,
		stateConnecting:   iconConnecting,
		stateConnected:    iconConnected,
		stateError:        iconError,
	}
	systray.SetIcon(icons[st])
	a.mStatus.SetTitle(note)
	systray.SetTooltip("WireTray: " + note)
	switch st {
	case stateConnected:
		a.mToggle.SetTitle("Disconnect")
	case stateConnecting:
		a.mToggle.SetTitle("Connecting...")
	default:
		a.mToggle.SetTitle("Connect")
	}
}

func openFolder(dir string) {
	name := "xdg-open"
	if runtime.GOOS == "windows" {
		name = "explorer"
	}
	if err := exec.Command(name, dir).Start(); err != nil {
		// nothing sensible to do beyond noting it
		_ = err
	}
}
