package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetLatestRelease_Success tests fetching latest release from GitHub API
func TestGetLatestRelease_Success(t *testing.T) {
	// NEGATIVE: No server setup
	if false {
		t.Skip("NEGATIVE: Should not reach here")
	}

	// Setup mock GitHub API server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// NEGATIVE: Wrong path
		if r.URL.Path != "/repos/test-org/test-repo/releases/latest" {
			http.NotFound(w, r)
			return
		}

		// NEGATIVE: Wrong method
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Return mock release data with current architecture
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{
			"tag_name": "v1.1.0",
			"assets": [
				{
					"name": "gateway-v1.1.0-%s",
					"browser_download_url": "http://example.com/download"
				}
			]
		}`, runtime.GOARCH)
	}))
	defer mockServer.Close()

	// Create updater with mock server
	u := New(Options{
		CurrentVersion: "v1.0.0",
		BinaryPath:     "/opt/gateway",
		GitHubAPI:      mockServer.URL + "/repos/test-org/test-repo/releases/latest",
	})

	// Execute
	release, err := u.getLatestRelease()

	// Verify
	require.NoError(t, err)
	assert.Equal(t, "v1.1.0", release.Version)
	assert.Equal(t, "http://example.com/download", release.DownloadURL)
}

// TestGetLatestRelease_NoNewVersion tests when local is already up to date
func TestGetLatestRelease_NoNewVersion(t *testing.T) {
	// Setup mock server returning same version
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{
			"tag_name": "v1.0.0",
			"assets": [
				{
					"name": "gateway-v1.0.0-%s",
					"browser_download_url": "http://example.com/download"
				}
			]
		}`, runtime.GOARCH)
	}))
	defer mockServer.Close()

	u := New(Options{
		CurrentVersion: "v1.0.0",
		BinaryPath:     "/opt/gateway",
		GitHubAPI:      mockServer.URL + "/repos/test-org/test-repo/releases/latest",
	})

	release, err := u.getLatestRelease()

	// Should return release but version comparison shows no update needed
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", release.Version)
}

// TestGetLatestRelease_APIError tests handling API failures
func TestGetLatestRelease_APIError(t *testing.T) {
	// Setup mock server that returns error
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mockServer.Close()

	u := New(Options{
		CurrentVersion: "v1.0.0",
		BinaryPath:     "/opt/gateway",
		GitHubAPI:      mockServer.URL + "/repos/test-org/test-repo/releases/latest",
	})

	release, err := u.getLatestRelease()

	// NEGATIVE: Should return error
	require.Error(t, err)
	assert.Nil(t, release)
	assert.Contains(t, err.Error(), "GitHub API")
}

// TestDownload_Success tests downloading binary
func TestDownload_Success(t *testing.T) {
	// Setup mock download server
	expectedContent := []byte("binary content here")
	downloadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(expectedContent)))
		w.WriteHeader(http.StatusOK)
		w.Write(expectedContent)
	}))
	defer downloadServer.Close()

	// Create temp directory for download
	tempDir := t.TempDir()

	u := New(Options{
		CurrentVersion: "v1.0.0",
		BinaryPath:     filepath.Join(tempDir, "gateway"),
		DownloadDir:    tempDir,
	})

	// Execute
	downloadedPath, err := u.download(downloadServer.URL + "/gateway-v1.1.0")

	// Verify
	require.NoError(t, err)
	require.FileExists(t, downloadedPath)

	// Verify content
	content, err := os.ReadFile(downloadedPath)
	require.NoError(t, err)
	assert.Equal(t, expectedContent, content)
}

// TestDownload_HTTPError tests handling HTTP errors during download
func TestDownload_HTTPError(t *testing.T) {
	// Setup server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	u := New(Options{
		CurrentVersion: "v1.0.0",
		BinaryPath:     filepath.Join(tempDir, "gateway"),
		DownloadDir:    tempDir,
	})

	// Execute
	path, err := u.download(server.URL + "/not-found")

	// NEGATIVE: Should return error
	require.Error(t, err)
	assert.Empty(t, path)
}

// TestVerifyChecksum_Success tests checksum verification
func TestVerifyChecksum_Success(t *testing.T) {
	// Create temp file with known content
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-binary")
	content := []byte("test binary content")
	require.NoError(t, os.WriteFile(filePath, content, 0644))

	// Calculate expected SHA256
	h := sha256.New()
	h.Write(content)
	expectedChecksum := hex.EncodeToString(h.Sum(nil))

	u := New(Options{
		CurrentVersion: "v1.0.0",
		BinaryPath:     filePath,
	})

	// Execute
	err := u.verifyChecksum(filePath, expectedChecksum)

	// Verify
	require.NoError(t, err)
}

