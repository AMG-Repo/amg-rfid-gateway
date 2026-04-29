package updater

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSafety_Backup_Success tests successful backup creation
func TestSafety_Backup_Success(t *testing.T) {
	// Create temp directories
	tempDir := t.TempDir()
	binaryPath := filepath.Join(tempDir, "gateway")
	backupDir := filepath.Join(tempDir, "backup")

	// Create a test binary
	testContent := []byte("gateway binary v1.0.0")
	require.NoError(t, os.WriteFile(binaryPath, testContent, 0755))

	s := NewSafety(binaryPath, backupDir, "http://localhost:8080/health")

	// Execute
	err := s.backup("v1.0.0")

	// Verify
	require.NoError(t, err)
	backupPath := filepath.Join(backupDir, "gateway-v1.0.0")
	assert.FileExists(t, backupPath)

	// Verify content
	content, err := os.ReadFile(backupPath)
	require.NoError(t, err)
	assert.Equal(t, testContent, content)
}

// TestSafety_Backup_EmptyVersion tests backup with empty version
func TestSafety_Backup_EmptyVersion(t *testing.T) {
	tempDir := t.TempDir()
	s := NewSafety(filepath.Join(tempDir, "gateway"), filepath.Join(tempDir, "backup"), "")

	// Execute with empty version
	err := s.backup("")

	// NEGATIVE: Should return error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version cannot be empty")
}

// TestSafety_Backup_BinaryNotFound tests backup when binary doesn't exist
func TestSafety_Backup_BinaryNotFound(t *testing.T) {
	tempDir := t.TempDir()
	s := NewSafety(filepath.Join(tempDir, "nonexistent"), filepath.Join(tempDir, "backup"), "")

	// Execute
	err := s.backup("v1.0.0")

	// NEGATIVE: Should return error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "binary not found")
}

// TestSafety_Rollback_Success tests successful rollback
func TestSafety_Rollback_Success(t *testing.T) {
	// Create temp directories
	tempDir := t.TempDir()
	binaryPath := filepath.Join(tempDir, "gateway")
	backupDir := filepath.Join(tempDir, "backup")

	// Create backup
	backupContent := []byte("gateway binary v1.0.0")
	require.NoError(t, os.MkdirAll(backupDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(backupDir, "gateway-v1.0.0"), backupContent, 0644))

	// Create current binary (different version)
	currentContent := []byte("gateway binary v1.1.0 (bad)")
	require.NoError(t, os.WriteFile(binaryPath, currentContent, 0755))

	s := NewSafety(binaryPath, backupDir, "")

	// Execute rollback
	err := s.rollback("v1.0.0")

	// Verify
	require.NoError(t, err)
	content, err := os.ReadFile(binaryPath)
	require.NoError(t, err)
	assert.Equal(t, backupContent, content)
}

// TestSafety_Rollback_BackupNotFound tests rollback when backup doesn't exist
func TestSafety_Rollback_BackupNotFound(t *testing.T) {
	tempDir := t.TempDir()
	s := NewSafety(filepath.Join(tempDir, "gateway"), filepath.Join(tempDir, "backup"), "")

	// Execute
	err := s.rollback("v1.0.0")

	// NEGATIVE: Should return error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backup not found")
}

// TestSafety_Rollback_EmptyVersion tests rollback with empty version
func TestSafety_Rollback_EmptyVersion(t *testing.T) {
	tempDir := t.TempDir()
	s := NewSafety(filepath.Join(tempDir, "gateway"), filepath.Join(tempDir, "backup"), "")

	// Execute
	err := s.rollback("")

	// NEGATIVE: Should return error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version cannot be empty")
}

// TestSafety_HealthCheck_Success tests successful health check
func TestSafety_HealthCheck_Success(t *testing.T) {
	// Setup mock health server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// NEGATIVE: Wrong path
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	s := NewSafety("/opt/gateway", "/opt/backup", server.URL+"/health")

	// Execute
	err := s.healthCheck()

	// Verify
	require.NoError(t, err)
}

// TestSafety_HealthCheck_Failure tests failed health check
func TestSafety_HealthCheck_Failure(t *testing.T) {
	// Setup failing health server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	s := NewSafety("/opt/gateway", "/opt/backup", server.URL+"/health")

	// Execute
	err := s.healthCheck()

	// NEGATIVE: Should return error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 503")
}

