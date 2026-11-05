package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"panel/internal/nodeproto"
	"panel/internal/panel/service"
	"panel/internal/panel/store"
)

// --- для агентов на других серверах ---

// nodeAuth пускает только агентов с действующим токеном. Чужим — такая же
// пустая 404, как на любой неизвестный путь, и ограничение попыток.
func (s *server) nodeAuth(next func(http.ResponseWriter, *http.Request, store.Node)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		decoy := func() { w.WriteHeader(http.StatusNotFound) }
		if s.Nodes == nil {
			decoy()
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		node, err := s.Nodes.Authenticate(r.Context(), token)
		if !ok || err != nil {
			if !s.nodeLimiter.allow(clientIP(r), time.Now()) {
				s.Logger.Warn("node auth rate limit hit", "ip", clientIP(r))
			}
			decoy()
			return
		}
		next(w, r, node)
	}
}

func (s *server) nodeSync(w http.ResponseWriter, r *http.Request, node store.Node) {
	var req nodeproto.SyncRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	// Долгий опрос: стандартный WriteTimeout сервера его не обрывает (60 с > 25 с).
	writeJSON(w, http.StatusOK, s.Nodes.Sync(r.Context(), node, req))
}

// nodeFile отдаёт узлу файл (гео-базу), который панель ему назначила.
func (s *server) nodeFile(w http.ResponseWriter, r *http.Request, node store.Node) {
	path, ok := s.Nodes.FilePath(node.ID, r.URL.Query().Get("path"))
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Now().Add(10 * time.Minute)) // гео-базы — десятки мегабайт
	io.Copy(w, f)
}

// --- для админа ---

func (s *server) listNodes(w http.ResponseWriter, r *http.Request) {
	if s.Nodes == nil {
		writeJSON(w, http.StatusOK, []service.NodeStatus{})
		return
	}
	list, err := s.Nodes.List(r.Context())
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type nodeInput struct {
	Name string `json:"name"`
	Host string `json:"host"`
}

// joinCommand — команда, которую админ запускает на новом сервере.
func (s *server) joinCommand(r *http.Request, token string) string {
	host := s.PublicHost
	if host == "" {
		host = r.Host
	}
	if h := r.Host; h != "" {
		host = h // адрес, по которому админ сейчас открыл панель, точно доступен
	}
	url := "https://" + host + s.prefix
	code := service.JoinCode(url, token, s.CertPin)
	return "curl -fsSL https://raw.githubusercontent.com/CShark-root/multiprotocol/main/install.sh | sudo NODE_JOIN=" + code + " bash"
}

func (s *server) createNode(w http.ResponseWriter, r *http.Request) {
	var in nodeInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if s.Nodes == nil {
		writeError(w, http.StatusNotImplemented, "unsupported", "no local agent")
		return
	}
	node, token, err := s.Nodes.Create(r.Context(), in.Name, in.Host)
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	s.Store.AddAudit(r.Context(), store.AuditEntry{AdminID: adminFrom(r.Context()).ID, Action: "node.create", TargetType: "node",
		TargetID: strconv.FormatInt(node.ID, 10), Diff: map[string]any{"name": node.Name, "host": node.Host}}, time.Now())
	writeJSON(w, http.StatusCreated, map[string]any{"node": node, "command": s.joinCommand(r, token)})
}

func nodeID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id, err == nil && id > 0
}

func (s *server) updateNode(w http.ResponseWriter, r *http.Request) {
	id, ok := nodeID(r)
	var in nodeInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if !ok || s.Nodes == nil {
		writeError(w, http.StatusNotFound, "not_found", "no such server")
		return
	}
	node, err := s.Nodes.Update(r.Context(), id, in.Name, in.Host)
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	s.Store.AddAudit(r.Context(), store.AuditEntry{AdminID: adminFrom(r.Context()).ID, Action: "node.update", TargetType: "node",
		TargetID: strconv.FormatInt(id, 10), Diff: map[string]any{"name": node.Name, "host": node.Host}}, time.Now())
	writeJSON(w, http.StatusOK, node)
}

// nodeToken выдаёт новый токен и команду установки (старый токен перестаёт работать).
func (s *server) nodeToken(w http.ResponseWriter, r *http.Request) {
	id, ok := nodeID(r)
	if !ok || s.Nodes == nil {
		writeError(w, http.StatusNotFound, "not_found", "no such server")
		return
	}
	token, err := s.Nodes.NewToken(r.Context(), id)
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	s.Store.AddAudit(r.Context(), store.AuditEntry{AdminID: adminFrom(r.Context()).ID, Action: "node.token", TargetType: "node",
		TargetID: strconv.FormatInt(id, 10)}, time.Now())
	writeJSON(w, http.StatusOK, map[string]any{"command": s.joinCommand(r, token)})
}

func (s *server) deleteNode(w http.ResponseWriter, r *http.Request) {
	id, ok := nodeID(r)
	if !ok || s.Nodes == nil {
		writeError(w, http.StatusNotFound, "not_found", "no such server")
		return
	}
	if err := s.Nodes.Delete(r.Context(), id); err != nil {
		s.handleError(w, r, err)
		return
	}
	s.Store.AddAudit(r.Context(), store.AuditEntry{AdminID: adminFrom(r.Context()).ID, Action: "node.delete", TargetType: "node",
		TargetID: strconv.FormatInt(id, 10)}, time.Now())
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) restartNode(w http.ResponseWriter, r *http.Request) {
	id, ok := nodeID(r)
	if !ok || s.Nodes == nil {
		writeError(w, http.StatusNotFound, "not_found", "no such server")
		return
	}
	s.Nodes.Command(id, nodeproto.CmdRestartCores)
	w.WriteHeader(http.StatusAccepted)
}
