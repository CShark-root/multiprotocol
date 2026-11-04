package api

import (
	"fmt"
	"net"
	"net/http"
	"slices"
	"strconv"
	"time"

	"panel/internal/panel/config"
	"panel/internal/panel/service"
	"panel/internal/panel/validate"
)

// PanelConfig gives the settings page access to panel.json. Changes apply
// after a restart; Restart is nil when the panel cannot restart itself
// (dev mode, no service manager).
type PanelConfig struct {
	DataDir string
	Current config.Config
	Restart func()
}

type panelSettingsDTO struct {
	Port       int              `json:"port"`
	BasePath   string           `json:"basePath"`
	SubPath    string           `json:"subPath"`
	PublicHost string           `json:"publicHost"`
	Decoy      config.DecoyMode `json:"decoy"`
}

func (s *server) panelSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.Load(s.Panel.DataDir)
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	_, port, _ := net.SplitHostPort(cfg.Listen)
	p, _ := strconv.Atoi(port)
	writeJSON(w, http.StatusOK, map[string]any{
		"settings":         panelSettingsDTO{Port: p, BasePath: cfg.BasePath, SubPath: cfg.SubPath, PublicHost: cfg.PublicHost, Decoy: cfg.Decoy},
		"pending":          cfg != s.Panel.Current, // saved but not applied yet
		"restartSupported": s.Panel.Restart != nil,
	})
}

func (s *server) savePanelSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		panelSettingsDTO
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := s.Auth.ConfirmPassword(adminFrom(r.Context()), in.Password); err != nil {
		s.authError(w, r, err)
		return
	}
	cfg, err := config.Load(s.Panel.DataDir)
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	host, oldPort, _ := net.SplitHostPort(cfg.Listen)
	if err := s.checkPanelPort(r, in.Port, oldPort); err != nil {
		s.handleError(w, r, err)
		return
	}
	next := cfg
	next.Listen = net.JoinHostPort(host, strconv.Itoa(in.Port))
	next.BasePath, next.SubPath, next.PublicHost, next.Decoy = in.BasePath, in.SubPath, in.PublicHost, in.Decoy
	next.LANSubPort = cfg.LANSubPort
	if err := next.Validate(); err != nil {
		s.handleError(w, r, validate.Fail("settings", err.Error()))
		return
	}
	if err := config.Save(s.Panel.DataDir, next); err != nil {
		s.handleError(w, r, err)
		return
	}
	// Fill in fields that depend on the new values (e.g. the LAN
	// subscription port when the host became a private address).
	if saved, err := config.Load(s.Panel.DataDir); err == nil {
		next = saved
	}
	s.audit(r, "panel.settings", map[string]any{"port": in.Port, "publicHost": in.PublicHost, "decoy": in.Decoy,
		"basePathChanged": in.BasePath != cfg.BasePath, "subPathChanged": in.SubPath != cfg.SubPath})
	writeJSON(w, http.StatusOK, map[string]any{
		"pending": next != s.Panel.Current,
		"url":     fmt.Sprintf("https://%s/%s/", net.JoinHostPort(next.PublicHost, strconv.Itoa(in.Port)), next.BasePath),
	})
}

// checkPanelPort refuses a port an inbound or another program already uses.
func (s *server) checkPanelPort(r *http.Request, port int, current string) error {
	if port < 1 || port > 65535 {
		return validate.Fail("port", "1-65535")
	}
	if strconv.Itoa(port) == current {
		return nil
	}
	inbounds, err := s.Inbounds.List(r.Context())
	if err != nil {
		return err
	}
	for _, ib := range inbounds {
		if ib.Port == port && slices.Contains(service.Networks(ib.Protocol), "tcp") {
			return validate.Fail("port", fmt.Sprintf("used by inbound %q", ib.Remark))
		}
	}
	l, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return validate.Fail("port", "the port is busy or unavailable on this server")
	}
	l.Close()
	return nil
}

func (s *server) restartPanel(w http.ResponseWriter, r *http.Request) {
	if s.Panel.Restart == nil {
		writeError(w, http.StatusConflict, "restart_unsupported", "restart the panel service by hand")
		return
	}
	s.audit(r, "panel.restart", nil)
	writeJSON(w, http.StatusAccepted, map[string]bool{"restarting": true})
	go func() {
		time.Sleep(500 * time.Millisecond) // let the response reach the browser
		s.Panel.Restart()
	}()
}
