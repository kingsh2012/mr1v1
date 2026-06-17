package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type User struct {
	OpenID    string
	SteamID   string
	Nickname  string
	AvatarURL string
}

type LegacyPlayer struct {
	SteamID string `json:"steam_id"`
	Name    string `json:"name"`
}

type Store struct {
	pool *pgxpool.Pool
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS wx_users (
			openid     TEXT PRIMARY KEY,
			steam_id   TEXT NOT NULL DEFAULT '',
			nickname   TEXT NOT NULL DEFAULT '',
			avatar_url TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE TABLE IF NOT EXISTS wx_sessions (
			token      TEXT PRIMARY KEY,
			openid     TEXT NOT NULL REFERENCES wx_users(openid) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE TABLE IF NOT EXISTS wx_rooms (
			id              TEXT PRIMARY KEY,
			title           TEXT NOT NULL,
			creator_openid  TEXT NOT NULL REFERENCES wx_users(openid),
			joiner_openid   TEXT REFERENCES wx_users(openid),
			password        TEXT NOT NULL DEFAULT '',
			status          TEXT NOT NULL DEFAULT 'waiting',
			server_addr     TEXT NOT NULL DEFAULT '',
			match_id        TEXT NOT NULL DEFAULT '',
			created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
		);
	`)
	return err
}

func (s *Store) CreateSession(ctx context.Context, token, openid string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO wx_sessions (token, openid) VALUES ($1, $2)
		ON CONFLICT (token) DO NOTHING
	`, token, openid)
	return err
}

func (s *Store) GetOpenIDByToken(ctx context.Context, token string) (string, bool) {
	var openid string
	err := s.pool.QueryRow(ctx, `SELECT openid FROM wx_sessions WHERE token = $1`, token).Scan(&openid)
	if err != nil {
		return "", false
	}
	return openid, true
}

// UpsertUser 登录时建档，已存在则仅更新 updated_at
func (s *Store) UpsertUser(ctx context.Context, openid string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO wx_users (openid) VALUES ($1)
		ON CONFLICT (openid) DO UPDATE SET updated_at = now()
	`, openid)
	return err
}

func (s *Store) GetUser(ctx context.Context, openid string) (*User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		SELECT openid, steam_id, nickname, avatar_url FROM wx_users WHERE openid = $1
	`, openid).Scan(&u.OpenID, &u.SteamID, &u.Nickname, &u.AvatarURL)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return &u, err
}

func (s *Store) UpdateSteamID(ctx context.Context, openid, steamID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE wx_users SET steam_id = $2, updated_at = now() WHERE openid = $1
	`, openid, steamID)
	return err
}

// ── Rooms ──────────────────────────────────────────────────────────────────

type Room struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	CreatorOpenID string `json:"creator_openid"`
	CreatorName   string `json:"creator_name"`
	JoinerOpenID  string `json:"joiner_openid,omitempty"`
	JoinerName    string `json:"joiner_name,omitempty"`
	Locked        bool   `json:"locked"`
	Status        string `json:"status"` // waiting|ready|matched
}

func (s *Store) CreateRoom(ctx context.Context, id, title, creatorOpenID, password string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO wx_rooms (id, title, creator_openid, password) VALUES ($1, $2, $3, $4)
	`, id, title, creatorOpenID, password)
	return err
}

func (s *Store) ListRooms(ctx context.Context) ([]Room, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.title, r.creator_openid, COALESCE(u.nickname,'') AS creator_name,
		       COALESCE(r.joiner_openid,'') AS joiner_openid,
		       COALESCE(j.nickname,'') AS joiner_name,
		       r.password != '' AS locked, r.status
		FROM wx_rooms r
		JOIN wx_users u ON u.openid = r.creator_openid
		LEFT JOIN wx_users j ON j.openid = r.joiner_openid
		WHERE r.status IN ('waiting','ready')
		ORDER BY r.created_at DESC
		LIMIT 50
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rooms []Room
	for rows.Next() {
		var rm Room
		if err := rows.Scan(&rm.ID, &rm.Title, &rm.CreatorOpenID, &rm.CreatorName,
			&rm.JoinerOpenID, &rm.JoinerName, &rm.Locked, &rm.Status); err != nil {
			return nil, err
		}
		rooms = append(rooms, rm)
	}
	return rooms, rows.Err()
}

func (s *Store) GetRoom(ctx context.Context, id string) (*Room, error) {
	var rm Room
	var password string
	err := s.pool.QueryRow(ctx, `
		SELECT r.id, r.title, r.creator_openid, COALESCE(u.nickname,''),
		       COALESCE(r.joiner_openid,''), COALESCE(j.nickname,''),
		       r.password, r.status
		FROM wx_rooms r
		JOIN wx_users u ON u.openid = r.creator_openid
		LEFT JOIN wx_users j ON j.openid = r.joiner_openid
		WHERE r.id = $1
	`, id).Scan(&rm.ID, &rm.Title, &rm.CreatorOpenID, &rm.CreatorName,
		&rm.JoinerOpenID, &rm.JoinerName, &password, &rm.Status)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rm.Locked = password != ""
	return &rm, nil
}

func (s *Store) GetRoomPassword(ctx context.Context, id string) (string, error) {
	var pw string
	err := s.pool.QueryRow(ctx, `SELECT password FROM wx_rooms WHERE id = $1`, id).Scan(&pw)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	return pw, err
}

func (s *Store) JoinRoom(ctx context.Context, id, joinerOpenID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE wx_rooms SET joiner_openid = $2, status = 'ready'
		WHERE id = $1 AND joiner_openid IS NULL AND status = 'waiting'
	`, id, joinerOpenID)
	return err
}

func (s *Store) LeaveRoom(ctx context.Context, id, openid string) error {
	// creator 离开 → 删除房间；joiner 离开 → 清空 joiner，回到 waiting
	_, err := s.pool.Exec(ctx, `
		DO $$
		DECLARE r wx_rooms%ROWTYPE;
		BEGIN
			SELECT * INTO r FROM wx_rooms WHERE id = $1;
			IF r.creator_openid = $2 THEN
				DELETE FROM wx_rooms WHERE id = $1;
			ELSIF r.joiner_openid = $2 THEN
				UPDATE wx_rooms SET joiner_openid = NULL, status = 'waiting' WHERE id = $1;
			END IF;
		END $$;
	`, id, openid)
	return err
}

func (s *Store) SetRoomMatched(ctx context.Context, id, matchID, serverAddr string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE wx_rooms SET status='matched', match_id=$2, server_addr=$3 WHERE id=$1
	`, id, matchID, serverAddr)
	return err
}

func (s *Store) DeleteRoom(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM wx_rooms WHERE id = $1`, id)
	return err
}

// ── Legacy Players ──────────────────────────────────────────────────────────

func (s *Store) SearchLegacyPlayers(ctx context.Context, keyword string) ([]LegacyPlayer, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT steam_id, name FROM legacy_players
		WHERE name ILIKE '%' || $1 || '%'
		ORDER BY name
		LIMIT 20
	`, keyword)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var players []LegacyPlayer
	for rows.Next() {
		var p LegacyPlayer
		if err := rows.Scan(&p.SteamID, &p.Name); err != nil {
			return nil, err
		}
		players = append(players, p)
	}
	return players, rows.Err()
}