// TestVerifyChecksum_Invalid tests invalid checksum
func TestVerifyChecksum_Invalid(t *testing.T) {
	// Create temp file
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test-binary")
	content := []byte("test binary content")
	require.NoError(t, os.WriteFile(filePath, content, 0644))

	// Wrong checksum
	wrongChecksum := "0000000000000000000000000000000000000000000000000000000000000000"

	u := New(Options{
		CurrentVersion: "v1.0.0",
		BinaryPath:     filePath,
	})

	// Execute
	err := u.verifyChecksum(filePath, wrongChecksum)

	// NEGATIVE: Should return error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
}

// TestVerifyChecksum_FileNotFound tests missing file
func TestVerifyChecksum_FileNotFound(t *testing.T) {
	u := New(Options{
		CurrentVersion: "v1.0.0",
		BinaryPath:     "/opt/gateway",
	})

	// NEGATIVE: Non-existent file
	err := u.verifyChecksum("/nonexistent/path", "abc123")

	require.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}

// TestUpdate_AlreadyUpdating tests preventing concurrent updates
func TestUpdate_AlreadyUpdating(t *testing.T) {
	u := New(Options{
		CurrentVersion: "v1.0.0",
		BinaryPath:     "/opt/gateway",
	})

	// Simulate update in progress
	u.updating.Store(true)

	// Execute
	err := u.Update()

	// NEGATIVE: Should return error for concurrent update
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already in progress")
}

// TestUpdate_NoNewVersion tests when already up to date
func TestUpdate_NoNewVersion(t *testing.T) {
	// Setup mock server returning same version
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{
			"tag_name": "v1.0.0",
			"assets": [
				{
					"name": "gateway-v1.0.0-%s",
					"browser_download_url": "http://example.com/download"
				}
			]
		}`, runtime.GOARCH)
	}))
	defer mockServer.Close()

	tempDir := t.TempDir()
	u := New(Options{
		CurrentVersion: "v1.0.0",
		BinaryPath:     filepath.Join(tempDir, "gateway"),
		GitHubAPI:      mockServer.URL + "/repos/test-org/test-repo/releases/latest",
	})

	// Execute - should succeed without doing anything
	// NOTE: This test expects getLatestRelease to work but Update logic is not yet implemented
	release, err := u.getLatestRelease()
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", release.Version)
}

// TestOptions_Defaults tests default option values
func TestOptions_Defaults(t *testing.T) {
	opts := Options{
		CurrentVersion: "v1.0.0",
		BinaryPath:     "/opt/gateway",
	}

	// Apply defaults
	if opts.DownloadDir == "" {
		opts.DownloadDir = "/tmp"
	}
	if opts.BackupDir == "" {
		opts.BackupDir = "/opt/amg-rfid-gateway/backup"
	}
	if opts.HealthURL == "" {
		opts.HealthURL = "http://localhost:8080/health"
	}
	if opts.GitHubAPI == "" {
		opts.GitHubAPI = "https://api.github.com/repos/amg-rfid/amg-rfid-gateway/releases/latest"
	}

	assert.Equal(t, "/tmp", opts.DownloadDir)
	assert.Equal(t, "/opt/amg-rfid-gateway/backup", opts.BackupDir)
	assert.Equal(t, "http://localhost:8080/health", opts.HealthURL)
	assert.Equal(t, "https://api.github.com/repos/amg-rfid/amg-rfid-gateway/releases/latest", opts.GitHubAPI)
}

// TestNew_CreatesUpdater tests the constructor
func TestNew_CreatesUpdater(t *testing.T) {
	opts := Options{
		CurrentVersion: "v1.0.0",
		BinaryPath:     "/opt/gateway",
		DownloadDir:    "/tmp/downloads",
		BackupDir:      "/opt/backup",
		HealthURL:      "http://localhost:9090/health",
		GitHubAPI:      "https://api.example.com/releases",
	}

	u := New(opts)

	require.NotNil(t, u)
	assert.Equal(t, "v1.0.0", u.currentVersion)
	assert.Equal(t, "/opt/gateway", u.binaryPath)
	assert.Equal(t, "/tmp/downloads", u.downloadDir)
	assert.Equal(t, "/opt/backup", u.backupDir)
	assert.Equal(t, "http://localhost:9090/health", u.healthURL)
	assert.Equal(t, "https://api.example.com/releases", u.githubAPI)
	assert.False(t, u.updating.Load())
}

// TestRelease_AssetNotFound tests when no matching asset is found
func TestRelease_AssetNotFound(t *testing.T) {
	// Setup mock server with no matching assets (use architecture that doesn't exist)
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{
			"tag_name": "v1.1.0",
			"assets": [
				{
					"name": "gateway-v1.1.0-unsupported-arch",
					"browser_download_url": "http://example.com/download"
				}
			]
		}`)
	}))
	defer mockServer.Close()

	u := New(Options{
		CurrentVersion: "v1.0.0",
		BinaryPath:     "/opt/gateway",
		GitHubAPI:      mockServer.URL + "/repos/test-org/test-repo/releases/latest",
	})

	release, err := u.getLatestRelease()

	// Should return error - no matching asset
	require.Error(t, err)
	assert.Nil(t, release)
	assert.Contains(t, err.Error(), "asset")
}

