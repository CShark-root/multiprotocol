package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"panel/internal/agent"
	"panel/internal/nodeproto"
	"panel/internal/panel/store"
	"panel/internal/panel/validate"
)

// Узлы — другие наши серверы. Панель хранит для каждого желаемое состояние
// (конфиги ядер), а агент забирает его сам через sync и приносит трафик,
// кто онлайн и состояние ядер.

const (
	EventNodeDown = "node.down"
	EventNodeUp   = "node.up"
)

// nodeOffline — сколько без связи, чтобы считать узел недоступным.
const nodeOffline = 2 * time.Minute

type nodeState struct {
	hash    string
	configs map[string][]byte
	files   map[string]string // путь относительно dataDir → sha256
	ready   bool              // желаемое состояние уже собрано

	traffic  []agent.TrafficDelta
	online   map[string]agent.OnlineEntry
	health   agent.NodeHealth
	version  string
	xrayAPI  string
	applied  string
	applyErr string
	seen     time.Time
	commands []string
	down     bool // уже сообщили, что узел пропал

	wake chan struct{} // будит ждущий sync
}

type Nodes struct {
	store   *store.Store
	events  *Events
	dataDir string
	log     *slog.Logger
	now     func() time.Time
	changed func() // попросить синхронизацию собрать конфиги

	mu     sync.Mutex
	states map[int64]*nodeState
	sums   map[string]fileSum // кэш sha256 файлов по пути
}

type fileSum struct {
	size int64
	mod  time.Time
	sum  string
}

func NewNodes(st *store.Store, events *Events, dataDir string, log *slog.Logger) *Nodes {
	return &Nodes{store: st, events: events, dataDir: dataDir, log: log, now: time.Now, changed: func() {},
		states: map[int64]*nodeState{}, sums: map[string]fileSum{}}
}

// SetChanged задаёт, как попросить пересобрать конфиги (Syncer.Trigger).
func (n *Nodes) SetChanged(fn func()) { n.changed = fn }

func (n *Nodes) state(id int64) *nodeState {
	s := n.states[id]
	if s == nil {
		s = &nodeState{wake: make(chan struct{}, 1), online: map[string]agent.OnlineEntry{}}
		n.states[id] = s
	}
	return s
}

func newToken() (token, hash string) {
	b := make([]byte, 32)
	rand.Read(b)
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, hashToken(token)
}

func hashToken(t string) string {
	s := sha256.Sum256([]byte(t))
	return hex.EncodeToString(s[:])
}

func checkNodeFields(name, host string) (string, string, error) {
	name, host = strings.TrimSpace(name), strings.TrimSpace(host)
	if name == "" || len(name) > 64 || strings.ContainsAny(name, "\x00\r\n") {
		return "", "", validate.Fail("name", "1–64 characters")
	}
	if host == "" || len(host) > 253 || strings.ContainsAny(host, " /\\\"'\x00\r\n") {
		return "", "", validate.Fail("host", "an IP address or a domain")
	}
	return name, host, nil
}

// Create добавляет узел и возвращает токен — он показывается один раз.
func (n *Nodes) Create(ctx context.Context, name, host string) (store.Node, string, error) {
	name, host, err := checkNodeFields(name, host)
	if err != nil {
		return store.Node{}, "", err
	}
	token, hash := newToken()
	node, err := n.store.CreateNode(ctx, name, host, hash, n.now())
	return node, token, err
}

func (n *Nodes) Update(ctx context.Context, id int64, name, host string) (store.Node, error) {
	name, host, err := checkNodeFields(name, host)
	if err != nil {
		return store.Node{}, err
	}
	node, err := n.store.UpdateNode(ctx, id, name, host)
	if err == nil {
		n.changed() // адрес узла попадает в ссылки
	}
	return node, err
}

// NewToken выдаёт узлу новый токен (старый перестаёт работать).
func (n *Nodes) NewToken(ctx context.Context, id int64) (string, error) {
	if _, err := n.store.GetNode(ctx, id); err != nil {
		return "", err
	}
	token, hash := newToken()
	return token, n.store.SetNodeToken(ctx, id, hash)
}

func (n *Nodes) Delete(ctx context.Context, id int64) error {
	if err := n.store.DeleteNode(ctx, id); err != nil {
		return err
	}
	n.mu.Lock()
	delete(n.states, id)
	n.mu.Unlock()
	n.changed()
	return nil
}

