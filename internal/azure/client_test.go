// internal/azure/client_test.go
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

// mockTokenCredential implements the TokenCredential interface
type mockTokenCredential struct {
	token string
	err   error
}

func (m *mockTokenCredential) GetToken(
	ctx context.Context,
	options policy.TokenRequestOptions,
) (azcore.AccessToken, error) {
	return azcore.AccessToken{
		Token:     m.token,
		ExpiresOn: time.Now().Add(time.Hour),
	}, m.err
}

func skipIfIntegrationDisabled(t *testing.T) {
	if os.Getenv("AZURE_INTEGRATION") == "" {
		t.Skip("Set AZURE_INTEGRATION=1 to run this test")
	}
}

func TestNewGraphClient(t *testing.T) {
	skipIfIntegrationDisabled(t)
	t.Run("valid credentials", func(t *testing.T) {
		client := NewGraphClient("tenant", "client", "secret")
		require.NotNil(t, client, "Client should be created")
	})

	t.Run("invalid credentials panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic with invalid credentials")
			}
		}()
		_ = NewGraphClient("", "", "")
	})
}

func TestUserExists(t *testing.T) {
	skipIfIntegrationDisabled(t)
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Validate Authorization header first
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// Then handle other cases
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

	mockCred := &mockTokenCredential{token: "test-token"}

	tests := []struct {
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &GraphClient{
				cred: mockCred,
			}

			origClient := http.DefaultClient
			http.DefaultClient = testServer.Client()
			defer func() { http.DefaultClient = origClient }()

			origUserURL := userURLFormat
			userURLFormat = testServer.URL + "/v1.0/users/%s"
			defer func() { userURLFormat = origUserURL }()

			exists, err := client.UserExists(context.Background(), tt.email)

			if tt.expectError {
				require.Error(t, err, "Expected error")
				return
			}

			require.NoError(t, err, "Unexpected error")
			require.Equal(t, tt.wantExists, exists, "Existence mismatch")
		})
	}
}

func TestTokenAcquisitionError(t *testing.T) {
	skipIfIntegrationDisabled(t)
	mockCred := &mockTokenCredential{
		err: fmt.Errorf("token acquisition failed"),
	}

	client := &GraphClient{
		cred: mockCred,
	}

	_, err := client.UserExists(context.Background(), "any@example.com")
	require.Error(t, err, "Should return token error")
	require.Contains(t, err.Error(), "failed to get access token", "Error message mismatch")
}

func TestNetworkError(t *testing.T) {
	skipIfIntegrationDisabled(t)
	mockCred := &mockTokenCredential{token: "test-token"}
	client := &GraphClient{cred: mockCred}

	origUserURL := userURLFormat
	userURLFormat = "http://invalid.invalid/%s"
	defer func() { userURLFormat = origUserURL }()

	_, err := client.UserExists(context.Background(), "test@example.com")
	require.Error(t, err, "Expected network error")
}

var userURLFormat = "https://graph.microsoft.com/v1.0/users/%s"
