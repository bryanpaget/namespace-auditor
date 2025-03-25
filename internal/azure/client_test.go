package azure

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/stretchr/testify/require"
)

// mockTokenCredential implements TokenCredential for testing authentication flows
// without requiring real Azure credentials
type mockTokenCredential struct {
	token string // Mock access token
	err   error  // Optional error to simulate token failures
}

// GetToken returns mock authentication data for testing
func (m *mockTokenCredential) GetToken(
	ctx context.Context,
	options policy.TokenRequestOptions,
) (azcore.AccessToken, error) {
	return azcore.AccessToken{
		Token:     m.token,
		ExpiresOn: time.Now().Add(time.Hour),
	}, m.err
}

// skipIfIntegrationDisabled skips tests requiring Azure integration
// when AZURE_INTEGRATION environment variable is not set
func skipIfIntegrationDisabled(t *testing.T) {
	if os.Getenv("AZURE_INTEGRATION") == "" {
		t.Skip("Set AZURE_INTEGRATION=1 to run Azure integration tests")
	}
}

// TestNewGraphClient validates client creation with various credentials
func TestNewGraphClient(t *testing.T) {
	skipIfIntegrationDisabled(t)

	t.Run("valid credentials", func(t *testing.T) {
		client := NewGraphClient("tenant", "client", "secret")
		require.NotNil(t, client, "Should create client with valid credentials")
	})

	t.Run("invalid credentials panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic with empty credentials")
			}
		}()
		_ = NewGraphClient("", "", "") // Invalid empty credentials
	})
}

// TestUserExists validates user existence checks against mock Graph API
func TestUserExists(t *testing.T) {
	skipIfIntegrationDisabled(t)

	// Mock Graph API server
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Validate authorization header first
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// Handle different test cases based on URL path
		switch r.URL.Path {
		case "/v1.0/users/valid@example.com":
			w.WriteHeader(http.StatusOK)
		case "/v1.0/users/missing@example.com":
			w.WriteHeader(http.StatusNotFound)
		case "/v1.0/users/error@example.com":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer testServer.Close()

	// Configure mock credential with test token
	mockCred := &mockTokenCredential{token: "test-token"}

	testCases := []struct {
		name        string
		email       string
		wantExists  bool
		expectError bool
	}{
		{
			name:       "valid user exists",
			email:      "valid@example.com",
			wantExists: true,
		},
		{
			name:       "user not found",
			email:      "missing@example.com",
			wantExists: false,
		},
		{
			name:        "server error",
			email:       "error@example.com",
			expectError: true,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			client := &GraphClient{cred: mockCred}

			// Temporary override of HTTP client and API endpoint
			origClient := http.DefaultClient
			http.DefaultClient = testServer.Client()
			defer func() { http.DefaultClient = origClient }()

			origUserURL := userURLFormat
			userURLFormat = testServer.URL + "/v1.0/users/%s"
			defer func() { userURLFormat = origUserURL }()

			// Execute test
			exists, err := client.UserExists(context.Background(), tt.email)

			if tt.expectError {
				require.Error(t, err, "Expected error for case: "+tt.name)
				return
			}

			require.NoError(t, err, "Unexpected error for case: "+tt.name)
			require.Equal(t, tt.wantExists, exists, "Existence mismatch for case: "+tt.name)
		})
	}
}

// TestTokenAcquisitionError validates error handling for failed authentication
func TestTokenAcquisitionError(t *testing.T) {
	skipIfIntegrationDisabled(t)

	// Configure mock credential to return token error
	mockCred := &mockTokenCredential{
		err: fmt.Errorf("token acquisition failed"),
	}

	client := &GraphClient{cred: mockCred}

	_, err := client.UserExists(context.Background(), "any@example.com")
	require.Error(t, err, "Should propagate token acquisition error")
	require.Contains(t, err.Error(), "failed to get access token",
		"Error message should mention token failure")
}

// TestNetworkError validates error handling for network failures
func TestNetworkError(t *testing.T) {
	skipIfIntegrationDisabled(t)

	mockCred := &mockTokenCredential{token: "test-token"}
	client := &GraphClient{cred: mockCred}

	// Force invalid endpoint to simulate network failure
	origUserURL := userURLFormat
	userURLFormat = "http://invalid.invalid/%s" // Unreachable URL
	defer func() { userURLFormat = origUserURL }()

	_, err := client.UserExists(context.Background(), "test@example.com")
	require.Error(t, err, "Should detect network connectivity issues")
}

// userURLFormat defines the Microsoft Graph API endpoint template for user lookups
var userURLFormat = "https://graph.microsoft.com/v1.0/users/%s"