// Authenticate находит узел по токену из заголовка Authorization.
func (n *Nodes) Authenticate(ctx context.Context, token string) (store.Node, error) {
	if len(token) < 40 || len(token) > 100 {
		return store.Node{}, store.ErrNotFound
	}
	return n.store.NodeByToken(ctx, hashToken(token))
}

// SetDesired задаёт состояние узла: конфиги ядер и файлы (абсолютные пути
// внутри dataDir), которые ему нужны.
func (n *Nodes) SetDesired(id int64, configs map[string][]byte, files []string) {
	sums := map[string]string{}
	for _, abs := range files {
		rel, err := filepath.Rel(n.dataDir, abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		if sum := n.fileSum(abs); sum != "" {
			sums[filepath.ToSlash(rel)] = sum
		}
	}
	h := sha256.New()
	for _, k := range slices.Sorted(maps.Keys(configs)) {
		h.Write([]byte(k + "\x00"))
		h.Write(configs[k])
		h.Write([]byte{0})
	}
	for _, k := range slices.Sorted(maps.Keys(sums)) {
		h.Write([]byte(k + "=" + sums[k] + "\x00"))
	}
	hash := hex.EncodeToString(h.Sum(nil))[:16]
	n.mu.Lock()
	s := n.state(id)
	changed := s.hash != hash
	s.hash, s.configs, s.files, s.ready = hash, configs, sums, true
	n.mu.Unlock()
	if changed {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
}

func (n *Nodes) fileSum(abs string) string {
	st, err := os.Stat(abs)
	if err != nil || !st.Mode().IsRegular() {
		return ""
	}
	n.mu.Lock()
	c, ok := n.sums[abs]
	n.mu.Unlock()
	if ok && c.size == st.Size() && c.mod.Equal(st.ModTime()) {
		return c.sum
	}
	f, err := os.Open(abs)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	sum := hex.EncodeToString(h.Sum(nil))
	n.mu.Lock()
	n.sums[abs] = fileSum{size: st.Size(), mod: st.ModTime(), sum: sum}
	n.mu.Unlock()
	return sum
}

// FilePath — абсолютный путь файла, если этот файл узлу положен.
func (n *Nodes) FilePath(id int64, rel string) (string, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	s := n.states[id]
	if s == nil {
		return "", false
	}
	if _, ok := s.files[rel]; !ok {
		return "", false
	}
	return filepath.Join(n.dataDir, filepath.FromSlash(rel)), true
}

// XrayAPI — адрес API Xray, который сообщил узел ("" — ещё не приходил).
func (n *Nodes) XrayAPI(id int64) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if s := n.states[id]; s != nil {
		return s.xrayAPI
	}
	return ""
}

// Sync обрабатывает приход агента: забирает отчёт и отдаёт желаемое
// состояние. Если отдавать нечего, ждёт изменений до MaxWait.
func (n *Nodes) Sync(ctx context.Context, node store.Node, req nodeproto.SyncRequest) nodeproto.SyncResponse {
	n.mu.Lock()
	s := n.state(node.ID)
	s.traffic = append(s.traffic, req.Traffic...)
	if req.Online != nil {
		s.online = req.Online
	}
	s.health, s.version, s.applied, s.applyErr = req.Health, req.AgentVersion, req.Applied, req.ApplyError
	newAPI := req.XrayAPI != s.xrayAPI
	s.xrayAPI = req.XrayAPI
	wasDown := s.down
	s.seen, s.down = n.now(), false
	n.mu.Unlock()
	n.store.TouchNode(ctx, node.ID, n.now())
	if wasDown {
		n.events.Add(ctx, EventNodeUp, "info", 0, "", map[string]any{"node": node.Name})
	}
	if newAPI {
		n.changed()
	}

	wait := time.NewTimer(nodeproto.MaxWait * time.Second)
	defer wait.Stop()
	for {
		n.mu.Lock()
		ready := s.ready
		resp := nodeproto.SyncResponse{Hash: s.hash, Files: maps.Clone(s.files), Commands: s.commands}
		if ready && s.hash != req.Applied {
			resp.Configs = s.configs
		}
		filesMissing := false
		for k, v := range s.files {
			if req.Files[k] != v {
				filesMissing = true
			}
		}
		send := req.Fresh || len(s.commands) > 0 || (ready && (s.hash != req.Applied || filesMissing))
		if send {
			s.commands = nil
		}
		n.mu.Unlock()
		if send {
			return resp
		}
		select {
		case <-ctx.Done():
			return resp
		case <-wait.C:
			return resp
		case <-s.wake:
		}
	}
}

// Command ставит команду в очередь узла и будит его sync.
func (n *Nodes) Command(id int64, cmd string) {
	n.mu.Lock()
	s := n.state(id)
	if !slices.Contains(s.commands, cmd) {
		s.commands = append(s.commands, cmd)
	}
	n.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// TakeTraffic забирает накопленный трафик всех узлов.
func (n *Nodes) TakeTraffic() []agent.TrafficDelta {
	n.mu.Lock()
	defer n.mu.Unlock()
	var all []agent.TrafficDelta
	for _, s := range n.states {
		all = append(all, s.traffic...)
		s.traffic = nil
	}
	return all
}

// Online — кто подключён к узлам, которые сейчас на связи.
func (n *Nodes) Online() map[string]agent.OnlineEntry {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := map[string]agent.OnlineEntry{}
	for _, s := range n.states {
		if n.now().Sub(s.seen) > nodeOffline {
			continue
		}
		for k, e := range s.online {
			cur := out[k]
			cur.Email = e.Email
			cur.Connections += e.Connections
			for _, ip := range e.IPs {
				if !slices.Contains(cur.IPs, ip) {
					cur.IPs = append(cur.IPs, ip)
				}
			}
			out[k] = cur
		}
	}
	return out
}

// NodeStatus — узел для вкладки «Серверы».
type NodeStatus struct {
	store.Node
	Online       bool              `json:"online"`
	AgentVersion string            `json:"agentVersion,omitempty"`
	Health       *agent.NodeHealth `json:"health,omitempty"`
	InSync       bool              `json:"inSync"`
	ApplyError   string            `json:"applyError,omitempty"`
	Users        int               `json:"users"` // сколько пользователей подключено
}

func (n *Nodes) List(ctx context.Context) ([]NodeStatus, error) {
	list, err := n.store.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]NodeStatus, 0, len(list))
	for _, node := range list {
		ns := NodeStatus{Node: node}
		if s := n.states[node.ID]; s != nil && !s.seen.IsZero() {
			ns.Online = n.now().Sub(s.seen) <= nodeOffline
			ns.AgentVersion = s.version
			h := s.health
			ns.Health = &h
			ns.InSync = s.ready && s.applied == s.hash
			ns.ApplyError = s.applyErr
			ns.Users = len(s.online)
		}
		out = append(out, ns)
	}
	return out, nil
}

