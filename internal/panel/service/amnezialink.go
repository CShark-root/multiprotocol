package service

import (
	"context"

	"panel/internal/amnezia"
	"panel/internal/panel/store"
)

// AmneziaClients — то, что синхронизации нужно от помощника Amnezia.
type AmneziaClients interface {
	Status(ctx context.Context) (amnezia.Status, error)
	SetPaused(ctx context.Context, p amnezia.PauseParams) (amnezia.PauseResult, error)
}

// SetAmnezia включает связку клиентов AmneziaWG с пользователями панели.
// Вызывать до Run.
func (s *Syncer) SetAmnezia(a AmneziaClients) { s.amnezia = a }

// collectAmnezia добавляет пользователям трафик их клиентов AmneziaWG.
// Счётчики интерфейса растут с его запуска; меньше прошлого — значит,
// контейнер перезапускался, и считается всё текущее значение.
func (s *Syncer) collectAmnezia(ctx context.Context) error {
	s.amneziaSeen = nil
	if s.amnezia == nil {
		return nil
	}
	st, err := s.amnezia.Status(ctx)
	if err != nil || !st.Docker {
		return nil // помощника нет или Docker молчит — пропускаем этот круг
	}
	s.amneziaSeen = &st
	links, err := s.store.ListAmneziaLinks(ctx)
	if err != nil {
		return err
	}
	linked := map[[2]string]bool{}
	for _, l := range links {
		linked[[2]string{l.Container, l.Key}] = true
	}
	clients := map[[2]string]amnezia.Client{}
	// Непривязанные клиенты (роутеры и т. п.) идут в общий трафик сервера.
	// Их прошлые счётчики помним только в памяти: после перезапуска панели
	// первый круг лишь запоминает значения.
	var serverBytes int64
	seen := map[[2]string][2]int64{}
	for _, c := range st.Containers {
		for _, cl := range c.Clients {
			k := [2]string{c.Name, cl.Key}
			clients[k] = cl
			if linked[k] {
				continue
			}
			if prev, ok := s.amneziaFree[k]; ok {
				serverBytes += growth(prev[0], cl.RxBytes) + growth(prev[1], cl.TxBytes)
			}
			seen[k] = [2]int64{cl.RxBytes, cl.TxBytes}
		}
	}
	s.amneziaFree = seen
	if serverBytes > 0 {
		if err := s.store.AddServerTraffic(ctx, serverBytes, s.now()); err != nil {
			return err
		}
	}
	byUser := map[int64]int64{}
	var changed []store.AmneziaLink
	for _, l := range links {
		cl, ok := clients[[2]string{l.Container, l.Key}]
		if !ok {
			continue // клиент выключен или удалён в приложении
		}
		if l.Rx >= 0 && l.Tx >= 0 {
			byUser[l.UserID] += growth(l.Rx, cl.RxBytes) + growth(l.Tx, cl.TxBytes)
		}
		if l.Rx != cl.RxBytes || l.Tx != cl.TxBytes {
			l.Rx, l.Tx = cl.RxBytes, cl.TxBytes
			changed = append(changed, l)
		}
	}
	for id, b := range byUser {
		if b == 0 {
			delete(byUser, id)
		}
	}
	if len(byUser) > 0 {
		if err := s.store.AddTraffic(ctx, byUser, s.now()); err != nil {
			return err
		}
	}
	return s.store.SetAmneziaCounters(ctx, changed)
}

func growth(prev, cur int64) int64 {
	if cur >= prev {
		return cur - prev
	}
	return cur
}

// applyAmnezia держит выключенными клиентов тех пользователей, у кого
// доступа сейчас нет. Обходит все управляемые работающие контейнеры, чтобы
// вернуть и тех, кого отвязали.
func (s *Syncer) applyAmnezia(ctx context.Context, statuses map[int64]Status) {
	if s.amnezia == nil || s.amneziaSeen == nil {
		return
	}
	links, err := s.store.ListAmneziaLinks(ctx)
	if err != nil {
		return
	}
	paused := map[string][]string{}
	for _, l := range links {
		if st, ok := statuses[l.UserID]; ok && st != StatusActive {
			paused[l.Container] = append(paused[l.Container], l.Key)
		}
	}
	for _, c := range s.amneziaSeen.Containers {
		if !c.Managed || c.State != "running" {
			continue
		}
		keys := paused[c.Name]
		if keys == nil {
			keys = []string{}
		}
		if _, err := s.amnezia.SetPaused(ctx, amnezia.PauseParams{Container: c.Name, Keys: keys}); err != nil {
			s.log.Warn("amnezia paused clients", "container", c.Name, "err", err)
		}
	}
}
