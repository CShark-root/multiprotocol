package amnezia

// Client management for AmneziaWG containers, done the way the Amnezia app
// does it so that both stay in sync: a [Peer] block appended to awg0.conf,
// `awg syncconf` to apply it without a restart, and the name in
// clientsTable. The app reads the same files, so clients added here show
// up there and the other way round.
//
// Every change is careful: the files are copied to a backup first, parsed
// and refused when they look unfamiliar, re-read right before writing (so a
// change made by the app in the meantime is not lost), checked with
// `awg show` afterwards, and put back as they were on any failure.

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/zlib"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// BackupDir keeps copies of the files before every change.
const BackupDir = "/var/lib/panel-amnezia/backups"

// awgLayout is where the Amnezia app keeps an AmneziaWG container's files.
type awgLayout struct {
	dir, conf, table, pub, psk, bin, iface string
}

// Only the current AmneziaWG container is managed; older ones stay
// read-only.
var layouts = map[string]awgLayout{
	"amnezia-awg2": {
		dir: "/opt/amnezia/awg", conf: "/opt/amnezia/awg/awg0.conf", table: "/opt/amnezia/awg/clientsTable",
		pub: "/opt/amnezia/awg/wireguard_server_public_key.key", psk: "/opt/amnezia/awg/wireguard_psk.key",
		bin: "awg", iface: "awg0",
	},
}

// Manageable reports whether the panel can add clients to a container.
func Manageable(name string) bool { _, ok := layouts[name]; return ok }

// manager serialises changes: one at a time across all containers.
var manageMu sync.Mutex

// file is one file read from a container, with what is needed to write it
// back unchanged in mode and owner.
type file struct {
	data       []byte
	mode       int64
	uid, gid   int
	exists     bool
	modifiedAt time.Time
}

func (d *docker) raw(ctx context.Context, method, p, ctype string, body io.Reader) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+p, body)
	if err != nil {
		return nil, 0, err
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return data, resp.StatusCode, err
}

// readFile copies one file out of a container through the archive API.
func (d *docker) readFile(ctx context.Context, id, p string) (file, error) {
	data, code, err := d.raw(ctx, http.MethodGet, "/containers/"+id+"/archive?"+url.Values{"path": {p}}.Encode(), "", nil)
	if err != nil {
		return file{}, err
	}
	if code == http.StatusNotFound {
		return file{}, nil
	}
	if code != http.StatusOK {
		return file{}, fmt.Errorf("read %s: HTTP %d", p, code)
	}
	tr := tar.NewReader(bytes.NewReader(data))
	h, err := tr.Next()
	if err != nil {
		return file{}, fmt.Errorf("read %s: %w", p, err)
	}
	if h.Typeflag != tar.TypeReg {
		return file{}, fmt.Errorf("%s is not a regular file", p)
	}
	body, err := io.ReadAll(io.LimitReader(tr, 4<<20))
	if err != nil {
		return file{}, err
	}
	return file{data: body, mode: h.Mode, uid: h.Uid, gid: h.Gid, exists: true, modifiedAt: h.ModTime}, nil
}

// writeFile puts a file into a container, keeping the old mode and owner.
func (d *docker) writeFile(ctx context.Context, id, p string, data []byte, like file) error {
	mode, uid, gid := like.mode, like.uid, like.gid
	if !like.exists {
		mode = 0o644
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	tw.WriteHeader(&tar.Header{Name: path.Base(p), Mode: mode, Uid: uid, Gid: gid, Size: int64(len(data)), ModTime: time.Now(), Typeflag: tar.TypeReg})
	tw.Write(data)
	tw.Close()
	_, code, err := d.raw(ctx, http.MethodPut, "/containers/"+id+"/archive?"+url.Values{"path": {path.Dir(p)}}.Encode(), "application/x-tar", &buf)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("write %s: HTTP %d", p, code)
	}
	return nil
}

