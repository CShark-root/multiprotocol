// Command panel runs the VPN control panel.
//
//	panel [-data-dir DIR] [-dev]   serve the panel
//	panel show-url [-data-dir DIR] print the secret panel URL
//	panel version
package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"panel/internal/agent"
	"panel/internal/agent/hysteria"
	"panel/internal/agent/singbox"
	"panel/internal/agent/xray"
	"panel/internal/amnezia"
	"panel/internal/panel/api"
	"panel/internal/panel/auth"
	"panel/internal/panel/backup"
	"panel/internal/panel/config"
	"panel/internal/panel/domain"
	"panel/internal/panel/geo"
	"panel/internal/panel/journal"
	"panel/internal/panel/service"
	"panel/internal/panel/stealth"
	"panel/internal/panel/store"
	"panel/internal/panel/tlsutil"
	"panel/internal/shared/version"
	"panel/web"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version":
			fmt.Println(version.Version)
			return
		case "show-url":
			os.Exit(showURL(os.Args[2:]))
		case "set-host":
			os.Exit(setHost(os.Args[2:]))
		case "admin":
			os.Exit(adminCmd(os.Args[2:]))
		case "backup":
			os.Exit(backupCmd(os.Args[2:]))
		case "restore":
			os.Exit(restoreCmd(os.Args[2:]))
		case "amnezia-helper":
			os.Exit(amneziaHelper(os.Args[2:]))
		}
	}
	os.Exit(serve(os.Args[1:]))
}

