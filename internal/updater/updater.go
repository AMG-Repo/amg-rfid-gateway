package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/coreos/go-semver/semver"
)

// Options configures the Updater
type Options struct {
	CurrentVersion  string
	BinaryPath      string
	DownloadDir     string
	BackupDir       string
	HealthURL       string
	GitHubAPI       string
	MaxDownloadSize int64 // Maximum download size in bytes (default: 100MB)
}

// Default max download size: 100MB
const defaultMaxDownloadSize = 100 * 1024 * 1024

// Release represents a GitHub release
type Release struct {
	Version     string
	DownloadURL string
	Checksum    string
}

// Updater handles OTA updates
type Updater struct {
	currentVersion  string
	binaryPath      string
	downloadDir     string
	backupDir       string
	healthURL       string
	githubAPI       string
	maxDownloadSize int64
	updating        atomic.Bool
	client          *http.Client
	safety          *Safety
}

// New creates a new Updater with the given options
func New(opts Options) *Updater {
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
	if opts.MaxDownloadSize <= 0 {
		opts.MaxDownloadSize = defaultMaxDownloadSize
	}

	u := &Updater{
		currentVersion:  opts.CurrentVersion,
		binaryPath:      opts.BinaryPath,
		downloadDir:     opts.DownloadDir,
		backupDir:       opts.BackupDir,
		healthURL:       opts.HealthURL,
		githubAPI:       opts.GitHubAPI,
		maxDownloadSize: opts.MaxDownloadSize,
		client:          &http.Client{},
	}

	u.safety = NewSafety(opts.BinaryPath, opts.BackupDir, opts.HealthURL)

	return u
}

// getLatestRelease fetches the latest release from GitHub
func (u *Updater) getLatestRelease() (*Release, error) {
	// NEGATIVE: Empty API URL
	if u.githubAPI == "" {
		return nil, fmt.Errorf("GitHub API URL not configured")
	}

	// Make request
	resp, err := u.client.Get(u.githubAPI)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()

	// NEGATIVE: Non-200 response
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	// Parse response
	var releaseData struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&releaseData); err != nil {
		return nil, fmt.Errorf("failed to parse GitHub response: %w", err)
	}

	// Find matching asset for current architecture
	var downloadURL string
	for _, asset := range releaseData.Assets {
		if strings.Contains(asset.Name, runtime.GOARCH) {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	// NEGATIVE: No matching asset found
	if downloadURL == "" {
		return nil, fmt.Errorf("no asset found for architecture %s", runtime.GOARCH)
	}

	return &Release{
		Version:     releaseData.TagName,
		DownloadURL: downloadURL,
	}, nil
}

// download downloads a file from the given URL
func (u *Updater) download(url string) (string, error) {
	// NEGATIVE: Empty URL
	if url == "" {
		return "", fmt.Errorf("download URL is empty")
	}

	// Create download directory if needed
	if err := os.MkdirAll(u.downloadDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create download directory: %w", err)
	}

	// Make request
	resp, err := u.client.Get(url)
	if err != nil {
		return "", fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	// NEGATIVE: Non-200 response
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	// NEGATIVE: Content length exceeds max download size
	if resp.ContentLength > 0 && resp.ContentLength > u.maxDownloadSize {
		return "", fmt.Errorf("download size %d exceeds maximum allowed %d", resp.ContentLength, u.maxDownloadSize)
	}

	// Create temp file
	tempFile := filepath.Join(u.downloadDir, fmt.Sprintf("gateway-download-%d", os.Getpid()))
	out, err := os.Create(tempFile)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer out.Close()

	// Copy content with size limit using io.LimitReader
	limitedReader := io.LimitReader(resp.Body, u.maxDownloadSize+1)
	n, err := io.Copy(out, limitedReader)
	if err != nil {
		os.Remove(tempFile)
		return "", fmt.Errorf("failed to download content: %w", err)
	}

	// NEGATIVE: Download exceeded size limit
	if n > u.maxDownloadSize {
		os.Remove(tempFile)
		return "", fmt.Errorf("download size exceeded maximum allowed %d bytes", u.maxDownloadSize)
	}

	return tempFile, nil
}

// verifyChecksum verifies the SHA256 checksum of a file
func (u *Updater) verifyChecksum(filePath, expectedChecksum string) error {
	// NEGATIVE: Empty checksum
	if expectedChecksum == "" {
		return fmt.Errorf("checksum is empty")
	}

	// Open file
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Calculate checksum
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("failed to calculate checksum: %w", err)
	}
	actualChecksum := hex.EncodeToString(hash.Sum(nil))

	// NEGATIVE: Checksum mismatch
	if actualChecksum != strings.ToLower(expectedChecksum) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum)
	}

	return nil
}