// run executes a fixed command in a container and returns its output and
// exit code.
func (d *docker) run(ctx context.Context, id string, cmd []string) (string, int, error) {
	var created struct {
		ID string `json:"Id"`
	}
	if _, err := d.do(ctx, http.MethodPost, "/containers/"+id+"/exec", map[string]any{
		"Cmd": cmd, "AttachStdout": true, "AttachStderr": true, "Tty": false,
	}, &created); err != nil {
		return "", -1, err
	}
	out, err := d.do(ctx, http.MethodPost, "/exec/"+created.ID+"/start", map[string]any{"Detach": false, "Tty": false}, nil)
	if err != nil {
		return "", -1, err
	}
	var info struct {
		ExitCode int  `json:"ExitCode"`
		Running  bool `json:"Running"`
	}
	for i := 0; i < 20; i++ {
		if _, err := d.do(ctx, http.MethodGet, "/exec/"+created.ID+"/json", nil, &info); err != nil {
			return "", -1, err
		}
		if !info.Running {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return strings.Join(demux(out), "\n"), info.ExitCode, nil
}

// container finds a running Amnezia container by name.
func (d *docker) container(ctx context.Context, name string) (dockerContainer, error) {
	list, err := d.list(ctx)
	if err != nil {
		return dockerContainer{}, err
	}
	for _, c := range list {
		if strings.TrimPrefix(c.Names[0], "/") == name {
			if c.State != "running" {
				return dockerContainer{}, errors.New("контейнер не запущен")
			}
			return c, nil
		}
	}
	return dockerContainer{}, errors.New("нет такого контейнера Amnezia")
}

// ---- awg0.conf ----

// awgConf is a parsed server config. Raw keeps the exact text: changes are
// made by appending or cutting whole [Peer] sections, never by rewriting.
type awgConf struct {
	raw       string
	iface     map[string]string // [Interface] keys, including "# I1 = …" comments
	ifaceKeys []string          // in file order
	subnet    netip.Prefix
	port      int
	peers     []awgPeer
}

type awgPeer struct {
	key string
	ips []netip.Prefix
}

var confLine = regexp.MustCompile(`^\s*(#\s*)?([A-Za-z][A-Za-z0-9]*)\s*=\s*(.*?)\s*$`)

// Keys the Amnezia app writes. Anything else means a format the panel does
// not know, and it then refuses to edit the file.
var knownIface = []string{
	"PrivateKey", "Address", "ListenPort", "Jc", "Jmin", "Jmax", "S1", "S2", "S3", "S4",
	"H1", "H2", "H3", "H4", "I1", "I2", "I3", "I4", "I5", "HeaderProtectionKey", "ContentPaddingAddition",
	"RekeyAfterTime", "RekeyTimeout", "RejectAfterTime", "KeepaliveTimeout", "MaxHandshakeAttempts",
	"RandomTrailers", "DisableCookies", "MTU", "DNS", "PostUp", "PostDown", "PreUp", "PreDown", "SaveConfig", "Table", "FwMark",
}
var knownPeer = []string{"PublicKey", "PresharedKey", "AllowedIPs", "Endpoint", "PersistentKeepalive"}

func parseConf(text string) (awgConf, error) {
	c := awgConf{raw: text, iface: map[string]string{}}
	if !utf8.ValidString(text) || strings.Contains(text, "\x00") {
		return c, errors.New("awg0.conf: не текст")
	}
	section := ""
	var peer *awgPeer
	flush := func() error {
		if peer == nil {
			return nil
		}
		if peer.key == "" {
			return errors.New("awg0.conf: [Peer] без PublicKey")
		}
		c.peers = append(c.peers, *peer)
		peer = nil
		return nil
	}
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimRight(sc.Text(), "\r"))
		switch {
		case line == "":
			continue
		case line == "[Interface]":
			if section != "" {
				return c, errors.New("awg0.conf: [Interface] не в начале")
			}
			section = "interface"
			continue
		case line == "[Peer]":
			if section == "" {
				return c, errors.New("awg0.conf: нет [Interface]")
			}
			if err := flush(); err != nil {
				return c, err
			}
			section, peer = "peer", &awgPeer{}
			continue
		}
		m := confLine.FindStringSubmatch(line)
		if m == nil {
			if strings.HasPrefix(line, "#") {
				continue // a plain comment
			}
			return c, fmt.Errorf("awg0.conf: непонятная строка %q", short(line))
		}
		comment, key, val := m[1] != "", m[2], m[3]
		switch section {
		case "interface":
			if !slices.Contains(knownIface, key) {
				if comment {
					continue
				}
				return c, fmt.Errorf("awg0.conf: незнакомый параметр %s", key)
			}
			if _, dup := c.iface[key]; !dup {
				c.ifaceKeys = append(c.ifaceKeys, key)
			}
			if comment {
				// "# I1 = …": the app keeps client-only values commented out.
				if _, set := c.iface[key]; !set {
					c.iface["#"+key] = val
				}
				continue
			}
			c.iface[key] = val
		case "peer":
			if comment {
				continue
			}
			if !slices.Contains(knownPeer, key) {
				return c, fmt.Errorf("awg0.conf: незнакомый параметр клиента %s", key)
			}
			switch key {
			case "PublicKey":
				if !validKey(val) {
					return c, errors.New("awg0.conf: неверный PublicKey")
				}
				peer.key = val
			case "AllowedIPs":
				for _, s := range strings.Split(val, ",") {
					p, err := netip.ParsePrefix(strings.TrimSpace(s))
					if err != nil {
						return c, fmt.Errorf("awg0.conf: неверный AllowedIPs %q", s)
					}
					peer.ips = append(peer.ips, p)
				}
			}
		default:
			return c, errors.New("awg0.conf: нет [Interface]")
		}
	}
	if err := flush(); err != nil {
		return c, err
	}
	if c.iface["PrivateKey"] == "" {
		return c, errors.New("awg0.conf: нет PrivateKey")
	}
	addr, err := netip.ParsePrefix(strings.TrimSpace(strings.Split(c.iface["Address"], ",")[0]))
	if err != nil || !addr.Addr().Is4() {
		return c, errors.New("awg0.conf: нет IPv4 Address")
	}
	c.subnet = addr
	if c.port, err = strconv.Atoi(c.iface["ListenPort"]); err != nil || c.port < 1 || c.port > 65535 {
		return c, errors.New("awg0.conf: нет ListenPort")
	}
	seen := map[string]bool{}
	for _, p := range c.peers {
		if seen[p.key] {
			return c, errors.New("awg0.conf: клиент записан дважды")
		}
		seen[p.key] = true
	}
	return c, nil
}

