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
	stateFallback // port open, traffic going DIRECT, not tunneled
)

// session is one live tunnel plus its watcher.
type session struct {
	tn        *engine.Tunnel
	stopWatch context.CancelFunc
}

type confItem struct {
	item *systray.MenuItem
	path string
}

type app struct {
	log      *slog.Logger
	dir      string // config dir root (state.json lives here)
	confDir  string // dir/configs
	conf     string // selected config path
	fallback bool   // direct fallback allowed when the tunnel is down
	st       appState
	sess     *session
	srv      *proxy.Server // nil when no listener; up while connected or in fallback

	mStatus   *systray.MenuItem
	mToggle   *systray.MenuItem
	mFallback *systray.MenuItem
	mAuto     *systray.MenuItem
	confs     []confItem

	tap func() // left-click handler, installed only while the tunnel is down

	// Icon and tooltip changes go through Shell_NotifyIcon(NIM_MODIFY),
	// and the taskbar dismisses an open menu or overflow flyout when one
	// lands. Everything below exists to make those calls rare: only on
	// real transitions, never on a timer, never redundantly.
	lastIconState appState // -1 until the first icon is set
	lastTooltip   string
	lastToggle    string
	tapPolicy     int8 // -1 unknown, 0 menu (nil handler), 1 connect handler
}

// Run blocks for the lifetime of the tray app.
func Run(o Options) {
	base, err := os.UserConfigDir()
	if err != nil {
		o.Log.Error("no user config dir: " + err.Error())
		base = "."
	}
	a := &app{
		log:           o.Log,
		dir:           filepath.Join(base, "wiretray"),
		confDir:       filepath.Join(base, "wiretray", "configs"),
		lastIconState: -1,
		tapPolicy:     -1,
	}
	if err := os.MkdirAll(a.confDir, 0o700); err != nil {
		o.Log.Error("create config dir: " + err.Error())
	}

	saved := loadState(a.dir)
	a.fallback = saved.Fallback
	a.conf = o.ConfPath
	if a.conf == "" {
		a.conf = saved.Config
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
	a.mFallback = systray.AddMenuItemCheckbox(
		"Allow direct fallback (dots turn blue while routing direct)",
		"When the tunnel is down, keep the port open and send traffic over the normal network",
		a.fallback)
	a.mAuto = systray.AddMenuItemCheckbox("Start at login", "", autostartEnabled())
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "")

	if len(paths) == 0 {
		a.mToggle.Disable()
	}

	// Left-click connects when the tunnel is down; while it is up the
	// handler is removed (see applyTapPolicy) so a left-click opens the
	// menu exactly like a right-click, and a stray click can never
	// disconnect you. The handler runs on the systray thread, so it
	// only pokes the event loop. It MUST exist before the first
	// setState below: applyTapPolicy dedupes by policy, so a handler
	// installed as nil would never be repaired.
	clickCh := make(chan struct{}, 1)
	a.tap = func() {
		select {
		case clickCh <- struct{}{}:
		default:
		}
	}

	// Initial posture: fallback opens the port immediately if allowed.
	if a.fallback {
		a.enterFallback()
	} else if len(paths) == 0 {
		a.setState(stateDisconnected, "No configs found, open the config folder")
	} else {
		a.setState(stateDisconnected, "Disconnected")
	}

	go a.loop(clickCh, pickCh, mFolder, mQuit)
}

// applyTapPolicy decides what a left-click on the icon means right now:
// connect while the tunnel is down, open the menu while it is up. A nil
// handler makes the library fall through to its menu behavior. The
// handler variable is read by the systray thread, so it is reassigned
// only when the policy actually flips.
func (a *app) applyTapPolicy() {
	want := int8(1)
	switch a.st {
	case stateConnected, stateConnecting:
		want = 0
	}
	if want == a.tapPolicy {
		return
	}
	a.tapPolicy = want
	if want == 0 {
		systray.SetOnTapped(nil)
		return
	}
	systray.SetOnTapped(a.tap)
}

// loop is the single goroutine that owns all state. Every menu click,
// connect result, and ticker beat arrives here as a channel message, so
// there is nothing to lock.
func (a *app) loop(clickCh chan struct{}, pickCh chan string, mFolder, mQuit *systray.MenuItem) {
	results := make(chan connectResult)
	tick := time.NewTicker(refreshEvery)
	defer tick.Stop()

	for {
		select {
		case <-clickCh:
			switch a.st {
			case stateDisconnected, stateError, stateFallback:
				a.beginConnect(results)
			}
			// connected or connecting: a left-click does nothing

		case <-a.mToggle.ClickedCh:
			closeActiveMenu()
			switch a.st {
			case stateConnected:
				a.disconnect()
			case stateDisconnected, stateError, stateFallback:
				a.beginConnect(results)
			}
			// clicks while connecting are ignored

		case r := <-results:
			a.finishConnect(r)

		case p := <-pickCh:
			closeActiveMenu()
			a.selectConf(p)

		case <-mFolder.ClickedCh:
			closeActiveMenu()
			openFolder(a.confDir)

		case <-a.mFallback.ClickedCh:
			closeActiveMenu()
			a.toggleFallback()

		case <-a.mAuto.ClickedCh:
			closeActiveMenu()
			a.toggleAutostart()

		case <-tick.C:
			a.refreshStatus()

		case <-mQuit.ClickedCh:
			a.stopTunnel()
			a.closeListener()
			systray.Quit()
			return
		}
	}
}

