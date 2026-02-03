// Package data provides the data access layer for ClickHouse.
package data

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-kratos/kratos/v2/log"
)

// Migrator handles database migrations for ClickHouse
type Migrator struct {
	data *Data
	log  *log.Helper
}

// NewMigrator creates a new Migrator instance
func NewMigrator(data *Data, logger log.Logger) *Migrator {
	return &Migrator{
		data: data,
		log:  log.NewHelper(logger),
	}
}

// AutoMigrate runs all SQL migration files from the migrations directory
func (m *Migrator) AutoMigrate(ctx context.Context, migrationsDir string) error {
	m.log.Info("Starting auto migration...")

	// Get all SQL files in the migrations directory
	files, err := m.getMigrationFiles(migrationsDir)
	if err != nil {
		return fmt.Errorf("failed to get migration files: %w", err)
	}

	if len(files) == 0 {
		m.log.Info("No migration files found")
		return nil
	}

	m.log.Infof("Found %d migration file(s)", len(files))

	// Execute each migration file
	for _, file := range files {
		if err := m.executeMigrationFile(ctx, file); err != nil {
			return fmt.Errorf("failed to execute migration %s: %w", filepath.Base(file), err)
		}
		m.log.Infof("Migration completed: %s", filepath.Base(file))
	}

	m.log.Info("Auto migration completed successfully")
	return nil
}

// getMigrationFiles returns sorted list of SQL migration files
func (m *Migrator) getMigrationFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".sql") {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}

	// Sort files by name to ensure order (001_, 002_, etc.)
	sort.Strings(files)
	return files, nil
}

// executeMigrationFile executes a single SQL migration file
func (m *Migrator) executeMigrationFile(ctx context.Context, filePath string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Split the content into individual statements
	statements := m.splitStatements(string(content))

	for i, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}

		// Skip comments-only statements
		if m.isCommentOnly(stmt) {
			continue
		}

		if err := m.data.ExecuteExec(ctx, stmt); err != nil {
			// Log the error but continue if it's a "table already exists" type error
			errStr := err.Error()
			if strings.Contains(errStr, "already exists") ||
				strings.Contains(errStr, "ALREADY_EXISTS") ||
				strings.Contains(errStr, "index already exists") {
				m.log.Debugf("Skipping (already exists): statement #%d", i+1)
				continue
			}
			return fmt.Errorf("statement #%d failed: %w", i+1, err)
		}
	}

	return nil
}

// splitStatements splits SQL content into individual statements
func (m *Migrator) splitStatements(content string) []string {
	var statements []string
	var currentStmt strings.Builder

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		trimmedLine := strings.TrimSpace(line)

		// Skip empty lines and full-line comments when building statements
		if trimmedLine == "" {
			continue
		}

		currentStmt.WriteString(line)
		currentStmt.WriteString("\n")

		// Check if line ends with semicolon (end of statement)
		if strings.HasSuffix(trimmedLine, ";") {
			stmt := strings.TrimSpace(currentStmt.String())
			if stmt != "" && !m.isCommentOnly(stmt) {
				statements = append(statements, stmt)
			}
			currentStmt.Reset()
		}
	}

	// Add any remaining statement
	if currentStmt.Len() > 0 {
		stmt := strings.TrimSpace(currentStmt.String())
		if stmt != "" && !m.isCommentOnly(stmt) {
			statements = append(statements, stmt)
		}
	}

	return statements
}

// isCommentOnly checks if a statement is only comments
func (m *Migrator) isCommentOnly(stmt string) bool {
	lines := strings.Split(stmt, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "--") && !strings.HasPrefix(line, "/*") {
			return false
		}
	}
	return true
}