func validKey(s string) bool {
	b, err := base64.StdEncoding.DecodeString(s)
	return err == nil && len(b) == 32
}

// nextIP picks a free client address, after the highest one in use like the
// app does, or the first free one when the subnet end is reached.
func (c awgConf) nextIP() (netip.Addr, error) {
	used := map[netip.Addr]bool{c.subnet.Addr(): true}
	last := c.subnet.Masked().Addr()
	for _, p := range c.peers {
		for _, ip := range p.ips {
			if ip.Addr().Is4() {
				used[ip.Addr()] = true
				if c.subnet.Contains(ip.Addr()) && last.Less(ip.Addr()) {
					last = ip.Addr()
				}
			}
		}
	}
	ok := func(a netip.Addr) bool {
		b := a.As4()
		return c.subnet.Contains(a) && !used[a] && b[3] != 0 && b[3] != 255 && a != c.subnet.Masked().Addr()
	}
	for a := last.Next(); c.subnet.Contains(a); a = a.Next() {
		if ok(a) {
			return a, nil
		}
	}
	for a := c.subnet.Masked().Addr().Next(); c.subnet.Contains(a); a = a.Next() {
		if ok(a) {
			return a, nil
		}
	}
	return netip.Addr{}, errors.New("в подсети AmneziaWG закончились адреса")
}

// withoutPeer cuts the [Peer] section with this key out of the raw text,
// leaving everything else byte for byte.
func withoutPeer(raw, key string) (string, bool) {
	lines := strings.SplitAfter(raw, "\n")
	var out strings.Builder
	found := false
	for i := 0; i < len(lines); {
		if strings.TrimSpace(lines[i]) != "[Peer]" {
			out.WriteString(lines[i])
			i++
			continue
		}
		j := i + 1
		for j < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[j]), "[") {
			j++
		}
		block := lines[i:j]
		mine := false
		for _, l := range block {
			if m := confLine.FindStringSubmatch(strings.TrimSpace(l)); m != nil && m[1] == "" && m[2] == "PublicKey" && m[3] == key {
				mine = true
			}
		}
		if mine && !found {
			found = true
		} else {
			for _, l := range block {
				out.WriteString(l)
			}
		}
		i = j
	}
	return out.String(), found
}

// ---- clientsTable ----

// clientsTable is kept as raw JSON objects so fields the panel does not
// know survive a rewrite.
func parseTable(f file) ([]map[string]any, error) {
	if !f.exists || len(bytes.TrimSpace(f.data)) == 0 {
		return []map[string]any{}, nil
	}
	var list []map[string]any
	if err := json.Unmarshal(f.data, &list); err != nil {
		return nil, errors.New("clientsTable в старом формате — откройте список клиентов в приложении Amnezia один раз, оно его обновит")
	}
	return list, nil
}

func tableJSON(list []map[string]any) []byte {
	b, _ := json.MarshalIndent(list, "", "    ") // as the app writes it (Qt, sorted keys)
	return append(b, '\n')
}