type connectResult struct {
	cfg  *config.Config
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
		cfg, sess, err := connect(conf, log)
		results <- connectResult{cfg: cfg, sess: sess, err: err}
	}()
}

// connect runs off the event loop: it can take up to handshakeTimeout.
// It brings up the tunnel only; the listener is the loop's business.
func connect(confPath string, log *slog.Logger) (*config.Config, *session, error) {
	cfg, err := config.Parse(confPath)
	if err != nil {
		return nil, nil, err
	}

	tn, err := engine.Start(cfg, log)
	if err != nil {
		return nil, nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), handshakeTimeout)
	err = tn.AwaitHandshake(ctx)
	cancel()
	if err != nil {
		tn.Stop()
		return nil, nil, err
	}

	watchCtx, stopWatch := context.WithCancel(context.Background())
	go tn.Watch(watchCtx)

	return cfg, &session{tn: tn, stopWatch: stopWatch}, nil
}

func (a *app) finishConnect(r connectResult) {
	a.mToggle.Enable()
	if r.err != nil {
		if a.fallback {
			a.enterFallback()
			a.setState(stateError, r.err.Error()+" (direct fallback active)")
		} else {
			a.setState(stateError, r.err.Error())
		}
		return
	}

	if err := a.ensureListener(r.cfg.Bind, proxy.NetstackBackend(r.sess.tn.Net())); err != nil {
		r.sess.stopWatch()
		r.sess.tn.Stop()
		a.setState(stateError, err.Error())
		return
	}
	a.sess = r.sess
	a.setState(stateConnected, "Connected via "+filepath.Base(a.conf))
}

func (a *app) disconnect() {
	a.stopTunnel()
	if a.fallback {
		a.enterFallback()
		return
	}
	a.closeListener()
	a.setState(stateDisconnected, "Disconnected")
}

func (a *app) stopTunnel() {
	if a.sess == nil {
		return
	}
	a.sess.stopWatch()
	a.sess.tn.Stop()
	a.sess = nil
}

// enterFallback opens (or repoints) the listener onto the direct
// backend and shows the unmistakable blue state.
func (a *app) enterFallback() {
	if err := a.ensureListener(a.bindAddr(), proxy.DirectBackend()); err != nil {
		a.setState(stateError, err.Error())
		return
	}
	a.setState(stateFallback, "Direct fallback: NOT tunneled")
}

func (a *app) toggleFallback() {
	a.fallback = !a.fallback
	saveState(a.dir, persisted{Config: a.conf, Fallback: a.fallback})
	if a.fallback {
		a.mFallback.Check()
	} else {
		a.mFallback.Uncheck()
	}

	// A live tunnel is unaffected; the mode matters when it is down.
	switch a.st {
	case stateConnected, stateConnecting:
		return
	}
	if a.fallback {
		a.enterFallback()
	} else {
		a.closeListener()
		a.setState(stateDisconnected, "Disconnected")
	}
}

// ensureListener guarantees a listener on bind with the given backend,
// reusing the existing one when the address matches.
func (a *app) ensureListener(bind string, b proxy.Backend) error {
	if a.srv != nil {
		if a.srv.Addr() != nil && a.srv.Addr().String() == bind {
			a.srv.SetBackend(b)
			return nil
		}
		a.closeListener()
	}

	srv := proxy.New(b, a.log)
	if err := srv.Listen(bind); err != nil {
		return err
	}
	go func() {
		if err := srv.Serve(); err != nil {
			a.log.Error("proxy: " + err.Error())
		}
	}()
	a.srv = srv
	return nil
}

func (a *app) closeListener() {
	if a.srv == nil {
		return
	}
	a.srv.Close()
	a.srv = nil
}

// bindAddr is where the listener should sit: the selected config's
// choice, or the default when none is usable.
func (a *app) bindAddr() string {
	if a.conf != "" {
		if cfg, err := config.Parse(a.conf); err == nil {
			return cfg.Bind
		}
	}
	return config.DefaultBind
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
	saveState(a.dir, persisted{Config: a.conf, Fallback: a.fallback})
	if a.st == stateFallback {
		// the new config may want a different bind address
		a.enterFallback()
		return
	}
	a.setState(a.st, "Selected "+filepath.Base(path))
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
	// Menu items update through SetMenuItemInfo, which cannot dismiss
	// anything. The tooltip is deliberately NOT refreshed here: a
	// NIM_MODIFY firing on this timer was closing open menus at random.
	a.mStatus.SetTitle(note)
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
	a.applyTapPolicy()

	if st != a.lastIconState {
		icons := map[appState][]byte{
			stateDisconnected: iconDisconnected,
			stateConnecting:   iconConnecting,
			stateConnected:    iconConnected,
			stateError:        iconError,
			stateFallback:     iconFallback,
		}
		systray.SetIcon(icons[st])
		a.lastIconState = st
	}

	a.mStatus.SetTitle(note)

	if tip := "WireTray: " + note; tip != a.lastTooltip {
		systray.SetTooltip(tip)
		a.lastTooltip = tip
	}

	toggle := "Connect"
	switch st {
	case stateConnected:
		toggle = "Disconnect"
	case stateConnecting:
		toggle = "Connecting..."
	}
	if toggle != a.lastToggle {
		a.mToggle.SetTitle(toggle)
		a.lastToggle = toggle
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
