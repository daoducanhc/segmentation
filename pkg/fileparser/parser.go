// Package fileparser provides utilities for parsing CSV/Excel files.
package fileparser

import (
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"fmt"
	"io"
	"strings"

	"github.com/xuri/excelize/v2"
)

// ParseResult contains the result of parsing a file.
type ParseResult struct {
	UserIDs      []string
	Skipped      int
	Errors       []string
	DetectedType string
}

// ParseFile parses a base64 encoded file (CSV or Excel) and extracts user IDs from specified column
// fileName: file name with extension (e.g. "users.csv", "data.xlsx")
// headerName: required, the column name containing user IDs
func ParseFile(fileContent, fileName, headerName string) (*ParseResult, error) {
	if fileContent == "" {
		return nil, fmt.Errorf("file content is empty")
	}
	if headerName == "" {
		return nil, fmt.Errorf("header_name is required")
	}

	// Decode base64
	decoded, err := base64.StdEncoding.DecodeString(fileContent)
	if err != nil {
		// Try as raw content (not base64)
		decoded = []byte(fileContent)
	}

	// Get file type from extension
	fileType := getFileTypeFromName(fileName)

	var result *ParseResult

	switch fileType {
	case "csv":
		result, err = parseCSV(decoded, headerName)
		if result != nil {
			result.DetectedType = "csv"
		}
	case "xlsx":
		result, err = parseExcel(decoded, headerName)
		if result != nil {
			result.DetectedType = "xlsx"
		}
	default:
		return nil, fmt.Errorf("unsupported file type: %s (supported: .csv, .xlsx)", fileName)
	}

	return result, err
}

// getFileTypeFromName extracts file type from filename extension
func getFileTypeFromName(fileName string) string {
	fileName = strings.ToLower(fileName)

	if strings.HasSuffix(fileName, ".csv") {
		return "csv"
	}
	if strings.HasSuffix(fileName, ".xlsx") || strings.HasSuffix(fileName, ".xls") {
		return "xlsx"
	}

	return ""
}

// parseCSV parses a CSV file and extracts user IDs from the specified column
func parseCSV(data []byte, headerName string) (*ParseResult, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = -1 // Allow variable field count
	reader.LazyQuotes = true    // Be lenient with quotes

	// Read header row
	headers, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV header: %w", err)
	}

	// Find column index (case-insensitive)
	columnIndex := -1
	for i, h := range headers {
		if strings.EqualFold(strings.TrimSpace(h), strings.TrimSpace(headerName)) {
			columnIndex = i
			break
		}
	}
	if columnIndex == -1 {
		return nil, fmt.Errorf("column '%s' not found in CSV headers: %v", headerName, headers)
	}

	result := &ParseResult{
		UserIDs: make([]string, 0),
		Errors:  make([]string, 0),
	}

	// Track seen user IDs to avoid duplicates
	seen := make(map[string]bool)
	lineNum := 1 // Start after header

	for {
		lineNum++
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: %v", lineNum, err))
			result.Skipped++
			continue
		}

		if columnIndex >= len(record) {
			result.Skipped++
			continue
		}

		userID := strings.TrimSpace(record[columnIndex])
		if userID == "" {
			result.Skipped++
			continue
		}

		if seen[userID] {
			result.Skipped++
			continue
		}

		seen[userID] = true
		result.UserIDs = append(result.UserIDs, userID)
	}

	return result, nil
}

// parseExcel parses an Excel file and extracts user IDs from the specified column
func parseExcel(data []byte, headerName string) (*ParseResult, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to open Excel file: %w", err)
	}
	defer f.Close()

	// Get the first sheet
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("Excel file has no sheets")
	}
	sheetName := sheets[0]

	// Get all rows
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("failed to read Excel rows: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("Excel sheet is empty")
	}

	// Find column index from header row (case-insensitive)
	headers := rows[0]
	columnIndex := -1
	for i, h := range headers {
		if strings.EqualFold(strings.TrimSpace(h), strings.TrimSpace(headerName)) {
			columnIndex = i
			break
		}
	}
	if columnIndex == -1 {
		return nil, fmt.Errorf("column '%s' not found in Excel headers: %v", headerName, headers)
	}

	result := &ParseResult{
		UserIDs: make([]string, 0),
		Errors:  make([]string, 0),
	}

	// Track seen user IDs to avoid duplicates
	seen := make(map[string]bool)

	// Process data rows (skip header)
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if columnIndex >= len(row) {
			result.Skipped++
			continue
		}

		userID := strings.TrimSpace(row[columnIndex])
		if userID == "" {
			result.Skipped++
			continue
		}

		if seen[userID] {
			result.Skipped++
			continue
		}

		seen[userID] = true
		result.UserIDs = append(result.UserIDs, userID)
	}

	return result, nil
}

// ParseUserIDList cleans and deduplicates a list of user IDs
func ParseUserIDList(userIDs []string) *ParseResult {
	result := &ParseResult{
		UserIDs: make([]string, 0),
		Errors:  make([]string, 0),
	}

	seen := make(map[string]bool)
	for _, id := range userIDs {
		userID := strings.TrimSpace(id)
		if userID == "" {
			result.Skipped++
			continue
		}
		if seen[userID] {
			result.Skipped++
			continue
		}
		seen[userID] = true
		result.UserIDs = append(result.UserIDs, userID)
	}

	return result
}
