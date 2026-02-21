package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	_ "modernc.org/sqlite"
)

// NotificationStatus 通知处理状态
type NotificationStatus string

const (
	StatusPending      NotificationStatus = "pending"       // 待处理（新通知）
	StatusReplied      NotificationStatus = "replied"       // 已回复
	StatusSkipped      NotificationStatus = "skipped"       // 已跳过（不需要回复）
	StatusRetry        NotificationStatus = "retry"         // 待重试（上次超时/报错）
	StatusDeletedCheck NotificationStatus = "deleted_check" // 待二次确认（首次判断为已删除）
)

// NotificationRecord 通知记录
type NotificationRecord struct {
	ID              string             `json:"id"`
	Status          NotificationStatus `json:"status"`
	RetryCount      int                `json:"retry_count"`
	FeedID          string             `json:"feed_id"`
	XsecToken       string             `json:"xsec_token"`
	CommentID       string             `json:"comment_id"`
	ParentCommentID string             `json:"parent_comment_id"`
	CommentContent  string             `json:"comment_content"`
	UserID          string             `json:"user_id"`
	UserNickname    string             `json:"user_nickname"`
	NoteTitle       string             `json:"note_title"`
	RelationType    string             `json:"relation_type"`
	NotifTimeUnix   int64              `json:"notif_time_unix"`
	ReplyContent    string             `json:"reply_content"`
	UpdatedAt       int64              `json:"updated_at"`
	CreatedAt       int64              `json:"created_at"`
}

// NotificationStore 通知状态存储
type NotificationStore struct {
	db *sql.DB
	mu sync.Mutex
}

var globalStore *NotificationStore
var storeOnce sync.Once

// GetNotificationStore 获取全局通知存储实例（单例）
func GetNotificationStore() (*NotificationStore, error) {
	var initErr error
	storeOnce.Do(func() {
		dbPath := getDBPath()
		if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
			initErr = fmt.Errorf("创建数据库目录失败: %w", err)
			return
		}
		store, err := newNotificationStore(dbPath)
		if err != nil {
			initErr = err
			return
		}
		globalStore = store
		logrus.Infof("通知状态数据库已初始化: %s", dbPath)
	})
	if initErr != nil {
		return nil, initErr
	}
	return globalStore, nil
}

func getDBPath() string {
	// 与二进制文件同目录
	exe, err := os.Executable()
	if err != nil {
		return "notifications.db"
	}
	return filepath.Join(filepath.Dir(exe), "notifications.db")
}

func newNotificationStore(dbPath string) (*NotificationStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	db.SetMaxOpenConns(1) // SQLite 单连接避免锁竞争

	store := &NotificationStore{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("数据库迁移失败: %w", err)
	}

	return store, nil
}

func (s *NotificationStore) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS notifications (
			id               TEXT    PRIMARY KEY,
			status           TEXT    NOT NULL DEFAULT 'pending',
			retry_count      INTEGER NOT NULL DEFAULT 0,
			feed_id          TEXT    NOT NULL DEFAULT '',
			xsec_token       TEXT    NOT NULL DEFAULT '',
			comment_id       TEXT    NOT NULL DEFAULT '',
			parent_comment_id TEXT   NOT NULL DEFAULT '',
			comment_content  TEXT    NOT NULL DEFAULT '',
			user_id          TEXT    NOT NULL DEFAULT '',
			user_nickname    TEXT    NOT NULL DEFAULT '',
			note_title       TEXT    NOT NULL DEFAULT '',
			relation_type    TEXT    NOT NULL DEFAULT '',
			notif_time_unix  INTEGER NOT NULL DEFAULT 0,
			reply_content    TEXT    NOT NULL DEFAULT '',
			updated_at       INTEGER NOT NULL DEFAULT 0,
			created_at       INTEGER NOT NULL DEFAULT 0
		);

		CREATE INDEX IF NOT EXISTS idx_status ON notifications(status);
		CREATE INDEX IF NOT EXISTS idx_notif_time ON notifications(notif_time_unix);
		CREATE INDEX IF NOT EXISTS idx_updated_at ON notifications(updated_at);

		CREATE TABLE IF NOT EXISTS meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
	`)
	return err
}

// UpsertNotifications 批量插入新通知（已存在的跳过，不覆盖已有状态）
func (s *NotificationStore) UpsertNotifications(records []NotificationRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO notifications
		(id, status, feed_id, xsec_token, comment_id, parent_comment_id,
		 comment_content, user_id, user_nickname, note_title, relation_type,
		 notif_time_unix, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().Unix()
	for _, r := range records {
		if _, err := stmt.Exec(
			r.ID, string(StatusPending), r.FeedID, r.XsecToken,
			r.CommentID, r.ParentCommentID, r.CommentContent,
			r.UserID, r.UserNickname, r.NoteTitle, r.RelationType,
			r.NotifTimeUnix, now, now,
		); err != nil {
			return fmt.Errorf("插入通知 %s 失败: %w", r.ID, err)
		}
	}

	return tx.Commit()
}

// MarkResult 更新单条通知的处理结果
func (s *NotificationStore) MarkResult(id string, status NotificationStatus, replyContent string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	var result sql.Result
	var err error

	if status == StatusRetry {
		// 重试：累加 retry_count
		result, err = s.db.Exec(`
			UPDATE notifications
			SET status=?, reply_content=?, updated_at=?, retry_count=retry_count+1
			WHERE id=?
		`, string(status), replyContent, now, id)
	} else {
		result, err = s.db.Exec(`
			UPDATE notifications
			SET status=?, reply_content=?, updated_at=?
			WHERE id=?
		`, string(status), replyContent, now, id)
	}

	if err != nil {
		return fmt.Errorf("更新通知 %s 状态失败: %w", id, err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		// 通知不在 DB 中（可能是旧数据），直接插入
		_, err = s.db.Exec(`
			INSERT INTO notifications (id, status, reply_content, updated_at, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, id, string(status), replyContent, now, now)
		if err != nil {
			return fmt.Errorf("插入新通知 %s 失败: %w", id, err)
		}
	}

	return nil
}

