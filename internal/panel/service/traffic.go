package service

import (
	"context"
	"maps"
	"time"

	"panel/internal/panel/store"
)

const TrafficSettingsKey = "traffic"

type TrafficSettings struct {
	Detailed bool `json:"detailed"`
}

type UserRate struct {
	UpBps   int64 `json:"upBps"`
	DownBps int64 `json:"downBps"`
}

func TrafficSettingsOf(ctx context.Context, st *store.Store) TrafficSettings {
	var settings TrafficSettings
	st.GetSetting(ctx, TrafficSettingsKey, &settings)
	return settings
}

func (s *Syncer) SetDetailedTraffic(enabled bool) { s.detailEnabled.Store(enabled) }

func (s *Syncer) updateRates(amounts map[int64]store.TrafficAmount, now time.Time) {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	seconds := now.Sub(s.lastSampleAt).Seconds()
	s.lastSampleAt = now
	s.lastRateAt = now
	rates := make(map[int64]UserRate, len(amounts))
	if seconds > 0 && seconds < 5*60 {
		for id, amount := range amounts {
			rates[id] = UserRate{UpBps: int64(float64(amount.Up) / seconds), DownBps: int64(float64(amount.Down) / seconds)}
		}
	}
	s.rates = rates
}

func (s *Syncer) mergeRates(remote map[int64]UserRate) {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	for id, rate := range remote {
		total := s.rates[id]
		total.UpBps += rate.UpBps
		total.DownBps += rate.DownBps
		s.rates[id] = total
	}
}

func (s *Syncer) Rates() map[int64]UserRate {
	s.rateMu.RLock()
	defer s.rateMu.RUnlock()
	if s.now != nil && !s.lastRateAt.IsZero() && s.now().Sub(s.lastRateAt) > 15*time.Second {
		return map[int64]UserRate{}
	}
	return maps.Clone(s.rates)
}