// TestSafety_HealthCheck_EmptyURL tests health check with empty URL
func TestSafety_HealthCheck_EmptyURL(t *testing.T) {
	s := NewSafety("/opt/gateway", "/opt/backup", "")

	// Execute
	err := s.healthCheck()

	// NEGATIVE: Should return error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "health URL not configured")
}

// TestSafety_HealthCheckWithTimeout_Success tests health check with retries
func TestSafety_HealthCheckWithTimeout_Success(t *testing.T) {
	// Setup mock server that succeeds
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	s := NewSafety("/opt/gateway", "/opt/backup", server.URL)

	// Execute with timeout and retries
	err := s.healthCheckWithTimeout(5*time.Second, 3)

	// Verify
	require.NoError(t, err)
}

// TestSafety_Cleanup_Success tests cleanup of old backups
func TestSafety_Cleanup_Success(t *testing.T) {
	// Create temp directory with multiple backups
	tempDir := t.TempDir()
	backupDir := filepath.Join(tempDir, "backup")
	require.NoError(t, os.MkdirAll(backupDir, 0755))

	// Create test backups with delays to ensure different timestamps
	for i := 1; i <= 5; i++ {
		path := filepath.Join(backupDir, fmt.Sprintf("gateway-v1.0.%d", i))
		require.NoError(t, os.WriteFile(path, []byte("backup"), 0644))
		time.Sleep(100 * time.Millisecond)
	}

	s := NewSafety("/opt/gateway", backupDir, "")

	// Execute cleanup keeping only 2
	err := s.cleanup(2)

	// Verify
	require.NoError(t, err)
	entries, err := os.ReadDir(backupDir)
	require.NoError(t, err)
	assert.Len(t, entries, 2)
}

// TestSafety_Cleanup_InvalidKeep tests cleanup with invalid keep count
func TestSafety_Cleanup_InvalidKeep(t *testing.T) {
	tempDir := t.TempDir()
	s := NewSafety("/opt/gateway", filepath.Join(tempDir, "backup"), "")

	// Execute with negative keep
	err := s.cleanup(-1)

	// NEGATIVE: Should return error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keep count must be non-negative")
}

// TestSafety_Cleanup_NoBackups tests cleanup with no backups
func TestSafety_Cleanup_NoBackups(t *testing.T) {
	tempDir := t.TempDir()
	s := NewSafety("/opt/gateway", filepath.Join(tempDir, "backup"), "")

	// Execute
	err := s.cleanup(2)

	// Verify - should not error
	require.NoError(t, err)
}

// TestSafety_ListBackups_Success tests listing backups
func TestSafety_ListBackups_Success(t *testing.T) {
	// Create temp directory with backups
	tempDir := t.TempDir()
	backupDir := filepath.Join(tempDir, "backup")
	require.NoError(t, os.MkdirAll(backupDir, 0755))

	// Create test backups
	require.NoError(t, os.WriteFile(filepath.Join(backupDir, "gateway-v1.0.0"), []byte("backup1"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(backupDir, "gateway-v1.0.1"), []byte("backup2"), 0644))

	s := NewSafety("/opt/gateway", backupDir, "")

	// Execute
	backups, err := s.listBackups()

	// Verify
	require.NoError(t, err)
	assert.Len(t, backups, 2)
}

// TestSafety_ListBackups_NoDirectory tests listing when backup dir doesn't exist
func TestSafety_ListBackups_NoDirectory(t *testing.T) {
	tempDir := t.TempDir()
	s := NewSafety("/opt/gateway", filepath.Join(tempDir, "nonexistent"), "")

	// Execute
	backups, err := s.listBackups()

	// Verify - should return empty slice, no error
	require.NoError(t, err)
	assert.Empty(t, backups)
}

// TestSafety_GetCurrentVersion tests getting current version
func TestSafety_GetCurrentVersion(t *testing.T) {
	s := NewSafety("/opt/gateway", "/opt/backup", "")

	// Execute
	version, err := s.getCurrentVersion()

	// Verify - placeholder returns "unknown"
	require.NoError(t, err)
	assert.Equal(t, "unknown", version)
}

// TestCopyFile_Success tests file copy
func TestCopyFile_Success(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "source")
	dst := filepath.Join(tempDir, "dest")

	// Create source file
	content := []byte("test content")
	require.NoError(t, os.WriteFile(src, content, 0644))

	// Execute
	err := copyFile(src, dst)

	// Verify
	require.NoError(t, err)
	dstContent, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, content, dstContent)

	// Verify permissions were copied
	srcInfo, _ := os.Stat(src)
	dstInfo, _ := os.Stat(dst)
	assert.Equal(t, srcInfo.Mode(), dstInfo.Mode())
}