func serve(args []string) int {
	fsFlags := flag.NewFlagSet("panel", flag.ExitOnError)
	dataDir := fsFlags.String("data-dir", "data", "directory for the database, config and TLS keys")
	dev := fsFlags.Bool("dev", false, "development mode: plain HTTP on 127.0.0.1:8080 under /dev/")
	noAgent := fsFlags.Bool("no-local-agent", false, "do not run the embedded agent (multi-node panel host)")
	xrayBin := fsFlags.String("xray-bin", defaultXrayBin(), "path to the xray binary (geo .dat files next to it)")
	xrayAPI := fsFlags.String("xray-api", "auto", "local address for the Xray stats API (auto: a free loopback port; 3x-ui holds 62789)")
	hysteriaBin := fsFlags.String("hysteria-bin", defaultCoreBin("hysteria", "hysteria"), "path to the Hysteria 2 binary")
	singboxBin := fsFlags.String("singbox-bin", defaultCoreBin("sing-box", "sing-box"), "path to the sing-box binary")
	fsFlags.Parse(args)

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		log.Error("create data dir", "err", err)
		return 1
	}
	cfg, created, err := config.LoadOrCreate(*dataDir)
	if err != nil {
		log.Error("load config", "err", err)
		return 1
	}
	stored := cfg // as on disk, to tell the settings page what is not applied yet
	if *dev {
		cfg.Listen, cfg.BasePath = "127.0.0.1:8080", "dev"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The settings page restarts the panel to apply a new address: exit with
	// exitRestart and let systemd (Restart=on-failure) start it again.
	var restart func()
	var restarting atomic.Bool
	if !*dev {
		restart = func() {
			log.Warn("restarting to apply new panel settings")
			restarting.Store(true)
			stop()
		}
	}

	st, err := store.Open(ctx, filepath.Join(*dataDir, "panel.db"))
	if err != nil {
		log.Error("open database", "err", err)
		return 1
	}
	defer st.Close()

	// From here on every log record (panel and core output) also goes to the
	// journal shown in the UI.
	hub := journal.NewHub(st)
	hubDone := make(chan struct{})
	go func() { hub.Run(ctx); close(hubDone) }()
	defer func() { stop(); <-hubDone }() // flush the last batch before the store closes
	log = slog.New(hub.Handler(log.Handler()))
	slog.SetDefault(log)
	events := service.NewEvents(st)

	authSvc := auth.NewService(st)
	domains := service.NewDomains(st, events, *dataDir, log)
	var localAgent *agent.Agent
	var syncer *service.Syncer
	changed := func() {}
	geoDir := filepath.Join(*dataDir, "geo")
	if err := geo.Seed(geoDir, filepath.Dir(*xrayBin)); err != nil {
		log.Warn("no geo databases yet; they will be downloaded", "err", err)
	}
	var routingSvc *service.Routing
	if *xrayAPI == "auto" {
		// A fixed port would clash with other Xray installs on the same
		// machine (3x-ui uses 62789); the API is only ever reached locally.
		addr, err := freeLoopback()
		if err != nil {
			log.Error("pick a port for the xray api", "err", err)
			return 1
		}
		*xrayAPI = addr
	}
	if !*noAgent {
		xr := xray.New(*xrayBin, filepath.Join(*dataDir, "xray"), geoDir, *xrayAPI, log)
		hy := hysteria.New(*hysteriaBin, filepath.Join(*dataDir, "hysteria"), log)
		sb := singbox.New(*singboxBin, filepath.Join(*dataDir, "sing-box"), log)
		localAgent = agent.New(xr, hy, sb)
		defer localAgent.Stop()
		syncer = service.NewSyncer(st, localAgent, events, *xrayAPI, log)
		changed = syncer.Trigger
		routingSvc = service.NewRouting(st, events, geoDir, changed, localAgent.RestartCores, log)
		syncer.SetRouting(routingSvc)
		go syncer.Run(ctx)
		go routingSvc.RunGeoUpdates(ctx)
	} else {
		routingSvc = service.NewRouting(st, events, geoDir, nil, nil, log)
	}

	_, portStr, _ := net.SplitHostPort(cfg.Listen)
	panelPort, _ := strconv.Atoi(portStr)
	scheme := "https"
	if *dev {
		scheme = "http"
	}
	subBase := fmt.Sprintf("%s://%s/%s/", scheme, net.JoinHostPort(cfg.PublicHost, portStr), cfg.SubPath)
	lanSub := !*dev && cfg.LANOnly() && cfg.LANSubPort > 0
	switch {
	case *dev:
		subBase = fmt.Sprintf("http://%s/%s/", cfg.Listen, cfg.SubPath)
	case lanSub:
		subBase = fmt.Sprintf("http://%s/%s/", net.JoinHostPort(cfg.PublicHost, strconv.Itoa(cfg.LANSubPort)), cfg.SubPath)
	}

	inboundsSvc := service.NewInbounds(st, changed, panelPort)
	inboundsSvc.UseDomainCerts(domains.CertFor)
	domains.OnCert(func(ctx context.Context, _ *domain.Cert) {
		if err := inboundsSvc.RefreshDomainCerts(ctx); err != nil {
			log.Warn("apply domain certificate to inbounds", "err", err)
		}
	})
	go domains.Run(ctx)

	handler := api.New(api.Deps{
		BasePath:   cfg.BasePath,
		Auth:       authSvc,
		Users:      service.NewUsers(st, changed),
		Inbounds:   inboundsSvc,
		Domains:    domains,
		Amnezia:    amneziaClient(*dev),
		Syncer:     syncer,
		Events:     events,
		Journal:    hub,
		Store:      st,
		Scanner:    service.NewScanner(),
		Routing:    routingSvc,
		Reboot:     rebootFunc(*dev),
		Panel:      api.PanelConfig{DataDir: *dataDir, Current: stored, Restart: restart},
		Agent:      localAgent,
		Web:        web.Files(),
		Logger:     log,
		TLS:        !*dev,
		PublicHost: cfg.PublicHost,
		SubBaseURL: subBase,
		SubOverLAN: lanSub,
	})
	sub := api.NewSubscription(api.SubscriptionDeps{
		SubPath: cfg.SubPath, Store: st, PublicHost: cfg.PublicHost, Decoy: cfg.Decoy,
	})
	srv := &http.Server{
		Handler:           stealth.Gate(cfg.Decoy, map[string]http.Handler{cfg.BasePath: handler, cfg.SubPath: sub}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
		// Scanners cause a steady stream of TLS handshake errors; keep them
		// out of the normal log.
		ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelDebug),
	}

	raw, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		log.Error("listen", "addr", cfg.Listen, "err", err)
		return 1
	}
	var ln net.Listener
	if *dev {
		if host, _, _ := net.SplitHostPort(cfg.Listen); !net.ParseIP(host).IsLoopback() {
			log.Error("dev mode serves plain HTTP and is only allowed on loopback")
			return 1
		}
		ln = raw
		log.Info("panel (dev, plain HTTP)", "url", "http://"+cfg.Listen+"/dev/")
	} else {
		cert, err := loadCert(*dataDir, cfg)
		if err != nil {
			log.Error("tls certificate", "err", err)
			return 1
		}
		// The domain's Let's Encrypt certificate when the client asks for the
		// domain (renewals apply without a restart), the self-signed one for an IP.
		tlsCfg := &tls.Config{
			MinVersion:     tls.VersionTLS12,
			GetCertificate: domains.GetCertificate(cert),
			NextProtos:     []string{"h2", "http/1.1"},
		}
		ln = tls.NewListener(stealth.TLSOnlyListener(raw), tlsCfg)
		if created {
			printURLs(os.Stdout, cfg, tlsutil.Fingerprint(cert))
		} else {
			log.Info("panel started; run `panel show-url` to print its address", "listen", cfg.Listen)
		}
	}

	// Test VMs only: the subscription (never the panel) over plain HTTP, see
	// config.LANSubPort.
	var lanSrv *http.Server
	if lanSub {
		lanSrv = &http.Server{
			Addr:              ":" + strconv.Itoa(cfg.LANSubPort),
			Handler:           stealth.Gate(cfg.Decoy, map[string]http.Handler{cfg.SubPath: sub}),
			ReadHeaderTimeout: 10 * time.Second,
			ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelDebug),
		}
		log.Warn("serving subscriptions over plain HTTP because the public host is a LAN address (test setup)",
			"port", cfg.LANSubPort)
		go func() {
			if err := lanSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("lan subscription listener", "err", err)
			}
		}()
	}

	go pruneSessions(ctx, authSvc, log)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
		if lanSrv != nil {
			lanSrv.Shutdown(shutdownCtx)
		}
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("serve", "err", err)
		return 1
	}
	if restarting.Load() {
		return exitRestart
	}
	return 0
}

