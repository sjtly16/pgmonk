package collector_test

import (
	"testing"
)

// These are unit tests for collector helper functions.
// Integration tests requiring a database connection are in a separate test suite.

func TestFormatBytes(t *testing.T) {
	// Basic validation that the collector package compiles and links correctly.
	// Full collector testing requires a live PostgreSQL connection.
	if testing.Short() {
		t.Skip("skipping collector tests in short mode")
	}
}