// RunWatch сообщает, когда узел пропал со связи.
func (n *Nodes) RunWatch(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		list, err := n.store.ListNodes(ctx)
		if err != nil {
			continue
		}
		for _, node := range list {
			n.mu.Lock()
			s := n.states[node.ID]
			lost := s != nil && !s.seen.IsZero() && !s.down && n.now().Sub(s.seen) > nodeOffline
			if lost {
				s.down = true
			}
			n.mu.Unlock()
			if lost {
				n.events.Add(ctx, EventNodeDown, "danger", 0, "", map[string]any{"node": node.Name})
			}
		}
	}
}

// joinData — то, что узнаёт агент при установке (зашито в команду).
type joinData struct {
	URL   string `json:"url"`   // https://host:port/<secret>
	Token string `json:"token"` // токен узла
	Pin   string `json:"pin"`   // SHA-256 сертификата панели (hex), если он самоподписанный
}

// JoinCode — строка для команды установки агента.
func JoinCode(url, token, pin string) string {
	b, _ := json.Marshal(joinData{URL: url, Token: token, Pin: pin})
	return base64.RawURLEncoding.EncodeToString(b)
}

// MergeOnline добавляет к m тех, кто подключён к узлам.
func MergeOnline(m map[string]agent.OnlineEntry, nodes *Nodes) map[string]agent.OnlineEntry {
	if nodes == nil {
		return m
	}
	if m == nil {
		m = map[string]agent.OnlineEntry{}
	}
	for k, e := range nodes.Online() {
		cur := m[k]
		cur.Email = e.Email
		cur.Connections += e.Connections
		for _, ip := range e.IPs {
			if !slices.Contains(cur.IPs, ip) {
				cur.IPs = append(cur.IPs, ip)
			}
		}
		m[k] = cur
	}
	return m
}