func tableIndex(list []map[string]any, key string) int {
	for i, e := range list {
		if e["clientId"] == key {
			return i
		}
	}
	return -1
}

// ---- operations ----

// AddedClient is what an added client needs to connect.
type AddedClient struct {
	Key     string `json:"key"`     // public key (the client id)
	IP      string `json:"ip"`      // its address inside the tunnel
	Config  string `json:"config"`  // AmneziaWG config (.conf), the "original format"
	VPNKey  string `json:"vpnKey"`  // vpn:// key for the AmneziaVPN app
	Version string `json:"version"` // AmneziaWG protocol version
}

type session struct {
	d      *docker
	id     string
	lay    awgLayout
	name   string
	backup string
}

func open(ctx context.Context, socket, container string) (*session, error) {
	lay, ok := layouts[container]
	if !ok {
		return nil, errors.New("этим контейнером панель не управляет")
	}
	d := newDocker(socket)
	c, err := d.container(ctx, container)
	if err != nil {
		return nil, err
	}
	return &session{d: d, id: c.ID, lay: lay, name: container}, nil
}

// snapshot reads the conf and table and stores a backup copy on the host.
func (s *session) snapshot(ctx context.Context) (file, file, error) {
	conf, err := s.d.readFile(ctx, s.id, s.lay.conf)
	if err != nil {
		return file{}, file{}, err
	}
	if !conf.exists {
		return file{}, file{}, errors.New("в контейнере нет " + s.lay.conf)
	}
	table, err := s.d.readFile(ctx, s.id, s.lay.table)
	if err != nil {
		return file{}, file{}, err
	}
	dir := filepath.Join(BackupDir, s.name+"-"+time.Now().UTC().Format("20060102-150405.000"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return file{}, file{}, fmt.Errorf("резервная копия: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, path.Base(s.lay.conf)), conf.data, 0o600); err != nil {
		return file{}, file{}, fmt.Errorf("резервная копия: %w", err)
	}
	if table.exists {
		if err := os.WriteFile(filepath.Join(dir, "clientsTable"), table.data, 0o600); err != nil {
			return file{}, file{}, fmt.Errorf("резервная копия: %w", err)
		}
	}
	s.backup = dir
	pruneBackups(s.name, 50)
	return conf, table, nil
}

func pruneBackups(name string, keep int) {
	entries, err := os.ReadDir(BackupDir)
	if err != nil {
		return
	}
	var mine []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), name+"-") {
			mine = append(mine, e.Name())
		}
	}
	sort.Strings(mine)
	for len(mine) > keep {
		os.RemoveAll(filepath.Join(BackupDir, mine[0]))
		mine = mine[1:]
	}
}

// unchanged makes sure nobody (the app) wrote the files since snapshot.
func (s *session) unchanged(ctx context.Context, conf, table file) error {
	c, err := s.d.readFile(ctx, s.id, s.lay.conf)
	if err != nil {
		return err
	}
	t, err := s.d.readFile(ctx, s.id, s.lay.table)
	if err != nil {
		return err
	}
	if sha256.Sum256(c.data) != sha256.Sum256(conf.data) || sha256.Sum256(t.data) != sha256.Sum256(table.data) || t.exists != table.exists {
		return errors.New("файлы только что изменились (возможно, приложением Amnezia) — повторите")
	}
	return nil
}

// sync applies awg0.conf to the running interface without a restart.
func (s *session) sync(ctx context.Context) error {
	cmd := []string{"bash", "-c", fmt.Sprintf("%[1]s syncconf %[2]s <(%[1]s-quick strip %[3]s)", s.lay.bin, s.lay.iface, s.lay.conf)}
	out, code, err := s.d.run(ctx, s.id, cmd)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("%s syncconf: %s", s.lay.bin, strings.TrimSpace(out))
	}
	return nil
}

// livePeers lists the public keys the running interface knows.
func (s *session) livePeers(ctx context.Context) (map[string]bool, error) {
	out, code, err := s.d.run(ctx, s.id, []string{s.lay.bin, "show", s.lay.iface, "peers"})
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("%s show: %s", s.lay.bin, strings.TrimSpace(out))
	}
	peers := map[string]bool{}
	for _, l := range strings.Fields(out) {
		peers[l] = true
	}
	return peers, nil
}

