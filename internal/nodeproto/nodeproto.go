// Package nodeproto — обмен между главной панелью и агентом на другом
// сервере. Агент сам приходит к панели (POST sync) по HTTPS на её секретный
// путь с токеном узла: на узле не нужен ни один открытый служебный порт.
// Панель держит запрос до 25 секунд, пока ей нечего сказать, так что новые
// настройки доходят до узла почти сразу.
package nodeproto

import "panel/internal/agent"

// Path — путь под секретным путём панели.
const Path = "/node-api/v1"

// MaxWait — сколько панель держит sync, если ничего не изменилось.
const MaxWait = 25

type SyncRequest struct {
	AgentVersion string                       `json:"agentVersion"`
	Health       agent.NodeHealth             `json:"health"`
	Traffic      []agent.TrafficDelta         `json:"traffic"`
	Online       map[string]agent.OnlineEntry `json:"online"`
	XrayAPI      string                       `json:"xrayApi"` // адрес API Xray на узле (127.0.0.1:порт)
	Applied      string                       `json:"applied"` // хэш последнего применённого состояния
	ApplyError   string                       `json:"applyError,omitempty"`
	// Files — какие файлы (путь относительно каталога данных → sha256) у агента уже есть.
	Files map[string]string `json:"files"`
	// Fresh: агент только запустился — панель отвечает сразу, без ожидания.
	Fresh bool `json:"fresh,omitempty"`
}

type SyncResponse struct {
	Hash string `json:"hash"`
	// Configs — конфиги ядер; пусто, если Hash совпадает с Applied.
	Configs map[string][]byte `json:"configs,omitempty"`
	// Files — какие файлы должны быть у агента (гео-базы). Недостающие он
	// скачивает GET file?path=…
	Files    map[string]string `json:"files"`
	Commands []string          `json:"commands,omitempty"`
}

// Команды узлу.
const (
	CmdRestartCores = "restart-cores"
)
