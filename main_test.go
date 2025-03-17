// main_test.go
package main

import (
	"testing"
	"time"
)

func TestIsValidDomain(t *testing.T) {
	tests := []struct {
		email   string
		domains []string
		want    bool
	}{
		{"user@statcan.gc.ca", []string{"statcan.gc.ca"}, true},
		{"user@cloud.statcan.ca", []string{"statcan.gc.ca", "cloud.statcan.ca"}, true},
		{"invalid@example.com", []string{"statcan.gc.ca"}, false},
	}

	for _, tt := range tests {
		got := isValidDomain(tt.email, tt.domains)
		if got != tt.want {
			t.Errorf("isValidDomain(%q) = %v, want %v", tt.email, got, tt.want)
		}
	}
}

func TestMustParseDuration(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"1h", time.Hour},
		{"720h", 720 * time.Hour},
	}

	for _, tt := range tests {
		got := mustParseDuration(tt.input)
		if got != tt.want {
			t.Errorf("mustParseDuration(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