// GetPendingIDs 获取所有待处理状态的通知 ID 集合（pending/retry/deleted_check）
func (s *NotificationStore) GetPendingIDs() (map[string]NotificationStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
		SELECT id, status FROM notifications
		WHERE status IN ('pending', 'retry', 'deleted_check')
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]NotificationStatus)
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			return nil, err
		}
		result[id] = NotificationStatus(status)
	}
	return result, rows.Err()
}

// GetProcessedIDs 获取所有已完成状态的通知 ID 集合（replied/skipped）
func (s *NotificationStore) GetProcessedIDs() (map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
		SELECT id FROM notifications WHERE status IN ('replied', 'skipped')
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = true
	}
	return result, rows.Err()
}

// GetRetryIDs 获取 retry 状态的通知 ID 集合
func (s *NotificationStore) GetRetryIDs() (map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT id FROM notifications WHERE status='retry'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = true
	}
	return result, rows.Err()
}

// GetDeletedCheckIDs 获取 deleted_check 状态的通知 ID 集合
func (s *NotificationStore) GetDeletedCheckIDs() (map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT id FROM notifications WHERE status='deleted_check'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = true
	}
	return result, rows.Err()
}

// GetLastFetchTime 获取上次成功拉取通知的最新时间戳（秒）
func (s *NotificationStore) GetLastFetchTime() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var val string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key='last_fetch_time'`).Scan(&val)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	var t int64
	fmt.Sscanf(val, "%d", &t)
	return t, nil
}

// SetLastFetchTime 更新上次成功拉取通知的最新时间戳
func (s *NotificationStore) SetLastFetchTime(unixSec int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO meta(key, value) VALUES('last_fetch_time', ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value
	`, fmt.Sprintf("%d", unixSec))
	return err
}

// GetRecord 获取单条通知记录
func (s *NotificationStore) GetRecord(id string) (*NotificationRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRow(`
		SELECT id, status, retry_count, feed_id, xsec_token, comment_id,
		       parent_comment_id, comment_content, user_id, user_nickname,
		       note_title, relation_type, notif_time_unix, reply_content,
		       updated_at, created_at
		FROM notifications WHERE id=?
	`, id)

	r := &NotificationRecord{}
	var status string
	err := row.Scan(
		&r.ID, &status, &r.RetryCount, &r.FeedID, &r.XsecToken,
		&r.CommentID, &r.ParentCommentID, &r.CommentContent,
		&r.UserID, &r.UserNickname, &r.NoteTitle, &r.RelationType,
		&r.NotifTimeUnix, &r.ReplyContent, &r.UpdatedAt, &r.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.Status = NotificationStatus(status)
	return r, nil
}

// GetPendingRecords 获取所有待处理状态（pending/retry/deleted_check）的完整记录，按时间倒序
func (s *NotificationStore) GetPendingRecords() ([]NotificationRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
		SELECT id, status, retry_count, feed_id, xsec_token, comment_id,
		       parent_comment_id, comment_content, user_id, user_nickname,
		       note_title, relation_type, notif_time_unix, reply_content,
		       updated_at, created_at
		FROM notifications
		WHERE status IN ('pending', 'retry', 'deleted_check')
		ORDER BY notif_time_unix DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []NotificationRecord
	for rows.Next() {
		r := NotificationRecord{}
		var status string
		if err := rows.Scan(
			&r.ID, &status, &r.RetryCount, &r.FeedID, &r.XsecToken,
			&r.CommentID, &r.ParentCommentID, &r.CommentContent,
			&r.UserID, &r.UserNickname, &r.NoteTitle, &r.RelationType,
			&r.NotifTimeUnix, &r.ReplyContent, &r.UpdatedAt, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		r.Status = NotificationStatus(status)
		result = append(result, r)
	}
	return result, rows.Err()
}

// AutoSkipExcessiveRetries 将重试次数超过上限的通知自动标记为 skipped
func (s *NotificationStore) AutoSkipExcessiveRetries(maxRetries int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	result, err := s.db.Exec(`
		UPDATE notifications
		SET status='skipped', reply_content='自动跳过：重试次数超过上限', updated_at=?
		WHERE status='retry' AND retry_count >= ?
	`, now, maxRetries)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// Stats 返回各状态的通知数量统计
func (s *NotificationStore) Stats() (map[string]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`SELECT status, COUNT(*) FROM notifications GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		result[status] = count
	}
	return result, rows.Err()
}

// Close 关闭数据库连接
func (s *NotificationStore) Close() error {
	return s.db.Close()
}
