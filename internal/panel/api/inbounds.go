package api

import (
	"net/http"
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
	Network   string    `json:"network"` // tcp | udp
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

	// Hysteria2
	Obfs       bool   `json:"obfs"`
	Masquerade string `json:"masquerade,omitempty"`
	UpMbps     int    `json:"upMbps,omitempty"`
	DownMbps   int    `json:"downMbps,omitempty"`
	CertPin    string `json:"certPin,omitempty"`
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
	Obfs            bool   `json:"obfs"`
	Masquerade      string `json:"masquerade"`
	UpMbps          int    `json:"upMbps"`
	DownMbps        int    `json:"downMbps"`
	Enabled         *bool  `json:"enabled"` // omitted = true
}

func toInboundDTO(ib store.Inbound) inboundDTO {
	d := inboundDTO{
		ID: ib.ID, Remark: ib.Remark, Protocol: ib.Protocol, Transport: ib.Transport, Security: ib.Security,
		Network: service.Network(ib.Protocol), Port: ib.Port, Address: ib.Address, Enabled: ib.Enabled,
		CreatedAt: ib.CreatedAt,
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
	}
	return d
}

func (in inboundInputDTO) toInput() service.InboundInput {
	return service.InboundInput{
		Remark: in.Remark, Protocol: in.Protocol, Port: in.Port, Address: in.Address,
		Enabled:   in.Enabled == nil || *in.Enabled,
		Transport: in.Transport, Target: in.Target, ServerName: in.ServerName, Fingerprint: in.Fingerprint,
		XHTTPPath: in.XHTTPPath, XHTTPMode: in.XHTTPMode, GRPCServiceName: in.GRPCServiceName,
		Obfs: in.Obfs, Masquerade: in.Masquerade, UpMbps: in.UpMbps, DownMbps: in.DownMbps,
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

func (s *server) suggestInbound(w http.ResponseWriter, r *http.Request) {
	network := r.URL.Query().Get("network")
	if network != "udp" {
		network = "tcp"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"port":         s.Inbounds.SuggestPort(r.Context(), network),
		"targets":      realityTargets,
		"masquerades":  masqueradeSites,
		"fingerprints": service.Fingerprints,
		"xhttpModes":   service.XHTTPModes,
		"publicHost":   s.PublicHost,
	})
}
