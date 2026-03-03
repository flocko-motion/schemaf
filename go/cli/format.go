package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Color codes for terminal output
const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
	ColorGray   = "\033[90m"
)

// Print prints a formatted message
func Print(format string, args ...interface{}) {
	fmt.Printf(format+"\n", args...)
}

// Success prints a success message in green
func Success(format string, args ...interface{}) {
	fmt.Printf("%s✓%s %s\n", ColorGreen, ColorReset, fmt.Sprintf(format, args...))
}

// Error prints an error message in red to stderr
func Error(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "%s✗%s %s\n", ColorRed, ColorReset, fmt.Sprintf(format, args...))
}

// Errorf prints an error object in red to stderr
func Errorf(err error) {
	fmt.Fprintf(os.Stderr, "%s✗%s %v\n", ColorRed, ColorReset, err)
}

// Fatalf prints an error message in red to stderr and exits with code 1
func Fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "%s✗%s %s\n", ColorRed, ColorReset, fmt.Sprintf(format, args...))
	os.Exit(1)
}

// Warning prints a warning message in yellow
func Warning(format string, args ...interface{}) {
	fmt.Printf("%s⚠%s %s\n", ColorYellow, ColorReset, fmt.Sprintf(format, args...))
}

// Info prints an info message in blue
func Info(format string, args ...interface{}) {
	fmt.Printf("%sℹ%s %s\n", ColorBlue, ColorReset, fmt.Sprintf(format, args...))
}

// JSON prints a value as JSON (pretty-printed by default)
func JSON(v interface{}, pretty bool) error {
	var data []byte
	var err error

	if pretty {
		data, err = json.MarshalIndent(v, "", "  ")
	} else {
		data, err = json.Marshal(v)
	}

	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	fmt.Println(string(data))
	return nil
}

// Table prints a simple table
func Table(headers []string, rows [][]string) {
	// Calculate column widths
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Print header
	for i, h := range headers {
		fmt.Printf("%-*s  ", widths[i], h)
	}
	fmt.Println()

	// Print separator
	for _, w := range widths {
		fmt.Print(strings.Repeat("-", w) + "  ")
	}
	fmt.Println()

	// Print rows
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) {
				fmt.Printf("%-*s  ", widths[i], cell)
			}
		}
		fmt.Println()
	}
}

// KeyValue prints key-value pairs
func KeyValue(pairs map[string]string) {
	maxKeyLen := 0
	for key := range pairs {
		if len(key) > maxKeyLen {
			maxKeyLen = len(key)
		}
	}

	for key, value := range pairs {
		fmt.Printf("  %-*s  %s\n", maxKeyLen, key+":", value)
	}
}
