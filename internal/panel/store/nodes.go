package store

import (
	"context"
	"database/sql"
	"time"
)

// Node — другой наш сервер с агентом.
type Node struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Host      string     `json:"host"` // адрес для ссылок клиентов
	CreatedAt time.Time  `json:"createdAt"`
	LastSeen  time.Time  `json:"lastSeen"`
	PaidUntil *time.Time `json:"paidUntil"` // до какого дня оплачен VPS
	Price     string     `json:"price"`
}

const nodeColumns = `id, name, host, created_at, last_seen, paid_until, price`

func scanNode(row scanner) (Node, error) {
	var n Node
	var created, seen, paid int64
	if err := row.Scan(&n.ID, &n.Name, &n.Host, &created, &seen, &paid, &n.Price); err != nil {
		return Node{}, err
	}
	n.CreatedAt = fromUnix(created)
	if seen > 0 {
		n.LastSeen = fromUnix(seen)
	}
	if paid > 0 {
		t := fromUnix(paid)
		n.PaidUntil = &t
	}
	return n, nil
}

func (s *Store) ListNodes(ctx context.Context) ([]Node, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+nodeColumns+` FROM nodes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Node{}
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) GetNode(ctx context.Context, id int64) (Node, error) {
	n, err := scanNode(s.db.QueryRowContext(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE id = ?`, id))
	return n, notFound(err)
}

// NodeByToken ищет узел по хэшу его токена.
func (s *Store) NodeByToken(ctx context.Context, tokenHash string) (Node, error) {
	n, err := scanNode(s.db.QueryRowContext(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE token_hash = ?`, tokenHash))
	return n, notFound(err)
}

func (s *Store) CreateNode(ctx context.Context, name, host, tokenHash string, now time.Time) (Node, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO nodes (name, host, token_hash, created_at) VALUES (?, ?, ?, ?)`,
		name, host, tokenHash, now.Unix())
	if err != nil {
		return Node{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Node{}, err
	}
	return s.GetNode(ctx, id)
}

func (s *Store) UpdateNode(ctx context.Context, id int64, name, host string) (Node, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE nodes SET name = ?, host = ? WHERE id = ?`, name, host, id)
	if err != nil {
		return Node{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Node{}, ErrNotFound
	}
	return s.GetNode(ctx, id)
}

func (s *Store) SetNodeToken(ctx context.Context, id int64, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE nodes SET token_hash = ? WHERE id = ?`, tokenHash, id)
	return err
}

func (s *Store) TouchNode(ctx context.Context, id int64, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE nodes SET last_seen = ? WHERE id = ?`, now.Unix(), id)
	return err
}

// DeleteNode удаляет узел вместе с его подключениями.
func (s *Store) DeleteNode(ctx context.Context, id int64) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM inbounds WHERE node_id = ?`, id); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM nodes WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// WithNodeHosts подставляет адрес узла в подключения других серверов, у
// которых свой адрес не задан: ссылки клиентов ведут на нужный сервер.
func (s *Store) WithNodeHosts(ctx context.Context, list []Inbound) []Inbound {
	nodes, err := s.ListNodes(ctx)
	if err != nil || len(nodes) == 0 {
		return list
	}
	host := map[int64]string{}
	for _, n := range nodes {
		host[n.ID] = n.Host
	}
	for i := range list {
		if list[i].NodeID > 0 && list[i].Address == "" {
			list[i].Address = host[list[i].NodeID]
		}
	}
	return list
}

// SetNodeBilling задаёт оплату VPS (paidUntil nil — не задано).
func (s *Store) SetNodeBilling(ctx context.Context, id int64, paidUntil *time.Time, price string) error {
	var p int64
	if paidUntil != nil {
		p = paidUntil.Unix()
	}
	res, err := s.db.ExecContext(ctx, `UPDATE nodes SET paid_until = ?, price = ? WHERE id = ?`, p, price, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