// restore puts the snapshot back and re-applies it.
func (s *session) restore(ctx context.Context, conf, table file) error {
	errs := []error{s.d.writeFile(ctx, s.id, s.lay.conf, conf.data, conf)}
	if table.exists {
		errs = append(errs, s.d.writeFile(ctx, s.id, s.lay.table, table.data, table))
	}
	errs = append(errs, s.sync(ctx))
	return errors.Join(errs...)
}

// commit writes the new files, applies them and checks the result with
// check; on any failure the snapshot comes back.
func (s *session) commit(ctx context.Context, conf, table file, newConf, newTable []byte, check func(map[string]bool) bool) error {
	if err := s.unchanged(ctx, conf, table); err != nil {
		return err
	}
	fail := func(err error) error {
		if rerr := s.restore(ctx, conf, table); rerr != nil {
			return fmt.Errorf("%w; откат не удался (%v) — копия файлов: %s", err, rerr, s.backup)
		}
		return fmt.Errorf("%w (всё возвращено как было)", err)
	}
	if newConf != nil {
		if err := s.d.writeFile(ctx, s.id, s.lay.conf, newConf, conf); err != nil {
			return fail(err)
		}
		if err := s.sync(ctx); err != nil {
			return fail(err)
		}
		live, err := s.livePeers(ctx)
		if err != nil {
			return fail(err)
		}
		if !check(live) {
			return fail(errors.New("AmneziaWG не принял изменение"))
		}
	}
	if err := s.d.writeFile(ctx, s.id, s.lay.table, newTable, table); err != nil {
		return fail(err)
	}
	return nil
}

var clientNameRe = regexp.MustCompile(`^[^\x00-\x1f"\\]{1,64}$`)

func checkName(n string) (string, error) {
	n = strings.TrimSpace(n)
	if !clientNameRe.MatchString(n) {
		return "", errors.New("имя: от 1 до 64 символов, без кавычек и обратной косой черты")
	}
	return n, nil
}

// AddParams: who the client is and how devices reach the server.
type AddParams struct {
	Container string `json:"container"`
	Name      string `json:"name"`
	Host      string `json:"host"` // public address of the server
}

var hostRe = regexp.MustCompile(`^[A-Za-z0-9.:\[\]-]{1,253}$`)

// AddClient creates a client like the app's "Share" does.
func AddClient(ctx context.Context, socket string, p AddParams) (AddedClient, error) {
	manageMu.Lock()
	defer manageMu.Unlock()
	name, err := checkName(p.Name)
	if err != nil {
		return AddedClient{}, err
	}
	if !hostRe.MatchString(p.Host) {
		return AddedClient{}, errors.New("неизвестен адрес сервера")
	}
	s, err := open(ctx, socket, p.Container)
	if err != nil {
		return AddedClient{}, err
	}
	confF, tableF, err := s.snapshot(ctx)
	if err != nil {
		return AddedClient{}, err
	}
	conf, err := parseConf(string(confF.data))
	if err != nil {
		return AddedClient{}, fmt.Errorf("%w — менять не буду", err)
	}
	table, err := parseTable(tableF)
	if err != nil {
		return AddedClient{}, err
	}
	pubF, err := s.d.readFile(ctx, s.id, s.lay.pub)
	if err != nil {
		return AddedClient{}, err
	}
	pskF, err := s.d.readFile(ctx, s.id, s.lay.psk)
	if err != nil {
		return AddedClient{}, err
	}
	serverPub, psk := strings.TrimSpace(string(pubF.data)), strings.TrimSpace(string(pskF.data))
	if !validKey(serverPub) || (psk != "" && !validKey(psk)) {
		return AddedClient{}, errors.New("не прочитались ключи сервера — менять не буду")
	}
	ip, err := conf.nextIP()
	if err != nil {
		return AddedClient{}, err
	}
	priv, pub, err := genKey()
	if err != nil {
		return AddedClient{}, err
	}

	// The peer exactly as the app appends it.
	peer := "[Peer]\nPublicKey = " + pub + "\n"
	if psk != "" {
		peer += "PresharedKey = " + psk + "\n"
	}
	peer += "AllowedIPs = " + ip.String() + "/32\n\n"
	raw := conf.raw
	if raw != "" && !strings.HasSuffix(raw, "\n") {
		raw += "\n"
	}
	newConf := raw + peer
	if c2, err := parseConf(newConf); err != nil || len(c2.peers) != len(conf.peers)+1 {
		return AddedClient{}, errors.New("не удалось собрать новый конфиг — менять не буду")
	}
	table = append(table, map[string]any{
		"clientId": pub,
		"userData": map[string]any{"clientName": name, "creationDate": time.Now().Format("Mon Jan 2 15:04:05 2006")},
	})
	if err := s.commit(ctx, confF, tableF, []byte(newConf), tableJSON(table), func(live map[string]bool) bool { return live[pub] }); err != nil {
		return AddedClient{}, err
	}

	port, err := publicPort(ctx, s.d, s.id, conf.port)
	if err != nil {
		return AddedClient{}, err
	}
	dns := "1.1.1.1, 1.0.0.1"
	if _, err := s.d.container(ctx, "amnezia-dns"); err == nil {
		dns = "172.29.172.254, 1.0.0.1" // AmneziaDNS, as the app uses it when installed
	}
	cfg := clientConfig(conf, priv, ip, dns, serverPub, psk, p.Host, port)
	// The app keeps MTU apart from the config text it stores.
	key, err := vpnKey(conf, strings.Replace(cfg, "MTU = 1280\n", "", 1), clientData{
		container: p.Container, description: "MultiProtocol · " + name, host: p.Host, ip: ip.String(), priv: priv, pub: pub,
		serverPub: serverPub, psk: psk, dns: dns, port: port,
	})
	if err != nil {
		return AddedClient{}, err
	}
	return AddedClient{Key: pub, IP: ip.String(), Config: cfg, VPNKey: key, Version: conf.Version()}, nil
}

