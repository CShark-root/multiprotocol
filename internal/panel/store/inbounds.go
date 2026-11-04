package store

import (
	"context"
	"encoding/json"
	"time"
)

type Inbound struct {
	ID        int64
	Remark    string
	Protocol  string
	Transport string
	Security  string
	Port      int
	Address   string
	Settings  InboundSettings
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// InboundSettings is stored as JSON; only the fields for the inbound's
// transport are set.
type InboundSettings struct {
	Reality  *RealitySettings  `json:"reality,omitempty"`
	XHTTP    *XHTTPSettings    `json:"xhttp,omitempty"`
	GRPC     *GRPCSettings     `json:"grpc,omitempty"`
	Hysteria *HysteriaSettings `json:"hysteria,omitempty"`
}

type HysteriaSettings struct {
	SNI          string `json:"sni"`
	CertPEM      string `json:"certPem"`
	KeyPEM       string `json:"keyPem"`
	PinSHA256    string `json:"pinSha256"` // lets clients pin the self-signed cert
	ObfsPassword string `json:"obfsPassword,omitempty"`
	Masquerade   string `json:"masquerade"` // site shown to anyone who is not a Hysteria client
	UpMbps       int    `json:"upMbps,omitempty"`
	DownMbps     int    `json:"downMbps,omitempty"`
}

type RealitySettings struct {
	Target      string   `json:"target"` // host:port of the site Reality imitates
	ServerNames []string `json:"serverNames"`
	PrivateKey  string   `json:"privateKey"`
	PublicKey   string   `json:"publicKey"`
	ShortIDs    []string `json:"shortIds"`
	Fingerprint string   `json:"fingerprint"` // client uTLS fingerprint, e.g. chrome
}

type XHTTPSettings struct {
	Path string `json:"path"`
	Mode string `json:"mode"` // auto | packet-up | stream-up | stream-one
}

type GRPCSettings struct {
	ServiceName string `json:"serviceName"`
}

type InboundFields struct {
	Remark    string
	Protocol  string
	Transport string
	Security  string
	Port      int
	Address   string
	Settings  InboundSettings
	Enabled   bool
}

const inboundColumns = `id, remark, protocol, transport, security, port, address, settings_json,
	enabled, created_at, updated_at`

func (s *Store) ListInbounds(ctx context.Context) ([]Inbound, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+inboundColumns+` FROM inbounds ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Inbound{}
	for rows.Next() {
		in, err := scanInbound(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, in)
	}
	return list, rows.Err()
}

func (s *Store) GetInbound(ctx context.Context, id int64) (Inbound, error) {
	in, err := scanInbound(s.db.QueryRowContext(ctx, `SELECT `+inboundColumns+` FROM inbounds WHERE id = ?`, id))
	return in, notFound(err)
}

func (s *Store) CreateInbound(ctx context.Context, f InboundFields, now time.Time) (Inbound, error) {
	settings, err := json.Marshal(f.Settings)
	if err != nil {
		return Inbound{}, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO inbounds
		(remark, protocol, transport, security, port, address, settings_json, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.Remark, f.Protocol, f.Transport, f.Security, f.Port, f.Address, string(settings), f.Enabled,
		now.Unix(), now.Unix())
	if err != nil {
		return Inbound{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Inbound{}, err
	}
	return s.GetInbound(ctx, id)
}

func (s *Store) UpdateInbound(ctx context.Context, id int64, f InboundFields, now time.Time) (Inbound, error) {
	settings, err := json.Marshal(f.Settings)
	if err != nil {
		return Inbound{}, err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE inbounds SET
		remark = ?, protocol = ?, transport = ?, security = ?, port = ?, address = ?, settings_json = ?,
		enabled = ?, updated_at = ? WHERE id = ?`,
		f.Remark, f.Protocol, f.Transport, f.Security, f.Port, f.Address, string(settings), f.Enabled,
		now.Unix(), id)
	if err != nil {
		return Inbound{}, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return Inbound{}, err
	} else if n == 0 {
		return Inbound{}, ErrNotFound
	}
	return s.GetInbound(ctx, id)
}

func (s *Store) DeleteInbound(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM inbounds WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanInbound(row scanner) (Inbound, error) {
	var in Inbound
	var settings string
	var enabled, created, updated int64
	err := row.Scan(&in.ID, &in.Remark, &in.Protocol, &in.Transport, &in.Security, &in.Port, &in.Address,
		&settings, &enabled, &created, &updated)
	if err != nil {
		return Inbound{}, err
	}
	if err := json.Unmarshal([]byte(settings), &in.Settings); err != nil {
		return Inbound{}, err
	}
	in.Enabled = enabled != 0
	in.CreatedAt, in.UpdatedAt = fromUnix(created), fromUnix(updated)
	return in, nil
}
