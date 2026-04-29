package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/coreos/go-semver/semver"
)

// Safety provides backup, rollback, and health check functionality
type Safety struct {
	binaryPath string
	backupDir  string
	healthURL  string
}

// NewSafety creates a new Safety instance
func NewSafety(binaryPath, backupDir, healthURL string) *Safety {
	return &Safety{
		binaryPath: binaryPath,
		backupDir:  backupDir,
		healthURL:  healthURL,
	}
}

// backup creates a backup of the current binary
func (s *Safety) backup(version string) error {
	// NEGATIVE: Empty version
	if version == "" {
		return fmt.Errorf("version cannot be empty")
	}

	// NEGATIVE: Binary doesn't exist
	if _, err := os.Stat(s.binaryPath); os.IsNotExist(err) {
		return fmt.Errorf("binary not found: %s", s.binaryPath)
	}

	// Create backup directory
	if err := os.MkdirAll(s.backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Backup path
	backupPath := filepath.Join(s.backupDir, fmt.Sprintf("gateway-%s", version))

	// Copy binary to backup
	if err := copyFile(s.binaryPath, backupPath); err != nil {
		return fmt.Errorf("failed to backup binary: %w", err)
	}

	return nil
}

// rollback restores the binary from backup with integrity verification
func (s *Safety) rollback(version string) error {
	// NEGATIVE: Empty version
	if version == "" {
		return fmt.Errorf("version cannot be empty")
	}

	// Backup path
	backupPath := filepath.Join(s.backupDir, fmt.Sprintf("gateway-%s", version))

	// NEGATIVE: Backup doesn't exist
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup not found: %s", backupPath)
	}

	// Calculate checksum of backup before restoring
	backupChecksum, err := s.calculateChecksum(backupPath)
	if err != nil {
		return fmt.Errorf("failed to calculate backup checksum: %w", err)
	}

	// Remove current binary if exists
	if err := os.Remove(s.binaryPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove current binary: %w", err)
	}

	// Restore from backup
	if err := copyFile(backupPath, s.binaryPath); err != nil {
		return fmt.Errorf("failed to restore backup: %w", err)
	}

	// Make binary executable
	if err := os.Chmod(s.binaryPath, 0755); err != nil {
		return fmt.Errorf("failed to make binary executable: %w", err)
	}

	// NEGATIVE: Verify restored binary integrity
	restoredChecksum, err := s.calculateChecksum(s.binaryPath)
	if err != nil {
		return fmt.Errorf("failed to verify restored binary: %w", err)
	}

	if restoredChecksum != backupChecksum {
		return fmt.Errorf("integrity check failed: restored binary checksum mismatch (expected %s, got %s)", backupChecksum, restoredChecksum)
	}

	return nil
}

// calculateChecksum calculates the SHA256 checksum of a file
func (s *Safety) calculateChecksum(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("failed to calculate checksum: %w", err)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// healthCheck performs a health check via HTTP
func (s *Safety) healthCheck() error {
	// NEGATIVE: Empty health URL
	if s.healthURL == "" {
		return fmt.Errorf("health URL not configured")
	}

	// Create client with timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Perform request
	resp, err := client.Get(s.healthURL)
	if err != nil {
		return fmt.Errorf("health check request failed: %w", err)
	}
	defer resp.Body.Close()

	// NEGATIVE: Non-200 response
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned status %d", resp.StatusCode)
	}

	return nil
}

// healthCheckWithTimeout performs a health check with custom timeout and retries
func (s *Safety) healthCheckWithTimeout(timeout time.Duration, maxRetries int) error {
	// NEGATIVE: Invalid timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	// NEGATIVE: Invalid retries
	if maxRetries < 0 {
		maxRetries = 0
	}

	client := &http.Client{
		Timeout: timeout,
	}

	var lastErr error
	for i := 0; i <= maxRetries; i++ {
		resp, err := client.Get(s.healthURL)
		if err != nil {
			lastErr = err
			if i < maxRetries {
				time.Sleep(time.Second * time.Duration(i+1))
				continue
			}
			break
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			return nil
		}

		lastErr = fmt.Errorf("health check returned status %d", resp.StatusCode)
		if i < maxRetries {
			time.Sleep(time.Second * time.Duration(i+1))
		}
	}

	return fmt.Errorf("health check failed after %d retries: %w", maxRetries, lastErr)
}

