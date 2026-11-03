// Command panel runs the VPN control panel.
//
//	panel [-data-dir DIR] [-dev]   serve the panel
//	panel show-url [-data-dir DIR] print the secret panel URL
//	panel version
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"panel/internal/agent"
	"panel/internal/agent/xray"
	"panel/internal/panel/api"
	"panel/internal/panel/auth"
	"panel/internal/panel/config"
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
	xrayAPI := fsFlags.String("xray-api", "127.0.0.1:62789", "local address for the Xray stats API")
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
	if *dev {
		cfg.Listen, cfg.BasePath = "127.0.0.1:8080", "dev"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, filepath.Join(*dataDir, "panel.db"))
	if err != nil {
		log.Error("open database", "err", err)
		return 1
	}
	defer st.Close()

	authSvc := auth.NewService(st)
	var localAgent *agent.Agent
	var syncer *service.Syncer
	changed := func() {}
	if !*noAgent {
		xr := xray.New(*xrayBin, filepath.Join(*dataDir, "xray"), *xrayAPI, log)
		localAgent = agent.New(xr)
		defer localAgent.Stop()
		syncer = service.NewSyncer(st, localAgent, *xrayAPI, log)
		changed = syncer.Trigger
		go syncer.Run(ctx)
	}

	_, portStr, _ := net.SplitHostPort(cfg.Listen)
	panelPort, _ := strconv.Atoi(portStr)
	scheme := "https"
	if *dev {
		scheme = "http"
	}
	subBase := fmt.Sprintf("%s://%s/%s/", scheme, net.JoinHostPort(cfg.PublicHost, portStr), cfg.SubPath)
	if *dev {
		subBase = fmt.Sprintf("http://%s/%s/", cfg.Listen, cfg.SubPath)
	}

	handler := api.New(api.Deps{
		BasePath:   cfg.BasePath,
		Auth:       authSvc,
		Users:      service.NewUsers(st, changed),
		Inbounds:   service.NewInbounds(st, changed, panelPort),
		Syncer:     syncer,
		Agent:      localAgent,
		Web:        web.Files(),
		Logger:     log,
		TLS:        !*dev,
		PublicHost: cfg.PublicHost,
		SubBaseURL: subBase,
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
		tlsCfg := &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cert},
			NextProtos:   []string{"h2", "http/1.1"},
		}
		ln = tls.NewListener(stealth.TLSOnlyListener(raw), tlsCfg)
		if created {
			printURLs(os.Stdout, cfg, tlsutil.Fingerprint(cert))
		} else {
			log.Info("panel started; run `panel show-url` to print its address", "listen", cfg.Listen)
		}
	}

	go pruneSessions(ctx, authSvc, log)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("serve", "err", err)
		return 1
	}
	return 0
}

// defaultXrayBin is where scripts/install-xray.sh puts the core on Linux.
func defaultXrayBin() string {
	if runtime.GOOS == "linux" {
		return "/usr/local/lib/panel/xray/xray"
	}
	return "xray"
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

func printURLs(w *os.File, cfg config.Config, fingerprint string) {
	host, port, _ := net.SplitHostPort(cfg.Listen)
	var hosts []string
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		for _, ip := range tlsutil.LocalIPs() {
			if !ip.IsLoopback() {
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