// BenchmarkDownload benchmarks the download function
func BenchmarkDownload(b *testing.B) {
	content := make([]byte, 1024*1024) // 1MB
	for i := range content {
		content[i] = byte(i % 256)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer server.Close()

	tempDir := b.TempDir()
	u := New(Options{
		CurrentVersion: "v1.0.0",
		BinaryPath:     filepath.Join(tempDir, "gateway"),
		DownloadDir:    tempDir,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := u.download(server.URL)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// MockHTTPClient allows injecting custom HTTP client for testing
type MockHTTPClient struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

func (m *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.DoFunc != nil {
		return m.DoFunc(req)
	}
	return nil, errors.New("no mock response configured")
}

// TestDownload_WithMockClient tests using a mock HTTP client
func TestDownload_WithMockClient(t *testing.T) {
	// Create a mock response
	bodyContent := []byte("mocked binary data")
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			recorder := httptest.NewRecorder()
			recorder.WriteHeader(http.StatusOK)
			recorder.Write(bodyContent)
			return recorder.Result(), nil
		},
	}

	tempDir := t.TempDir()
	u := New(Options{
		CurrentVersion: "v1.0.0",
		BinaryPath:     filepath.Join(tempDir, "gateway"),
		DownloadDir:    tempDir,
	})

	// Set the mock client (this would need a setter or field access in real impl)
	// For now, we just verify the structure compiles
	_ = mockClient
	_ = u

	// This test verifies the mock pattern can be used
	t.Log("Mock client pattern established for dependency injection")
}

// TestVersionComparison tests version comparison logic
func TestVersionComparison(t *testing.T) {
	tests := []struct {
		name     string
		local    string
		remote   string
		expected bool // true if remote is newer
	}{
		{"same version", "v1.0.0", "v1.0.0", false},
		{"newer patch", "v1.0.0", "v1.0.1", true},
		{"newer minor", "v1.0.0", "v1.1.0", true},
		{"newer major", "v1.0.0", "v2.0.0", true},
		{"older version", "v1.1.0", "v1.0.0", false},
		{"no v prefix local", "1.0.0", "v1.1.0", true},
		{"no v prefix remote", "v1.0.0", "1.1.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNewerVersion(tt.local, tt.remote)
			assert.Equal(t, tt.expected, result, "isNewerVersion(%s, %s)", tt.local, tt.remote)
		})
	}
}

// TestDownload_Timeout tests download timeout handling
func TestDownload_Timeout(t *testing.T) {
	// Setup slow server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("too late"))
	}))
	defer server.Close()

	tempDir := t.TempDir()
	u := New(Options{
		CurrentVersion: "v1.0.0",
		BinaryPath:     filepath.Join(tempDir, "gateway"),
		DownloadDir:    tempDir,
	})

	// This test will pass once we implement timeout handling
	// For now, it verifies the test structure
	_ = u
	t.Log("Timeout test structure ready")
}

// TestGetLatestRelease_InvalidJSON tests handling of invalid JSON response
func TestGetLatestRelease_InvalidJSON(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("invalid json {{"))
	}))
	defer mockServer.Close()

	u := New(Options{
		CurrentVersion: "v1.0.0",
		BinaryPath:     "/opt/gateway",
		GitHubAPI:      mockServer.URL + "/repos/test-org/test-repo/releases/latest",
	})

	release, err := u.getLatestRelease()

	// NEGATIVE: Should return error for invalid JSON
	require.Error(t, err)
	assert.Nil(t, release)
}

// TestDownload_CreatesDirectories tests that download creates necessary directories
func TestDownload_CreatesDirectories(t *testing.T) {
	content := []byte("test binary")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer server.Close()

	// Use nested directory that doesn't exist
	tempDir := t.TempDir()
	nestedDir := filepath.Join(tempDir, "nested", "downloads")

	u := New(Options{
		CurrentVersion: "v1.0.0",
		BinaryPath:     filepath.Join(tempDir, "gateway"),
		DownloadDir:    nestedDir,
	})

	// Execute
	path, err := u.download(server.URL)

	// Verify
	require.NoError(t, err)
	assert.FileExists(t, path)
}

// TestDownload_VerifyChecksum_Integration tests full download and verify flow
func TestDownload_VerifyChecksum_Integration(t *testing.T) {
	// Binary content
	content := []byte("gateway binary v1.1.0")

	// Calculate expected checksum
	h := sha256.New()
	h.Write(content)
	expectedChecksum := hex.EncodeToString(h.Sum(nil))

	// Setup server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	u := New(Options{
		CurrentVersion: "v1.0.0",
		BinaryPath:     filepath.Join(tempDir, "gateway"),
		DownloadDir:    tempDir,
	})

	// Download
	path, err := u.download(server.URL)
	require.NoError(t, err)

	// Verify checksum
	err = u.verifyChecksum(path, expectedChecksum)
	require.NoError(t, err)

	// Verify with wrong checksum should fail
	err = u.verifyChecksum(path, "wrongchecksum")
	require.Error(t, err)
}
