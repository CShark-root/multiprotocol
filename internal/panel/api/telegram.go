package api

import (
	"net/http"
	"time"

	"panel/internal/panel/service"
)

type telegramDTO struct {
	HasToken          bool      `json:"hasToken"`
	ChatID            int64     `json:"chatId"`
	ChatName          string    `json:"chatName"`
	Events            []string  `json:"events"`
	AllEvents         []string  `json:"allEvents"`
	DailyBackup       bool      `json:"dailyBackup"`
	Control           bool      `json:"control"`
	HasBackupPassword bool      `json:"hasBackupPassword"`
	LastBackup        time.Time `json:"lastBackup"`
	LastError         string    `json:"lastError"`
}

func toTelegramDTO(s service.TelegramSettings) telegramDTO {
	if s.Events == nil {
		s.Events = []string{}
	}
	return telegramDTO{
		HasToken: s.Token != "", ChatID: s.ChatID, ChatName: s.ChatName, Events: s.Events, AllEvents: service.NotifyEvents,
		DailyBackup: s.DailyBackup, Control: s.Control, HasBackupPassword: s.BackupPassword != "", LastBackup: s.LastBackup, LastError: s.LastError,
	}
}

func (s *server) getTelegram(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, toTelegramDTO(s.Telegram.Get(r.Context())))
}

func (s *server) saveTelegram(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token          string   `json:"token"`
		ChatID         int64    `json:"chatId"`
		ChatName       string   `json:"chatName"`
		Events         []string `json:"events"`
		DailyBackup    bool     `json:"dailyBackup"`
		Control        bool     `json:"control"`
		BackupPassword string   `json:"backupPassword"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	st, err := s.Telegram.Save(r.Context(), adminFrom(r.Context()).ID, service.TelegramSettings{
		Token: in.Token, ChatID: in.ChatID, ChatName: in.ChatName, Events: in.Events, DailyBackup: in.DailyBackup, Control: in.Control, BackupPassword: in.BackupPassword,
	})
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toTelegramDTO(st))
}

func (s *server) findTelegramChat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	chat, err := s.Telegram.FindChat(r.Context(), in.Token)
	if err != nil {
		s.upstreamError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, chat)
}

func (s *server) testTelegram(w http.ResponseWriter, r *http.Request) {
	if err := s.Telegram.Test(r.Context()); err != nil {
		s.upstreamError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) telegramBackup(w http.ResponseWriter, r *http.Request) {
	if err := s.Telegram.BackupNow(r.Context()); err != nil {
		s.upstreamError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