// Update performs the full update process
func (u *Updater) Update() error {
	// NEGATIVE: Already updating
	if u.updating.Load() {
		return fmt.Errorf("update already in progress")
	}

	// Mark as updating
	if !u.updating.CompareAndSwap(false, true) {
		return fmt.Errorf("update already in progress")
	}
	defer u.updating.Store(false)

	// Get latest release
	release, err := u.getLatestRelease()
	if err != nil {
		return fmt.Errorf("get latest release: %w", err)
	}

	// NEGATIVE: No new version
	if !isNewerVersion(u.currentVersion, release.Version) {
		return nil // Already up to date
	}

	// Download new binary
	newBinary, err := u.download(release.DownloadURL)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}

	// Verify checksum if provided
	if release.Checksum != "" {
		if err := u.verifyChecksum(newBinary, release.Checksum); err != nil {
			os.Remove(newBinary)
			return fmt.Errorf("checksum failed: %w", err)
		}
	}

	// Create backup of current version before updating
	if err := u.safety.backup(u.currentVersion); err != nil {
		os.Remove(newBinary)
		return fmt.Errorf("backup failed: %w", err)
	}

	// Apply the update by replacing binary
	if err := u.applyUpdate(newBinary); err != nil {
		// Rollback on failure
		if rbErr := u.safety.rollback(u.currentVersion); rbErr != nil {
			return fmt.Errorf("update failed and rollback failed: %v (rollback error: %w)", err, rbErr)
		}
		return fmt.Errorf("update failed, rolled back: %w", err)
	}

	// Clean up downloaded file
	os.Remove(newBinary)

	// Clean up old backups
	if err := u.safety.cleanup(3); err != nil {
		// Non-fatal: log but don't fail
		_ = err
	}

	return nil
}

// applyUpdate replaces the current binary with the new one
func (u *Updater) applyUpdate(newBinaryPath string) error {
	// Remove current binary
	if err := os.Remove(u.binaryPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove current binary: %w", err)
	}

	// Move new binary to target location
	if err := os.Rename(newBinaryPath, u.binaryPath); err != nil {
		return fmt.Errorf("failed to install new binary: %w", err)
	}

	// Make binary executable
	if err := os.Chmod(u.binaryPath, 0755); err != nil {
		return fmt.Errorf("failed to make binary executable: %w", err)
	}

	return nil
}

// isNewerVersion compares two semantic versions
// Returns true if remote is newer than local
func isNewerVersion(local, remote string) bool {
	// NEGATIVE: Same version string
	if local == remote {
		return false
	}

	// Normalize versions (ensure 'v' prefix for parsing)
	local = normalizeVersion(local)
	remote = normalizeVersion(remote)

	// Parse local version
	localVer, err := semver.NewVersion(local)
	if err != nil {
		// If we can't parse local, assume remote is newer
		return true
	}

	// Parse remote version
	remoteVer, err := semver.NewVersion(remote)
	if err != nil {
		// If we can't parse remote, assume not newer
		return false
	}

	// Compare versions: remote > local means remote is newer
	return remoteVer.Compare(*localVer) > 0
}

// normalizeVersion ensures version is in semver format (X.Y.Z without 'v' prefix)
func normalizeVersion(v string) string {
	if v == "" {
		return "0.0.0"
	}
	// Remove 'v' or 'V' prefix if present
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	return v
}
