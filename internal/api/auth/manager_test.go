package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthManager_SetupLoginAndSession(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	assert.False(t, manager.IsInitialized())

	err = manager.Setup("admin", "password123")
	require.NoError(t, err)
	assert.True(t, manager.IsInitialized())

	username, ok := manager.Username()
	require.True(t, ok)
	assert.Equal(t, "admin", username)

	credPath := credentialPathForConfig(configFile)
	info, err := os.Stat(credPath)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	_, err = manager.Login("admin", "wrong-password", false)
	assert.ErrorIs(t, err, ErrInvalidCredentials)

	sessionID, err := manager.Login("admin", "password123", false)
	require.NoError(t, err)
	require.NotEmpty(t, sessionID)

	sessionUser, err := manager.AuthenticateSession(sessionID)
	require.NoError(t, err)
	assert.Equal(t, "admin", sessionUser)

	manager.Logout(sessionID)
	_, err = manager.AuthenticateSession(sessionID)
	assert.ErrorIs(t, err, ErrInvalidSession)
}

func TestAuthManager_SetupValidation(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	err = manager.Setup("", "password123")
	assert.ErrorIs(t, err, ErrInvalidUsername)

	err = manager.Setup("admin", "short")
	assert.ErrorIs(t, err, ErrWeakPassword)

	err = manager.Setup("admin", "password123")
	require.NoError(t, err)

	err = manager.Setup("another", "password123")
	assert.ErrorIs(t, err, ErrAuthAlreadySet)
}

func TestAuthManager_LoadMalformedFile(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	credPath := credentialPathForConfig(configFile)
	require.NoError(t, os.WriteFile(credPath, []byte("{not-json"), 0o600))

	_, err := NewAuthManager(configFile, time.Hour)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse auth credential file")
}

func TestAuthManager_SessionExpiry(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, 20*time.Millisecond)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	sessionID, err := manager.Login("admin", "password123", false)
	require.NoError(t, err)

	time.Sleep(40 * time.Millisecond)
	_, err = manager.AuthenticateSession(sessionID)
	assert.ErrorIs(t, err, ErrInvalidSession)
}

func TestAuthManager_LoadExistingCredentialFile(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	original, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, original.Setup("admin", "password123"))

	reloaded, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	assert.True(t, reloaded.IsInitialized())

	sessionID, err := reloaded.Login("admin", "password123", false)
	require.NoError(t, err)
	require.NotEmpty(t, sessionID)
}

func TestAuthManager_RememberedSessionPersistsAcrossReload(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	sessionID, err := manager.Login("admin", "password123", true)
	require.NoError(t, err)

	reloaded, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	username, err := reloaded.AuthenticateSession(sessionID)
	require.NoError(t, err)
	assert.Equal(t, "admin", username)
}

func TestAuthManager_NonRememberedSessionDoesNotPersistAcrossReload(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	sessionID, err := manager.Login("admin", "password123", false)
	require.NoError(t, err)

	reloaded, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	_, err = reloaded.AuthenticateSession(sessionID)
	assert.ErrorIs(t, err, ErrInvalidSession)
}

func TestAuthManager_RememberedSessionOutlivesEphemeralTTL(t *testing.T) {
	// Regression test for the "Remember me" login persistency bug: a
	// remember-me session must outlive an ephemeral (non-remembered)
	// session. Users selecting "Remember me" expect to stay logged in
	// across server restarts for a long window (weeks), not the same
	// short TTL as an ephemeral session. Before this fix, both paths used
	// the same sessionTTL — so a server restart after the TTL window
	// expired the session + prompted the user to log in again, despite
	// "Remember me" being selected.
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	// Fixed clock so we can fast-forward deterministically.
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	manager.nowFn = func() time.Time { return base }

	ephemeralID, err := manager.Login("admin", "password123", false)
	require.NoError(t, err)

	rememberedID, err := manager.Login("admin", "password123", true)
	require.NoError(t, err)

	// Both sessions are valid at T0.
	_, err = manager.AuthenticateSession(ephemeralID)
	require.NoError(t, err)
	_, err = manager.AuthenticateSession(rememberedID)
	require.NoError(t, err)

	// Fast-forward past the ephemeral TTL (1h). The ephemeral session
	// must expire; the remember-me session must STILL be valid.
	base = base.Add(2 * time.Hour)

	_, err = manager.AuthenticateSession(ephemeralID)
	assert.ErrorIs(t, err, ErrInvalidSession, "ephemeral session must expire after its TTL")

	_, err = manager.AuthenticateSession(rememberedID)
	assert.NoError(t, err, "remember-me session must outlive the ephemeral TTL")
}