// exitRestart is a non-zero exit code so systemd's Restart=on-failure brings
// the panel back with the new settings.
const exitRestart = 75

// defaultXrayBin is where scripts/install-xray.sh puts the core on Linux.
func defaultXrayBin() string {
	if runtime.GOOS == "linux" {
		return "/usr/local/lib/panel/xray/xray"
	}
	return "xray"
}

// defaultCoreBin is where scripts/install-<core>.sh puts a core on Linux.
func defaultCoreBin(dir, name string) string {
	if runtime.GOOS == "linux" {
		return "/usr/local/lib/panel/" + dir + "/" + name
	}
	return name
}

func loadCert(dataDir string, cfg config.Config) (tls.Certificate, error) {
	if cfg.TLSCert != "" {
		return tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
	}
	dir := filepath.Join(dataDir, "tls")
	return tlsutil.EnsureSelfSigned(filepath.Join(dir, "panel.crt"), filepath.Join(dir, "panel.key"))
}

func pruneSessions(ctx context.Context, a *auth.Service, log *slog.Logger) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := a.PruneSessions(ctx); err != nil {
				log.Warn("prune sessions", "err", err)
			}
		}
	}
}

func showURL(args []string) int {
	fsFlags := flag.NewFlagSet("show-url", flag.ExitOnError)
	dataDir := fsFlags.String("data-dir", "data", "panel data directory")
	fsFlags.Parse(args)

	cfg, err := config.Load(*dataDir)
	if errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(os.Stderr, "panel is not initialised yet: start it once to generate its config")
		return 1
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fingerprint := ""
	if cert, err := loadCert(*dataDir, cfg); err == nil {
		fingerprint = tlsutil.Fingerprint(cert)
	}
	printURLs(os.Stdout, cfg, fingerprint)
	return 0
}

