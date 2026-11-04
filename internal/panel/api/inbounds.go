package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"panel/internal/panel/service"
	"panel/internal/panel/store"
)

// inboundDTO never carries private keys, certificates' keys or obfuscation
// secrets.
type inboundDTO struct {
	ID        int64     `json:"id"`
	Remark    string    `json:"remark"`
	Protocol  string    `json:"protocol"`
	Transport string    `json:"transport"`
	Security  string    `json:"security"`
	Network   string    `json:"network"` // tcp | udp | tcp+udp
	Core      string    `json:"core"`    // xray | hysteria | singbox
	Apps      []string  `json:"apps"`    // client app families: xray, singbox
	TestKit   bool      `json:"testKit"`
	Port      int       `json:"port"`
	Address   string    `json:"address"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`

	// VLESS + Reality
	Target          string `json:"target,omitempty"`
	ServerName      string `json:"serverName"`
	Fingerprint     string `json:"fingerprint,omitempty"`
	PublicKey       string `json:"publicKey,omitempty"`
	ShortID         string `json:"shortId,omitempty"`
	XHTTPPath       string `json:"xhttpPath,omitempty"`
	XHTTPMode       string `json:"xhttpMode,omitempty"`
	GRPCServiceName string `json:"grpcServiceName,omitempty"`
	WSPath          string `json:"wsPath,omitempty"`

	// Hysteria2 (CertPin also for TUIC and AnyTLS)
	Obfs        bool   `json:"obfs"`
	Masquerade  string `json:"masquerade,omitempty"`
	UpMbps      int    `json:"upMbps,omitempty"`
	DownMbps    int    `json:"downMbps,omitempty"`
	HopPorts    string `json:"hopPorts,omitempty"`
	HopInterval int    `json:"hopInterval,omitempty"`
	CertPin     string `json:"certPin,omitempty"`

	Expert service.ExpertInput `json:"expert"`
}

type inboundInputDTO struct {
	Remark          string `json:"remark"`
	Protocol        string `json:"protocol"`
	Transport       string `json:"transport"`
	Port            int    `json:"port"`
	Address         string `json:"address"`
	Target          string `json:"target"`
	ServerName      string `json:"serverName"`
	Fingerprint     string `json:"fingerprint"`
	XHTTPPath       string `json:"xhttpPath"`
	XHTTPMode       string `json:"xhttpMode"`
	GRPCServiceName string `json:"grpcServiceName"`
	WSPath          string `json:"wsPath"`
	Obfs            bool   `json:"obfs"`
	Masquerade      string `json:"masquerade"`
	UpMbps          int    `json:"upMbps"`
	DownMbps        int    `json:"downMbps"`
	HopPorts        string `json:"hopPorts"`
	HopInterval     int    `json:"hopInterval"`
	Enabled         *bool  `json:"enabled"` // omitted = true

	Expert service.ExpertInput `json:"expert"`
}

func toInboundDTO(ib store.Inbound) inboundDTO {
	spec, _ := service.Spec(ib.Protocol)
	d := inboundDTO{
		ID: ib.ID, Remark: ib.Remark, Protocol: ib.Protocol, Transport: ib.Transport, Security: ib.Security,
		Network: strings.Join(spec.Networks, "+"), Core: spec.Core, Apps: service.ClientApps(ib.Protocol), TestKit: ib.TestKit,
		Port: ib.Port, Address: ib.Address, Enabled: ib.Enabled, CreatedAt: ib.CreatedAt,
	}
	if r := ib.Settings.Reality; r != nil {
		d.Target, d.Fingerprint, d.PublicKey = r.Target, r.Fingerprint, r.PublicKey
		if len(r.ServerNames) > 0 {
			d.ServerName = r.ServerNames[0]
		}
		if len(r.ShortIDs) > 0 {
			d.ShortID = r.ShortIDs[0]
		}
	}
	if x := ib.Settings.XHTTP; x != nil {
		d.XHTTPPath, d.XHTTPMode = x.Path, x.Mode
	}
	if g := ib.Settings.GRPC; g != nil {
		d.GRPCServiceName = g.ServiceName
	}
	if h := ib.Settings.Hysteria; h != nil {
		d.ServerName, d.Masquerade, d.UpMbps, d.DownMbps = h.SNI, h.Masquerade, h.UpMbps, h.DownMbps
		d.Obfs, d.CertPin = h.ObfsPassword != "", h.PinSHA256
		d.HopPorts, d.HopInterval = h.HopPorts, h.HopInterval
	}
	if t := ib.Settings.TLS; t != nil {
		d.ServerName, d.CertPin = t.SNI, t.PinSHA256
	}
	if ws := ib.Settings.WS; ws != nil {
		d.WSPath = ws.Path
	}
	if st := ib.Settings.ShadowTLS; st != nil {
		d.Target = st.Handshake
	}
	if r := ib.Settings.Reality; r != nil {
		d.Expert.Reality = &service.RealityExpert{
			Xver: r.Xver, MaxTimeDiff: r.MaxTimeDiff, MinClientVer: r.MinClientVer, MaxClientVer: r.MaxClientVer,
			SpiderX: r.SpiderX, ShortIDs: r.ShortIDs, MLDSA65: r.MLDSA65Seed != "",
		}
	}
	if x := ib.Settings.XHTTP; x != nil {
		cp := *x
		d.Expert.XHTTP = &cp
	}
	d.Expert.Sniffing, d.Expert.Sockopt = ib.Settings.Sniffing, ib.Settings.Sockopt
	return d
}