// Interface keys a client gets from the server: the obfuscation settings
// must match on both ends.
var clientKeys = []string{
	"Jc", "Jmin", "Jmax", "S1", "S2", "S3", "S4", "H1", "H2", "H3", "H4", "I1", "I2", "I3", "I4", "I5",
	"HeaderProtectionKey", "ContentPaddingAddition", "RekeyAfterTime", "RekeyTimeout", "RejectAfterTime",
	"KeepaliveTimeout", "MaxHandshakeAttempts", "RandomTrailers", "DisableCookies",
}

// clientConfig follows the app's client template (server_scripts/awg/template.conf).
func clientConfig(c awgConf, priv string, ip netip.Addr, dns, serverPub, psk, host string, port int) string {
	var b strings.Builder
	b.WriteString("[Interface]\nAddress = " + ip.String() + "/32\nDNS = " + dns + "\nPrivateKey = " + priv + "\n")
	for _, k := range clientKeys {
		v, ok := c.iface[k]
		if !ok {
			v = c.iface["#"+k]
		}
		if v != "" {
			b.WriteString(k + " = " + v + "\n")
		}
	}
	b.WriteString("MTU = 1280\n\n[Peer]\nPublicKey = " + serverPub + "\n")
	if psk != "" {
		b.WriteString("PresharedKey = " + psk + "\n")
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	b.WriteString("AllowedIPs = 0.0.0.0/0, ::/0\nEndpoint = " + host + ":" + strconv.Itoa(port) + "\nPersistentKeepalive = 25\n")
	return b.String()
}

// publicPort is the host port Docker maps to the container's ListenPort.
func publicPort(ctx context.Context, d *docker, id string, listen int) (int, error) {
	list, err := d.list(ctx)
	if err != nil {
		return 0, err
	}
	for _, c := range list {
		if c.ID != id {
			continue
		}
		for _, p := range c.Ports {
			if p.Type == "udp" && p.PrivatePort == listen && p.PublicPort != 0 {
				return p.PublicPort, nil
			}
		}
	}
	return listen, nil
}

func genKey() (string, string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", "", err
	}
	b[0] &= 248 // as `awg genkey` clamps
	b[31] = b[31]&127 | 64
	k, err := ecdh.X25519().NewPrivateKey(b[:])
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(b[:]), base64.StdEncoding.EncodeToString(k.PublicKey().Bytes()), nil
}

// KeyParams names a client by its public key.
type KeyParams struct {
	Container string `json:"container"`
	Key       string `json:"key"`
	Name      string `json:"name,omitempty"`
}