// TestCopyFile_SourceNotFound tests copy with missing source
func TestCopyFile_SourceNotFound(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "nonexistent")
	dst := filepath.Join(tempDir, "dest")

	// Execute
	err := copyFile(src, dst)

	// NEGATIVE: Should return error
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}

// TestSafety_Integration_FullUpdateFlow tests the complete update flow with safety
func TestSafety_Integration_FullUpdateFlow(t *testing.T) {
	// Create temp directories
	tempDir := t.TempDir()
	binaryPath := filepath.Join(tempDir, "gateway")
	backupDir := filepath.Join(tempDir, "backup")

	// Create initial binary (v1.0.0)
	v1Content := []byte("gateway v1.0.0")
	require.NoError(t, os.WriteFile(binaryPath, v1Content, 0755))

	// Setup health server
	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthServer.Close()

	s := NewSafety(binaryPath, backupDir, healthServer.URL)

	// Step 1: Backup current binary
	err := s.backup("v1.0.0")
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(backupDir, "gateway-v1.0.0"))

	// Step 2: Simulate update (replace binary)
	v2Content := []byte("gateway v1.1.0")
	require.NoError(t, os.WriteFile(binaryPath, v2Content, 0755))

	// Step 3: Health check passes
	err = s.healthCheck()
	require.NoError(t, err)

	// Step 4: Verify new binary is in place
	content, err := os.ReadFile(binaryPath)
	require.NoError(t, err)
	assert.Equal(t, v2Content, content)

	// Step 5: Simulate rollback
	err = s.rollback("v1.0.0")
	require.NoError(t, err)

	// Step 6: Verify original binary restored
	content, err = os.ReadFile(binaryPath)
	require.NoError(t, err)
	assert.Equal(t, v1Content, content)
}

// TestSafety_Integration_RollbackOnFailedHealth tests rollback when health check fails
func TestSafety_Integration_RollbackOnFailedHealth(t *testing.T) {
	// Create temp directories
	tempDir := t.TempDir()
	binaryPath := filepath.Join(tempDir, "gateway")
	backupDir := filepath.Join(tempDir, "backup")

	// Create initial binary
	v1Content := []byte("gateway v1.0.0 (stable)")
	require.NoError(t, os.WriteFile(binaryPath, v1Content, 0755))

	s := NewSafety(binaryPath, backupDir, "")

	// Backup
	err := s.backup("v1.0.0")
	require.NoError(t, err)

	// Simulate update
	v2Content := []byte("gateway v1.1.0 (broken)")
	require.NoError(t, os.WriteFile(binaryPath, v2Content, 0755))

	// Try health check with empty URL (will fail)
	err = s.healthCheck()
	require.Error(t, err)

	// Rollback
	err = s.rollback("v1.0.0")
	require.NoError(t, err)

	// Verify rollback
	content, err := os.ReadFile(binaryPath)
	require.NoError(t, err)
	assert.Equal(t, v1Content, content)
}

// TestSafety_Backup_PreservesPermissions tests that backup preserves file permissions
func TestSafety_Backup_PreservesPermissions(t *testing.T) {
	tempDir := t.TempDir()
	binaryPath := filepath.Join(tempDir, "gateway")
	backupDir := filepath.Join(tempDir, "backup")

	// Create binary with specific permissions
	content := []byte("gateway binary")
	require.NoError(t, os.WriteFile(binaryPath, content, 0750))

	s := NewSafety(binaryPath, backupDir, "")

	// Execute
	err := s.backup("v1.0.0")
	require.NoError(t, err)

	// Verify permissions preserved
	backupPath := filepath.Join(backupDir, "gateway-v1.0.0")
	info, err := os.Stat(backupPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0750), info.Mode().Perm())
}

