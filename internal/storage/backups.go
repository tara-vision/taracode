package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CreateBackup creates a backup of a file before editing
// Returns the backup file path or an error
func (m *Manager) CreateBackup(originalPath string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Read original file content
	content, err := os.ReadFile(originalPath) //nolint:gosec // the caller supplies the path to back up before editing it
	if err != nil {
		return "", fmt.Errorf("failed to read file for backup: %w", err)
	}

	// Create backup filename: original_name.timestamp
	baseName := filepath.Base(originalPath)
	timestamp := time.Now().Unix()
	backupName := fmt.Sprintf("%s.%d", baseName, timestamp)
	backupPath := filepath.Join(m.rootDir, "backups", backupName)

	// Write backup
	//nolint:gosec // matches the 0644 used for every other file this package writes
	if err := os.WriteFile(backupPath, content, 0644); err != nil {
		return "", fmt.Errorf("failed to write backup: %w", err)
	}

	return backupPath, nil
}

// GetBackupDir returns the path to the backups directory
func (m *Manager) GetBackupDir() string {
	return filepath.Join(m.rootDir, "backups")
}

// ListBackups returns a list of backup files for a given original filename
func (m *Manager) ListBackups(originalFilename string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	backupDir := filepath.Join(m.rootDir, "backups")
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read backup directory: %w", err)
	}

	var backups []string
	prefix := originalFilename + "."
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			backups = append(backups, filepath.Join(backupDir, entry.Name()))
		}
	}

	return backups, nil
}