// RemoveClient revokes a client like the app's "Revoke" does.
func RemoveClient(ctx context.Context, socket string, p KeyParams) error {
	manageMu.Lock()
	defer manageMu.Unlock()
	if !validKey(p.Key) {
		return errors.New("неверный ключ клиента")
	}
	s, err := open(ctx, socket, p.Container)
	if err != nil {
		return err
	}
	confF, tableF, err := s.snapshot(ctx)
	if err != nil {
		return err
	}
	conf, err := parseConf(string(confF.data))
	if err != nil {
		return fmt.Errorf("%w — менять не буду", err)
	}
	table, err := parseTable(tableF)
	if err != nil {
		return err
	}
	newRaw, found := withoutPeer(conf.raw, p.Key)
	var newConf []byte
	if found {
		c2, err := parseConf(newRaw)
		if err != nil || len(c2.peers) != len(conf.peers)-1 {
			return errors.New("не удалось собрать новый конфиг — менять не буду")
		}
		newConf = []byte(newRaw)
	}
	i := tableIndex(table, p.Key)
	if !found && i < 0 {
		return errors.New("такого клиента нет")
	}
	if i >= 0 {
		table = slices.Delete(table, i, i+1)
	}
	return s.commit(ctx, confF, tableF, newConf, tableJSON(table), func(live map[string]bool) bool { return !live[p.Key] })
}

// RenameClient changes only the name in clientsTable.
func RenameClient(ctx context.Context, socket string, p KeyParams) error {
	manageMu.Lock()
	defer manageMu.Unlock()
	name, err := checkName(p.Name)
	if err != nil {
		return err
	}
	if !validKey(p.Key) {
		return errors.New("неверный ключ клиента")
	}
	s, err := open(ctx, socket, p.Container)
	if err != nil {
		return err
	}
	confF, tableF, err := s.snapshot(ctx)
	if err != nil {
		return err
	}
	conf, err := parseConf(string(confF.data))
	if err != nil {
		return fmt.Errorf("%w — менять не буду", err)
	}
	table, err := parseTable(tableF)
	if err != nil {
		return err
	}
	i := tableIndex(table, p.Key)
	if i < 0 {
		// A peer the app never named: add it to the table, like the app does
		// when it lists clients.
		known := slices.ContainsFunc(conf.peers, func(pe awgPeer) bool { return pe.key == p.Key })
		if !known {
			return errors.New("такого клиента нет")
		}
		table = append(table, map[string]any{"clientId": p.Key, "userData": map[string]any{}})
		i = len(table) - 1
	}
	ud, _ := table[i]["userData"].(map[string]any)
	if ud == nil {
		ud = map[string]any{}
	}
	ud["clientName"] = name
	if _, ok := ud["creationDate"]; !ok {
		ud["creationDate"] = time.Now().Format("Mon Jan 2 15:04:05 2006")
	}
	table[i]["userData"] = ud
	return s.commit(ctx, confF, tableF, nil, tableJSON(table), nil)
}

// ---- AmneziaWG version and the AmneziaVPN key ----

var awg3Keys = []string{"HeaderProtectionKey", "ContentPaddingAddition", "RekeyAfterTime", "RekeyTimeout", "RejectAfterTime", "KeepaliveTimeout", "MaxHandshakeAttempts"}

// value of an [Interface] key, set or kept in a comment by the app.
func (c awgConf) value(k string) string {
	if v, ok := c.iface[k]; ok {
		return v
	}
	return c.iface["#"+k]
}

// Version names the AmneziaWG protocol version the way the Amnezia app does
// (AwgProtocolConfig::awgVersionOf): "3.1", "2", "1.5" or "1".
func (c awgConf) Version() string {
	on := func(k string) bool {
		v := strings.TrimSpace(c.value(k))
		return v != "" && !strings.EqualFold(v, "off")
	}
	for _, k := range awg3Keys {
		if strings.TrimSpace(c.value(k)) != "" {
			return "3.1"
		}
	}
	if on("RandomTrailers") || on("DisableCookies") {
		return "3.1"
	}
	if c.value("S3") != "" || c.value("S4") != "" {
		return "2"
	}
	for _, k := range []string{"H1", "H2", "H3", "H4"} {
		if strings.Contains(c.value(k), "-") {
			return "2"
		}
	}
	for _, k := range []string{"I1", "I2", "I3", "I4", "I5"} {
		if c.value(k) != "" {
			return "1.5"
		}
	}
	return "1"
}

