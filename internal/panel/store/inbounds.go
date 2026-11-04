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
	TestKit   bool // created by the "test kit" button, removed with it
	CreatedAt time.Time
	UpdatedAt time.Time
}

// InboundSettings is stored as JSON; only the fields for the inbound's
// transport are set.
type InboundSettings struct {
	Reality     *RealitySettings     `json:"reality,omitempty"`
	XHTTP       *XHTTPSettings       `json:"xhttp,omitempty"`
	GRPC        *GRPCSettings        `json:"grpc,omitempty"`
	WS          *WSSettings          `json:"ws,omitempty"`
	Hysteria    *HysteriaSettings    `json:"hysteria,omitempty"`
	TLS         *TLSSettings         `json:"tls,omitempty"`         // self-signed cert: TUIC, AnyTLS
	Shadowsocks *ShadowsocksSettings `json:"shadowsocks,omitempty"` // Shadowsocks-2022, also inside ShadowTLS
	ShadowTLS   *ShadowTLSSettings   `json:"shadowtls,omitempty"`

	// Expert, Xray inbounds only; nil = Xray defaults.
	Sniffing *SniffingSettings `json:"sniffing,omitempty"`
	Sockopt  *SockoptSettings  `json:"sockopt,omitempty"`
}

type SniffingSettings struct {
	Enabled      bool     `json:"enabled"`
	DestOverride []string `json:"destOverride"` // http, tls, quic, fakedns
	MetadataOnly bool     `json:"metadataOnly"`
	RouteOnly    bool     `json:"routeOnly"`
}

type SockoptSettings struct {
	TCPFastOpen         bool   `json:"tcpFastOpen,omitempty"`
	TCPCongestion       string `json:"tcpCongestion,omitempty"` // bbr | cubic | reno
	TCPMptcp            bool   `json:"tcpMptcp,omitempty"`
	AcceptProxyProtocol bool   `json:"acceptProxyProtocol,omitempty"`
}

type WSSettings struct {
	Path string `json:"path"`
}

type TLSSettings struct {
	SNI       string `json:"sni"`
	CertPEM   string `json:"certPem"`
	KeyPEM    string `json:"keyPem"`
	PinSHA256 string `json:"pinSha256"`
	// DomainCert: the certificate is the panel domain's (trusted), not
	// self-signed; links then need neither pinning nor "insecure".
	DomainCert bool `json:"domainCert,omitempty"`
}

type ShadowsocksSettings struct {
	Method    string `json:"method"`    // 2022-blake3-aes-128-gcm
	ServerPSK string `json:"serverPsk"` // base64, 16 bytes; each user adds their own key
}

type ShadowTLSSettings struct {
	Handshake string `json:"handshake"` // host:port of the real site whose TLS handshake is borrowed
}

type HysteriaSettings struct {
	SNI          string `json:"sni"`
	CertPEM      string `json:"certPem"`
	KeyPEM       string `json:"keyPem"`
	PinSHA256    string `json:"pinSha256"` // lets clients pin the self-signed cert
	DomainCert   bool   `json:"domainCert,omitempty"`
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

	// Expert; zero values = Xray defaults.
	Xver          int    `json:"xver,omitempty"`        // PROXY protocol version sent to the target: 0-2
	MaxTimeDiff   int    `json:"maxTimeDiff,omitempty"` // ms of allowed client clock skew; 0 = no check
	MinClientVer  string `json:"minClientVer,omitempty"`
	MaxClientVer  string `json:"maxClientVer,omitempty"`
	SpiderX       string `json:"spiderX,omitempty"`       // client: path the crawler fetches
	MLDSA65Seed   string `json:"mldsa65Seed,omitempty"`   // server: post-quantum signature key
	MLDSA65Verify string `json:"mldsa65Verify,omitempty"` // client: matching verification key
}

type XHTTPSettings struct {
	Path string `json:"path"`
	Mode string `json:"mode"` // auto | packet-up | stream-up | stream-one

	// Expert; empty = Xray defaults. Ranges are "N" or "N-M".
	Host                string            `json:"host,omitempty"`
	Headers             map[string]string `json:"headers,omitempty"`
	PaddingBytes        string            `json:"paddingBytes,omitempty"`
	PaddingObfsMode     bool              `json:"paddingObfsMode,omitempty"`
	PaddingPlacement    string            `json:"paddingPlacement,omitempty"` // queryInHeader | header | cookie | query
	PaddingMethod       string            `json:"paddingMethod,omitempty"`    // repeat-x | tokenish
	UplinkHTTPMethod    string            `json:"uplinkHttpMethod,omitempty"` // POST | PUT | PATCH | GET (packet-up only)
	SessionPlacement    string            `json:"sessionPlacement,omitempty"` // path | cookie | header | query
	SeqPlacement        string            `json:"seqPlacement,omitempty"`
	UplinkDataPlacement string            `json:"uplinkDataPlacement,omitempty"` // auto | body | cookie | header
	NoSSEHeader         bool              `json:"noSseHeader,omitempty"`
	NoGRPCHeader        bool              `json:"noGrpcHeader,omitempty"`
	MaxEachPostBytes    string            `json:"maxEachPostBytes,omitempty"`
	MinPostsIntervalMs  string            `json:"minPostsIntervalMs,omitempty"`
	MaxBufferedPosts    int               `json:"maxBufferedPosts,omitempty"`
	StreamUpServerSecs  string            `json:"streamUpServerSecs,omitempty"`
	Xmux                *XmuxSettings     `json:"xmux,omitempty"` // client-side connection reuse
}

type XmuxSettings struct {
	MaxConcurrency   string `json:"maxConcurrency,omitempty"`
	MaxConnections   string `json:"maxConnections,omitempty"`
	CMaxReuseTimes   string `json:"cMaxReuseTimes,omitempty"`
	HMaxRequestTimes string `json:"hMaxRequestTimes,omitempty"`
	HMaxReusableSecs string `json:"hMaxReusableSecs,omitempty"`
	HKeepAlivePeriod int    `json:"hKeepAlivePeriod,omitempty"`
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
	TestKit   bool
}

const inboundColumns = `id, remark, protocol, transport, security, port, address, settings_json,
	enabled, test_kit, created_at, updated_at`

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
		(remark, protocol, transport, security, port, address, settings_json, enabled, test_kit, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.Remark, f.Protocol, f.Transport, f.Security, f.Port, f.Address, string(settings), f.Enabled, f.TestKit,
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
	var enabled, testKit, created, updated int64
	err := row.Scan(&in.ID, &in.Remark, &in.Protocol, &in.Transport, &in.Security, &in.Port, &in.Address,
		&settings, &enabled, &testKit, &created, &updated)
	if err != nil {
		return Inbound{}, err
	}
	if err := json.Unmarshal([]byte(settings), &in.Settings); err != nil {
		return Inbound{}, err
	}
	in.Enabled, in.TestKit = enabled != 0, testKit != 0
	in.CreatedAt, in.UpdatedAt = fromUnix(created), fromUnix(updated)
	return in, nil
}