// TestSafety_Rollback_MakesExecutable tests that rollback makes binary executable
func TestSafety_Rollback_MakesExecutable(t *testing.T) {
	tempDir := t.TempDir()
	binaryPath := filepath.Join(tempDir, "gateway")
	backupDir := filepath.Join(tempDir, "backup")

	// Create backup (not executable)
	backupContent := []byte("gateway binary")
	require.NoError(t, os.MkdirAll(backupDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(backupDir, "gateway-v1.0.0"), backupContent, 0644))

	// Create current binary
	require.NoError(t, os.WriteFile(binaryPath, []byte("new version"), 0755))

	s := NewSafety(binaryPath, backupDir, "")

	// Execute rollback
	err := s.rollback("v1.0.0")
	require.NoError(t, err)

	// Verify restored binary is executable
	info, err := os.Stat(binaryPath)
	require.NoError(t, err)
	// Should be executable by someone
	assert.NotEqual(t, 0, info.Mode().Perm()&0111)
}

// TestSafety_HealthCheckWithTimeout_Retry tests health check with retries
func TestSafety_HealthCheckWithTimeout_Retry(t *testing.T) {
	// Setup server that fails first 2 times
	attemptCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		if attemptCount < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	s := NewSafety("/opt/gateway", "/opt/backup", server.URL)

	// Execute with retries
	err := s.healthCheckWithTimeout(5*time.Second, 5)

	// Verify - should succeed on 3rd attempt
	require.NoError(t, err)
	assert.GreaterOrEqual(t, attemptCount, 3)
}

// TestSafety_HealthCheckWithTimeout_Failure tests health check exhausting retries
func TestSafety_HealthCheckWithTimeout_Failure(t *testing.T) {
	// Setup server that always fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	s := NewSafety("/opt/gateway", "/opt/backup", server.URL)

	// Execute with few retries
	err := s.healthCheckWithTimeout(5*time.Second, 2)

	// NEGATIVE: Should fail after retries
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after 2 retries")
}

// TestSafety_Cleanup_ExactCount tests cleanup when count equals keep
func TestSafety_Cleanup_ExactCount(t *testing.T) {
	// Create temp directory with exactly 3 backups
	tempDir := t.TempDir()
	backupDir := filepath.Join(tempDir, "backup")
	require.NoError(t, os.MkdirAll(backupDir, 0755))

	for i := 1; i <= 3; i++ {
		path := filepath.Join(backupDir, fmt.Sprintf("gateway-v1.0.%d", i))
		require.NoError(t, os.WriteFile(path, []byte("backup"), 0644))
	}

	s := NewSafety("/opt/gateway", backupDir, "")

	// Execute cleanup keeping all 3
	err := s.cleanup(3)

	// Verify - no backups should be removed
	require.NoError(t, err)
	entries, err := os.ReadDir(backupDir)
	require.NoError(t, err)
	assert.Len(t, entries, 3)
}

// TestSafety_Cleanup_ZeroKeep tests cleanup with keep=0 (remove all)
func TestSafety_Cleanup_ZeroKeep(t *testing.T) {
	// Create temp directory with backups
	tempDir := t.TempDir()
	backupDir := filepath.Join(tempDir, "backup")
	require.NoError(t, os.MkdirAll(backupDir, 0755))

	for i := 1; i <= 3; i++ {
		path := filepath.Join(backupDir, fmt.Sprintf("gateway-v1.0.%d", i))
		require.NoError(t, os.WriteFile(path, []byte("backup"), 0644))
		time.Sleep(100 * time.Millisecond)
	}

	s := NewSafety("/opt/gateway", backupDir, "")

	// Execute cleanup keeping 0
	err := s.cleanup(0)

	// Verify - all backups should be removed
	require.NoError(t, err)
	entries, err := os.ReadDir(backupDir)
	require.NoError(t, err)
	assert.Len(t, entries, 0)
}

// TestNewSafety_CreatesInstance tests constructor
func TestNewSafety_CreatesInstance(t *testing.T) {
	s := NewSafety("/opt/gateway", "/opt/backup", "http://localhost:8080/health")

	require.NotNil(t, s)
	assert.Equal(t, "/opt/gateway", s.binaryPath)
	assert.Equal(t, "/opt/backup", s.backupDir)
	assert.Equal(t, "http://localhost:8080/health", s.healthURL)
}

// BenchmarkBackup benchmarks backup creation
func BenchmarkBackup(b *testing.B) {
	tempDir := b.TempDir()
	binaryPath := filepath.Join(tempDir, "gateway")
	backupDir := filepath.Join(tempDir, "backup")

	// Create test binary (1MB)
	content := make([]byte, 1024*1024)
	require.NoError(b, os.WriteFile(binaryPath, content, 0755))

	s := NewSafety(binaryPath, backupDir, "")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		version := fmt.Sprintf("v1.0.%d", i)
		s.backup(version)
	}
}