// setHost changes the address clients connect to (used in share links), for
// servers whose public IP is not on an interface or that sit behind NAT.
// The running panel picks it up after a restart.
func setHost(args []string) int {
	fsFlags := flag.NewFlagSet("set-host", flag.ExitOnError)
	dataDir := fsFlags.String("data-dir", "data", "panel data directory")
	fsFlags.Parse(args)
	if fsFlags.NArg() != 1 || !config.ValidHost(fsFlags.Arg(0)) {
		fmt.Fprintln(os.Stderr, "usage: panel set-host [-data-dir DIR] <ip-or-domain>")
		return 2
	}
	cfg, _, err := config.LoadOrCreate(*dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cfg.PublicHost = fsFlags.Arg(0)
	if err := config.Save(*dataDir, cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("public host set to", cfg.PublicHost, "- restart the panel to apply")
	return 0
}

func printURLs(w *os.File, cfg config.Config, fingerprint string) {
	host, port, _ := net.SplitHostPort(cfg.Listen)
	var hosts []string
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		hosts = append(hosts, cfg.PublicHost)
		for _, ip := range tlsutil.ReachableIPs() {
			if ip.String() != cfg.PublicHost {
				hosts = append(hosts, ip.String())
			}
		}
	} else {
		hosts = []string{host}
	}
	fmt.Fprintln(w, "Panel address (keep it secret):")
	for _, h := range hosts {
		fmt.Fprintf(w, "  https://%s/%s/\n", net.JoinHostPort(h, port), cfg.BasePath)
	}
	if fingerprint != "" {
		fmt.Fprintln(w, "TLS certificate SHA-256 fingerprint (check it in the browser on first visit):")
		fmt.Fprintln(w, "  "+fingerprint)
	}
}

// rebootFunc reboots the machine through logind; the installer grants the
// panel user exactly this polkit action. Development runs cannot reboot.
func rebootFunc(dev bool) func() error {
	if dev || runtime.GOOS != "linux" {
		return nil
	}
	return func() error {
		out, err := exec.Command("systemctl", "reboot").CombinedOutput()
		if err != nil {
			return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
}

// amneziaHelper runs the read-only Docker helper (as root, from
// panel-amnezia.service): the panel itself never gets Docker access.
func amneziaHelper(args []string) int {
	fsFlags := flag.NewFlagSet("amnezia-helper", flag.ExitOnError)
	socket := fsFlags.String("socket", amnezia.HelperSocket, "unix socket to listen on")
	group := fsFlags.String("group", "panel", "group allowed to connect")
	fsFlags.Parse(args)
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	gid := -1
	if g, err := user.LookupGroup(*group); err == nil {
		gid, _ = strconv.Atoi(g.Gid)
	} else {
		log.Warn("group not found; socket stays root-only", "group", *group)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := amnezia.Serve(ctx, *socket, amnezia.DockerSocket, gid, log); err != nil {
		log.Error("amnezia helper", "err", err)
		return 1
	}
	return 0
}

// amneziaClient talks to the helper on Linux servers; dev runs have none.
func amneziaClient(dev bool) *amnezia.HelperClient {
	if dev || runtime.GOOS != "linux" {
		return nil
	}
	return amnezia.NewClient(amnezia.HelperSocket)
}

// backupCmd writes a backup file; it works while the panel runs.
//
//	sudo -u panel panel backup -data-dir /var/lib/panel -out /tmp/panel.tar.gz
func backupCmd(args []string) int {
	fsFlags := flag.NewFlagSet("backup", flag.ExitOnError)
	dataDir := fsFlags.String("data-dir", "data", "panel data directory")
	out := fsFlags.String("out", "", "file to write (default: a dated name in the current directory)")
	fsFlags.Parse(args)
	if *out == "" {
		*out = backup.Name(time.Now())
	}
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(*dataDir, "panel.db"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "open database:", err)
		return 1
	}
	defer st.Close()
	f, err := os.OpenFile(*out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := backup.Write(ctx, st, *dataDir, f); err != nil {
		f.Close()
		os.Remove(*out)
		fmt.Fprintln(os.Stderr, "backup:", err)
		return 1
	}
	if err := f.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(*out)
	return 0
}

// restoreCmd unpacks a backup; stop the panel first. On a new server run
// `panel set-host` afterwards if its address differs.
//
//	sudo systemctl stop panel
//	sudo -u panel panel restore -data-dir /var/lib/panel backup.tar.gz
//	sudo systemctl start panel
func restoreCmd(args []string) int {
	fsFlags := flag.NewFlagSet("restore", flag.ExitOnError)
	dataDir := fsFlags.String("data-dir", "data", "panel data directory")
	fsFlags.Parse(args)
	if fsFlags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: panel restore -data-dir DIR backup.tar.gz")
		return 2
	}
	f, err := os.Open(fsFlags.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer f.Close()
	got, err := backup.Restore(f, *dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "restore:", err)
		return 1
	}
	fmt.Println("restored:", strings.Join(got, ", "))
	fmt.Println("previous files were kept with the suffix .before-restore")
	return 0
}

// adminCmd creates the first admin or sets a new password. The password is
// read from stdin (one line), never from the command line, so it does not
// end up in the process list or shell history.
//
//	panel admin -data-dir D -check                  exit 0 if an admin exists, 3 if not
//	echo "$PW" | panel admin -data-dir D -username alice
//	echo "$PW" | panel admin -data-dir D -username alice -disable-2fa
func adminCmd(args []string) int {
	fsFlags := flag.NewFlagSet("admin", flag.ExitOnError)
	dataDir := fsFlags.String("data-dir", "data", "panel data directory")
	username := fsFlags.String("username", "", "admin login")
	check := fsFlags.Bool("check", false, "only report whether an admin exists")
	no2fa := fsFlags.Bool("disable-2fa", false, "also turn two-factor authentication off (lost phone)")
	fsFlags.Parse(args)
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(*dataDir, "panel.db"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "open database:", err)
		return 1
	}
	defer st.Close()
	svc := auth.NewService(st)
	names, err := svc.Admins(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *check {
		if len(names) == 0 {
			return 3
		}
		fmt.Println(strings.Join(names, "\n"))
		return 0
	}
	if *username == "" {
		fmt.Fprintln(os.Stderr, "usage: panel admin -data-dir DIR -username NAME  (password on stdin)")
		return 2
	}
	line, err := bufio.NewReader(io.LimitReader(os.Stdin, 4096)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintln(os.Stderr, "read password:", err)
		return 1
	}
	password := strings.TrimRight(line, "\r\n")
	if len(names) == 0 {
		if _, err := svc.Setup(ctx, *username, password); err != nil {
			fmt.Fprintln(os.Stderr, "create admin:", err)
			return 1
		}
		fmt.Println("admin created:", *username)
		return 0
	}
	if err := svc.ResetPassword(ctx, *username, password, *no2fa); err != nil {
		fmt.Fprintln(os.Stderr, "set password:", err)
		return 1
	}
	fmt.Println("password changed for", *username, "- all sessions signed out")
	return 0
}

func freeLoopback() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer l.Close()
	return l.Addr().String(), nil
}
