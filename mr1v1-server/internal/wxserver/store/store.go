package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mr1v1-server/internal/wxserver/namegen"
)

// ErrRoomNotJoinable 表示房间已经不满足"可加入"条件（已被别人抢先加入/已满/已删除）。
// JoinRoom 的 UPDATE 靠 WHERE 条件保证数据库层面只有一个并发请求能真正生效，
// 但调用方必须检查 RowsAffected，否则没抢到的请求会被误判成功。
var ErrRoomNotJoinable = errors.New("room not joinable")

type User struct {
	OpenID    string
	SteamID   string
	Nickname  string
	AvatarURL string
	Status    string // enabled|disabled，manager后台可以禁用账号
	CreatedAt time.Time
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
			status     TEXT NOT NULL DEFAULT 'enabled',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		ALTER TABLE wx_users ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'enabled';
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
			created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
			deleted_at      TIMESTAMPTZ
		);
		ALTER TABLE wx_rooms ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
		ALTER TABLE wx_rooms ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
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

// GetOpenIDByToken 顺带校验账号状态：被manager后台禁用的账号，已签发的session
// 立刻失效（下一次请求就会401），不用等用户重新登录才生效。
func (s *Store) GetOpenIDByToken(ctx context.Context, token string) (string, bool) {
	var openid string
	err := s.pool.QueryRow(ctx, `
		SELECT s.openid FROM wx_sessions s
		JOIN wx_users u ON u.openid = s.openid
		WHERE s.token = $1 AND u.status = 'enabled'
	`, token).Scan(&openid)
	if err != nil {
		return "", false
	}
	return openid, true
}

// UpsertUser 登录时建档，已存在则仅更新updated_at（不会覆盖已有nickname，
// 包括用户后来自己改过的）。首次建档时给一个随机中文昵称，新用户不至于显示空白。
func (s *Store) UpsertUser(ctx context.Context, openid string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO wx_users (openid, nickname) VALUES ($1, $2)
		ON CONFLICT (openid) DO UPDATE SET updated_at = now()
	`, openid, namegen.Generate())
	return err
}

func (s *Store) GetUser(ctx context.Context, openid string) (*User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		SELECT openid, steam_id, nickname, avatar_url, status, created_at FROM wx_users WHERE openid = $1
	`, openid).Scan(&u.OpenID, &u.SteamID, &u.Nickname, &u.AvatarURL, &u.Status, &u.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return &u, err
}

// ResolveSession 校验token是否有效，并区分"账号被禁用"和"token无效/账号已删除"两种情况——
// 前者要明确提示用户联系管理员，后者按普通的未登录处理就行。
func (s *Store) ResolveSession(ctx context.Context, token string) (openid string, disabled bool, ok bool) {
	var status string
	err := s.pool.QueryRow(ctx, `
		SELECT s.openid, u.status FROM wx_sessions s
		JOIN wx_users u ON u.openid = s.openid
		WHERE s.token = $1
	`, token).Scan(&openid, &status)
	if err != nil {
		return "", false, false
	}
	return openid, status != "enabled", true
}

func (s *Store) UpdateSteamID(ctx context.Context, openid, steamID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE wx_users SET steam_id = $2, updated_at = now() WHERE openid = $1
	`, openid, steamID)
	return err
}

func (s *Store) UpdateProfile(ctx context.Context, openid, avatarURL, nickname string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE wx_users SET avatar_url = $2, nickname = $3, updated_at = now() WHERE openid = $1
	`, openid, avatarURL, nickname)
	return err
}

// ── Rooms ──────────────────────────────────────────────────────────────────

type Room struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	CreatorOpenID string `json:"-"` // 不对外序列化：openid是用户隐私标识，房间列表现在游客可访问，不能裸传
	CreatorName   string `json:"creator_name"`
	CreatorAvatar string `json:"creator_avatar"`
	JoinerOpenID  string `json:"-"`
	JoinerName    string `json:"joiner_name,omitempty"`
	JoinerAvatar  string `json:"joiner_avatar,omitempty"`
	Locked        bool   `json:"locked"`
	Status        string `json:"status"`            // waiting|ready|matched
	IsMine        bool   `json:"is_mine,omitempty"` // 仅ListRooms根据当前登录用户算出来，游客/未关联到则为false
}

