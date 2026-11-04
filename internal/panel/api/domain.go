package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"panel/internal/panel/domain"
	"panel/internal/panel/service"
)

type certDTO struct {
	Domain   string    `json:"domain"`
	NotAfter time.Time `json:"notAfter"`
	Issuer   string    `json:"issuer"`
	Staging  bool      `json:"staging"`
}

type domainDTO struct {
	Name       string               `json:"name"`
	Domain     string               `json:"domain"`
	HasToken   bool                 `json:"hasToken"`
	IPMode     string               `json:"ipMode"`
	IP         string               `json:"ip"`
	Staging    bool                 `json:"staging"`
	Status     service.DomainStatus `json:"status"`
	Cert       *certDTO             `json:"cert"`
	PublicHost string               `json:"publicHost"`
	Terms      string               `json:"terms"`
}

func (s *server) domainDTO(r *http.Request) (domainDTO, error) {
	d, err := s.Domains.Get(r.Context())
	if err != nil {
		return domainDTO{}, err
	}
	out := domainDTO{
		Name: d.Name, Domain: d.Domain(), HasToken: d.Token != "", IPMode: d.IPMode, IP: d.IP, Staging: d.Staging,
		Status: s.Domains.Status(r.Context()), PublicHost: s.PublicHost, Terms: domain.TermsURL,
	}
	if c := s.Domains.Cert(); c != nil {
		out.Cert = &certDTO{Domain: c.Domain, NotAfter: c.NotAfter, Issuer: c.Issuer, Staging: isStaging(c.Issuer)}
	}
	return out, nil
}

// Let's Encrypt staging certificates are issued by "(STAGING) ..." CAs.
func isStaging(issuer string) bool {
	return strings.Contains(issuer, "STAGING") || strings.Contains(issuer, "Fake")
}

func (s *server) getDomain(w http.ResponseWriter, r *http.Request) {
	d, err := s.domainDTO(r)
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *server) saveDomain(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name    string `json:"name"`
		Token   string `json:"token"`
		IPMode  string `json:"ipMode"`
		IP      string `json:"ip"`
		Staging bool   `json:"staging"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if _, err := s.Domains.Save(r.Context(), adminFrom(r.Context()).ID, service.DomainSettings{
		Name: in.Name, Token: in.Token, IPMode: in.IPMode, IP: in.IP, Staging: in.Staging,
	}); err != nil {
		s.handleError(w, r, err)
		return
	}
	s.getDomain(w, r)
}

// issueCert starts issuance in the background (DNS propagation takes a
// minute or two, longer than a request may run); the UI polls getDomain.
func (s *server) issueCert(w http.ResponseWriter, r *http.Request) {
	d, err := s.Domains.Get(r.Context())
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	if d.Name == "" || d.Token == "" {
		writeError(w, http.StatusConflict, "no_domain", service.ErrNoDomain.Error())
		return
	}
	admin := adminFrom(r.Context()).ID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if _, err := s.Domains.Issue(ctx, admin); err != nil {
			s.Logger.Warn("certificate issuance failed", "err", err)
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]bool{"started": true})
}

func (s *server) deleteDomain(w http.ResponseWriter, r *http.Request) {
	if err := s.Domains.Delete(r.Context(), adminFrom(r.Context()).ID); err != nil {
		s.handleError(w, r, err)
		return
	}
	s.getDomain(w, r)
}