func (in inboundInputDTO) toInput() service.InboundInput {
	return service.InboundInput{
		Remark: in.Remark, Protocol: in.Protocol, Port: in.Port, Address: in.Address,
		Enabled:   in.Enabled == nil || *in.Enabled,
		Transport: in.Transport, Target: in.Target, ServerName: in.ServerName, Fingerprint: in.Fingerprint,
		XHTTPPath: in.XHTTPPath, XHTTPMode: in.XHTTPMode, GRPCServiceName: in.GRPCServiceName, WSPath: in.WSPath,
		Obfs: in.Obfs, Masquerade: in.Masquerade, UpMbps: in.UpMbps, DownMbps: in.DownMbps,
		HopPorts: in.HopPorts, HopInterval: in.HopInterval,
		Expert: in.Expert,
	}
}

func (s *server) listInbounds(w http.ResponseWriter, r *http.Request) {
	list, err := s.Inbounds.List(r.Context())
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	out := make([]inboundDTO, len(list))
	for i, ib := range list {
		out[i] = toInboundDTO(ib)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) getInbound(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ib, err := s.Inbounds.Get(r.Context(), id)
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toInboundDTO(ib))
}

func (s *server) createInbound(w http.ResponseWriter, r *http.Request) {
	var in inboundInputDTO
	if !decodeJSON(w, r, &in) {
		return
	}
	ib, err := s.Inbounds.Create(r.Context(), adminFrom(r.Context()).ID, in.toInput())
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toInboundDTO(ib))
}

func (s *server) updateInbound(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in inboundInputDTO
	if !decodeJSON(w, r, &in) {
		return
	}
	ib, err := s.Inbounds.Update(r.Context(), adminFrom(r.Context()).ID, id, in.toInput())
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toInboundDTO(ib))
}

func (s *server) deleteInbound(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.Inbounds.Delete(r.Context(), adminFrom(r.Context()).ID, id); err != nil {
		s.handleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Sites that serve TLS 1.3 + HTTP/2 and are commonly used as Reality
// targets. Best is a site hosted in the same network as the server.
var realityTargets = []string{
	"www.microsoft.com", "www.apple.com", "dl.google.com", "www.samsung.com",
	"www.nvidia.com", "addons.mozilla.org", "www.amd.com", "www.cisco.com",
}

// Sites with HTTP/3 that work well as a Hysteria2 masquerade.
var masqueradeSites = []string{
	"https://www.bing.com/", "https://www.cloudflare.com/", "https://www.google.com/", "https://www.youtube.com/",
}

// scanTargets measures candidate Reality / ShadowTLS sites from this server.
func (s *server) scanTargets(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Hosts []string `json:"hosts"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	results, err := s.Scanner.Scan(r.Context(), in.Hosts)
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *server) createTestKit(w http.ResponseWriter, r *http.Request) {
	list, err := s.Inbounds.CreateTestKit(r.Context(), adminFrom(r.Context()).ID)
	if errors.Is(err, service.ErrTestKitExists) {
		writeError(w, http.StatusConflict, "test_kit_exists", err.Error())
		return
	}
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int{"inbounds": len(list)})
}

func (s *server) deleteTestKit(w http.ResponseWriter, r *http.Request) {
	n, err := s.Inbounds.DeleteTestKit(r.Context(), adminFrom(r.Context()).ID)
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"inbounds": n})
}

func (s *server) suggestInbound(w http.ResponseWriter, r *http.Request) {
	networks := service.Networks(r.URL.Query().Get("protocol"))
	if networks == nil {
		networks = []string{"tcp"}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"port":         s.Inbounds.SuggestPort(r.Context(), networks...),
		"targets":      realityTargets,
		"masquerades":  masqueradeSites,
		"fingerprints": service.Fingerprints,
		"xhttpModes":   service.XHTTPModes,
		"publicHost":   s.PublicHost,
	})
}