func (s *Store) HasActiveRoom(ctx context.Context, openid string) (bool, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM wx_rooms WHERE creator_openid = $1 AND status IN ('waiting','ready') AND deleted_at IS NULL
	`, openid).Scan(&count)
	return count > 0, err
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
		       COALESCE(u.avatar_url,'') AS creator_avatar,
		       COALESCE(r.joiner_openid,'') AS joiner_openid,
		       COALESCE(j.nickname,'') AS joiner_name,
		       COALESCE(j.avatar_url,'') AS joiner_avatar,
		       r.password != '' AS locked, r.status
		FROM wx_rooms r
		JOIN wx_users u ON u.openid = r.creator_openid
		LEFT JOIN wx_users j ON j.openid = r.joiner_openid
		WHERE r.status IN ('waiting','ready') AND r.deleted_at IS NULL
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
			&rm.CreatorAvatar, &rm.JoinerOpenID, &rm.JoinerName, &rm.JoinerAvatar,
			&rm.Locked, &rm.Status); err != nil {
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
		       COALESCE(u.avatar_url,''),
		       COALESCE(r.joiner_openid,''), COALESCE(j.nickname,''),
		       COALESCE(j.avatar_url,''),
		       r.password, r.status
		FROM wx_rooms r
		JOIN wx_users u ON u.openid = r.creator_openid
		LEFT JOIN wx_users j ON j.openid = r.joiner_openid
		WHERE r.id = $1 AND r.deleted_at IS NULL
	`, id).Scan(&rm.ID, &rm.Title, &rm.CreatorOpenID, &rm.CreatorName,
		&rm.CreatorAvatar, &rm.JoinerOpenID, &rm.JoinerName, &rm.JoinerAvatar,
		&password, &rm.Status)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rm.Locked = password != ""
	return &rm, nil
}

// GetRoomIDByMatchID 根据 manager-backend 的 match_id 反查房间 id，
// 用于比赛结束时（手动销毁/超时/异常停止/正常完赛）同步关闭对应房间。
func (s *Store) GetRoomIDByMatchID(ctx context.Context, matchID string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM wx_rooms WHERE match_id = $1 AND deleted_at IS NULL
	`, matchID).Scan(&id)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	return id, err
}

func (s *Store) GetRoomPassword(ctx context.Context, id string) (string, error) {
	var pw string
	err := s.pool.QueryRow(ctx, `SELECT password FROM wx_rooms WHERE id = $1 AND deleted_at IS NULL`, id).Scan(&pw)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	return pw, err
}

func (s *Store) JoinRoom(ctx context.Context, id, joinerOpenID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE wx_rooms SET joiner_openid = $2, status = 'ready', updated_at = now()
		WHERE id = $1 AND joiner_openid IS NULL AND status = 'waiting' AND deleted_at IS NULL
	`, id, joinerOpenID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// WHERE条件没匹配到任何行：房间已经被别人抢先加入/状态已变化，
		// 而不是真的加入成功——调用方之前没检查这个，会把"没抢到"误报成"加入成功"
		return ErrRoomNotJoinable
	}
	return nil
}

// LeaveRoom 仅处理 joiner 离开（清空 joiner，回到 waiting）。
// creator 离开房间不再走这个方法——显式关闭走 DeleteRoom，断线不销毁房间（见 hub.go）。
func (s *Store) LeaveRoom(ctx context.Context, id, openid string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE wx_rooms SET joiner_openid = NULL, status = 'waiting', updated_at = now()
		WHERE id = $1 AND joiner_openid = $2 AND deleted_at IS NULL
	`, id, openid)
	return err
}

func (s *Store) SetRoomMatched(ctx context.Context, id, matchID, serverAddr string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE wx_rooms SET status='matched', match_id=$2, server_addr=$3, updated_at = now() WHERE id=$1 AND deleted_at IS NULL
	`, id, matchID, serverAddr)
	return err
}

func (s *Store) DeleteRoom(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE wx_rooms SET deleted_at = now(), updated_at = now() WHERE id = $1`, id)
	return err
}

// StaleRoomIDs 返回长时间无状态变化的活跃房间 ID（status 仍是 waiting/ready，
// 超过 idleFor 没有任何更新）。调用方需结合内存中的连接状态（room.Manager）
// 二次过滤掉仍有人在线的房间，避免误杀正在等待对手的合法房间。
func (s *Store) StaleRoomIDs(ctx context.Context, idleFor time.Duration) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM wx_rooms
		WHERE status IN ('waiting','ready') AND deleted_at IS NULL
		  AND updated_at < now() - $1 * INTERVAL '1 second'
	`, idleFor.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
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