// vpnKey builds the "vpn://" key the AmneziaVPN app exports for one
// connection (ExportController::generateConnectionConfig): the server JSON
// with this container and its client config, zlib-compressed the Qt way
// (qCompress: 4-byte big-endian length, then zlib) and base64url-encoded.
func vpnKey(c awgConf, native string, cl clientData) (string, error) {
	params := map[string]any{}
	for _, k := range clientKeys {
		if v := c.value(k); v != "" {
			params[k] = v
		}
	}
	client := map[string]any{
		"config": native, "hostName": cl.host, "port": cl.port, "client_ip": cl.ip,
		"client_priv_key": cl.priv, "client_pub_key": cl.pub, "server_pub_key": cl.serverPub, "psk_key": cl.psk,
		"clientId": cl.pub, "allowed_ips": []string{"0.0.0.0/0", "::/0"}, "persistent_keep_alive": "25", "mtu": "1280",
	}
	server := map[string]any{
		"port": strconv.Itoa(cl.port), "transport_proto": "udp", "subnet_address": c.subnet.Masked().Addr().String(),
	}
	for k, v := range params {
		client[k], server[k] = v, v
	}
	for _, k := range []string{"I1", "I2", "I3", "I4", "I5"} {
		if _, ok := server[k]; !ok {
			server[k] = "" // the app always writes these
		}
	}
	last, err := json.Marshal(client)
	if err != nil {
		return "", err
	}
	server["last_config"] = string(last)
	dns := strings.Split(cl.dns, ",")
	doc := map[string]any{
		"format_version":   1,
		"description":      cl.description,
		"hostName":         cl.host,
		"containers":       []any{map[string]any{"container": cl.container, "awg": server}},
		"defaultContainer": cl.container,
		"dns1":             strings.TrimSpace(dns[0]),
	}
	if len(dns) > 1 {
		doc["dns2"] = strings.TrimSpace(dns[1])
	}
	body, err := json.MarshalIndent(doc, "", "    ")
	if err != nil {
		return "", err
	}
	var z bytes.Buffer
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(body)))
	z.Write(n[:])
	zw, _ := zlib.NewWriterLevel(&z, 8)
	zw.Write(body)
	zw.Close()
	return "vpn://" + base64.RawURLEncoding.EncodeToString(z.Bytes()), nil
}

type clientData struct {
	container, description, host, ip, priv, pub, serverPub, psk, dns string
	port                                                             int
}

// DecodeVPNKey reverses vpnKey (for tests and for reading keys back).
func DecodeVPNKey(key string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(strings.TrimPrefix(key, "vpn://"), "="))
	if err != nil || len(raw) < 4 {
		return nil, errors.New("not a vpn:// key")
	}
	zr, err := zlib.NewReader(bytes.NewReader(raw[4:]))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(zr, 1<<20))
}

// PauseParams — клиенты, которых панель держит выключенными (у их
// пользователя кончился срок или трафик, или он выключен).
type PauseParams struct {
	Container string   `json:"container"`
	Keys      []string `json:"keys"`
}

type PauseResult struct {
	Paused   int `json:"paused"`   // убрано с интерфейса сейчас
	Restored int `json:"restored"` // возвращено из awg0.conf
}

// SetPaused приводит работающий интерфейс к виду «все клиенты из awg0.conf,
// кроме Keys». Файлы не меняются: приложение Amnezia по-прежнему видит всех
// клиентов, а после перезапуска контейнера или правки из приложения панель
// за минуту снова уберёт выключенных.
func SetPaused(ctx context.Context, socket string, p PauseParams) (PauseResult, error) {
	manageMu.Lock()
	defer manageMu.Unlock()
	var res PauseResult
	paused := map[string]bool{}
	for _, k := range p.Keys {
		if !validKey(k) {
			return res, errors.New("неверный ключ клиента")
		}
		paused[k] = true
	}
	s, err := open(ctx, socket, p.Container)
	if err != nil {
		return res, err
	}
	confF, err := s.d.readFile(ctx, s.id, s.lay.conf)
	if err != nil {
		return res, err
	}
	conf, err := parseConf(string(confF.data))
	if err != nil {
		return res, fmt.Errorf("%w — менять не буду", err)
	}
	live, err := s.livePeers(ctx)
	if err != nil {
		return res, err
	}
	// Кто-то включённый пропал с интерфейса — возвращаем всех из файла.
	for _, peer := range conf.peers {
		if !paused[peer.key] && !live[peer.key] {
			res.Restored++
		}
	}
	if res.Restored > 0 {
		if err := s.sync(ctx); err != nil {
			return res, err
		}
		if live, err = s.livePeers(ctx); err != nil {
			return res, err
		}
	}
	for k := range paused {
		if !live[k] {
			continue
		}
		out, code, err := s.d.run(ctx, s.id, []string{s.lay.bin, "set", s.lay.iface, "peer", k, "remove"})
		if err != nil {
			return res, err
		}
		if code != 0 {
			return res, fmt.Errorf("%s set: %s", s.lay.bin, strings.TrimSpace(out))
		}
		res.Paused++
	}
	return res, nil
}