func TestAuthManager_RememberedSessionSurvivesRestartPastEphemeralTTL(t *testing.T) {
	// Regression test for the user-reported symptom: "despite selecting
	// Remember me, I get prompted to login after server restarts." A
	// remember-me session must survive a server restart EVEN when the
	// restart happens past the ephemeral TTL window — because the
	// persistent session has a longer lifetime. Before the fix, the
	// persisted session carried the ephemeral TTL, so loadSessionsFromDisk
	// pruned it during the reload (the now.After(expiresAt) guard).
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	// Use real time as the base so loadSessionsFromDisk (which runs inside
	// NewAuthManager with time.Now before nowFn can be overridden) sees the
	// persisted session as non-expired during the reload below.
	base := time.Now()
	manager.nowFn = func() time.Time { return base }

	rememberedID, err := manager.Login("admin", "password123", true)
	require.NoError(t, err)

	// Simulate a server restart past the ephemeral TTL: fast-forward 2h,
	// then construct a fresh manager (loads sessions from disk). The
	// persisted session must still be valid because remember-me grants a
	// longer TTL.
	base = base.Add(2 * time.Hour)

	reloaded, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	reloaded.nowFn = func() time.Time { return base }

	_, err = reloaded.AuthenticateSession(rememberedID)
	assert.NoError(t, err, "remember-me session must survive a restart past the ephemeral TTL")
}

func TestAuthManager_LoadMalformedCredentialFields(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	credPath := credentialPathForConfig(configFile)
	payload := map[string]any{
		"version":  1,
		"username": "admin",
		// Missing hash/salt and argon2 params should fail load.
	}

	data, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(credPath, data, 0o600))

	_, err = NewAuthManager(configFile, time.Hour)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "argon2 parameters are required")
}

func TestAuthManager_LoadRepairsCredentialPermissions(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("windows permission bits are ACL-managed")
	}

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	credPath := credentialPathForConfig(configFile)
	require.NoError(t, os.Chmod(credPath, 0o644))

	reloaded, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	assert.True(t, reloaded.IsInitialized())

	info, err := os.Stat(credPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestAuthManager_LoadRejectsCredentialSymlink(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("windows symlink handling differs and does not use unix permission helper")
	}

	configDir := t.TempDir()
	configFile := filepath.Join(configDir, "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	credPath := credentialPathForConfig(configFile)
	targetPath := filepath.Join(configDir, "target.credentials.json")
	require.NoError(t, os.Rename(credPath, targetPath))
	require.NoError(t, os.Symlink(targetPath, credPath))

	_, err = NewAuthManager(configFile, time.Hour)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not be a symlink")
}

func TestAuthManager_SetupIgnoresPreexistingLegacyTmpSymlink(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("windows symlink handling differs and does not use unix permission helper")
	}

	configDir := t.TempDir()
	configFile := filepath.Join(configDir, "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)

	credPath := credentialPathForConfig(configFile)
	legacyTmpPath := credPath + ".tmp"
	targetPath := filepath.Join(configDir, "symlink-target.txt")
	require.NoError(t, os.WriteFile(targetPath, []byte("original"), 0o600))
	require.NoError(t, os.Symlink(targetPath, legacyTmpPath))

	require.NoError(t, manager.Setup("admin", "password123"))

	targetBytes, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	assert.Equal(t, "original", string(targetBytes))
}

func TestAuthManager_SessionCountIsBounded(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	firstSession, err := manager.Login("admin", "password123", false)
	require.NoError(t, err)

	for i := 0; i < maxActiveSessions+32; i++ {
		_, err := manager.Login("admin", "password123", false)
		require.NoError(t, err)
	}

	manager.mu.RLock()
	sessionCount := len(manager.sessions)
	manager.mu.RUnlock()
	assert.LessOrEqual(t, sessionCount, maxActiveSessions)

	_, err = manager.AuthenticateSession(firstSession)
	assert.ErrorIs(t, err, ErrInvalidSession)
}

func TestAuthManager_LoginRateLimitAndRecovery(t *testing.T) {
	t.Parallel()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	manager, err := NewAuthManager(configFile, time.Hour)
	require.NoError(t, err)
	require.NoError(t, manager.Setup("admin", "password123"))

	now := time.Now()
	manager.nowFn = func() time.Time { return now }

	for i := 0; i < maxFailedLoginAttempts; i++ {
		_, err := manager.Login("admin", "wrong-password", false)
		assert.ErrorIs(t, err, ErrInvalidCredentials)
	}

	_, err = manager.Login("admin", "password123", false)
	assert.ErrorIs(t, err, ErrLoginRateLimited)

	now = now.Add(loginLockoutDuration + time.Second)

	sessionID, err := manager.Login("admin", "password123", false)
	require.NoError(t, err)
	assert.NotEmpty(t, sessionID)
}