// cleanup removes old backups keeping only the N most recent
func (s *Safety) cleanup(keep int) error {
	// NEGATIVE: Invalid keep count
	if keep < 0 {
		return fmt.Errorf("keep count must be non-negative")
	}

	// Read backup directory
	entries, err := os.ReadDir(s.backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No backup dir, nothing to clean
		}
		return fmt.Errorf("failed to read backup directory: %w", err)
	}

	// Filter for backup files (exclude directories)
	// Backup files are named like "gateway-v1.0.0" with no extension
	type backupInfo struct {
		name    string
		modTime time.Time
	}
	var backups []backupInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		backups = append(backups, backupInfo{
			name:    entry.Name(),
			modTime: info.ModTime(),
		})
	}

	// NEGATIVE: Not enough backups to clean
	if len(backups) <= keep {
		return nil
	}

	// Sort by modification time (oldest first)
	for i := 0; i < len(backups)-1; i++ {
		for j := i + 1; j < len(backups); j++ {
			if backups[i].modTime.After(backups[j].modTime) {
				backups[i], backups[j] = backups[j], backups[i]
			}
		}
	}

	// Remove oldest backups
	for i := 0; i < len(backups)-keep; i++ {
		path := filepath.Join(s.backupDir, backups[i].name)
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("failed to remove old backup %s: %w", path, err)
		}
	}

	return nil
}

// listBackups returns a list of available backup versions
func (s *Safety) listBackups() ([]string, error) {
	// Read backup directory
	entries, err := os.ReadDir(s.backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil // No backup dir, no backups
		}
		return nil, fmt.Errorf("failed to read backup directory: %w", err)
	}

	// Filter for backup files
	var backups []string
	for _, entry := range entries {
		if !entry.IsDir() {
			backups = append(backups, entry.Name())
		}
	}

	return backups, nil
}

// getCurrentVersion returns the version of the current binary
// by executing it with --version flag
func (s *Safety) getCurrentVersion() (string, error) {
	// Check if binary exists
	if _, err := os.Stat(s.binaryPath); os.IsNotExist(err) {
		// Binary doesn't exist - return unknown (for testing)
		return "unknown", nil
	}

	// Execute binary with --version flag
	output, err := exec.Command(s.binaryPath, "--version").Output()
	if err != nil {
		// If execution fails, return unknown
		return "unknown", nil
	}

	// Parse version from output (expected format: "AMG RFID Gateway v0.1.0" or just "0.1.0")
	version := strings.TrimSpace(string(output))

	// Extract version from common patterns
	// Pattern 1: "vX.Y.Z"
	if strings.HasPrefix(version, "v") {
		return version, nil
	}

	// Pattern 2: "AMG RFID Gateway vX.Y.Z" - extract last word
	parts := strings.Fields(version)
	if len(parts) > 0 {
		lastPart := parts[len(parts)-1]
		if strings.HasPrefix(lastPart, "v") {
			return lastPart, nil
		}
		// Try to validate it looks like a version
		if _, err := semver.NewVersion(lastPart); err == nil {
			return lastPart, nil
		}
	}

	// Pattern 3: Try the whole string
	if _, err := semver.NewVersion(version); err == nil {
		return version, nil
	}

	return "unknown", nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	// Open source
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	// Create destination
	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	// Copy content
	if _, err := io.Copy(destination, source); err != nil {
		return err
	}

	// Copy file permissions
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	return os.Chmod(dst, info.Mode())
}
