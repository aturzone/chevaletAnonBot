package config

import "testing"

// TestBackupChatID locks BACKUP_CHAT_ID's parsing: optional (off, 0, by
// default), accepts any valid int64 — including a negative channel/supergroup
// id, unlike the old ADMINS-only BACKUP_ADMIN_ID this replaced — and rejects
// non-integer syntax at startup (a typo must not silently arm a nightly
// full-DB send to garbage).
func TestBackupChatID(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("BACKUP_CHAT_ID", "")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v; want nil", err)
	}
	if c.BackupChatID != 0 {
		t.Errorf("BackupChatID with BACKUP_CHAT_ID unset = %d; want 0 (feature off)", c.BackupChatID)
	}

	setRequiredEnv(t)
	t.Setenv("BACKUP_CHAT_ID", "1000000002") // a personal chat/admin DM
	c, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v; want nil", err)
	}
	if c.BackupChatID != 1000000002 {
		t.Errorf("BackupChatID = %d; want 1000000002", c.BackupChatID)
	}

	setRequiredEnv(t)
	t.Setenv("BACKUP_CHAT_ID", "-1001000000001") // a private channel/supergroup id
	c, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v; want nil", err)
	}
	if c.BackupChatID != -1001000000001 {
		t.Errorf("BackupChatID = %d; want -1001000000001 (channel ids must be accepted)", c.BackupChatID)
	}

	setRequiredEnv(t)
	t.Setenv("BACKUP_CHAT_ID", "not-an-int")
	if _, err := Load(); err == nil {
		t.Error("Load() with BACKUP_CHAT_ID=not-an-int: want an error, got nil")
	}
}
