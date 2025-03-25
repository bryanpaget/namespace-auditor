package azure

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

// TokenCredential defines the interface required for Azure token acquisition.
// This matches the azcore.TokenCredential interface from the Azure SDK.
type TokenCredential interface {
	GetToken(ctx context.Context, options policy.TokenRequestOptions) (azcore.AccessToken, error)
}

// GraphClient provides authentication and operations for Microsoft Graph API.
// Handles token acquisition and user existence checks.
type GraphClient struct {
	cred TokenCredential // Azure authentication credential
}

// NewGraphClient creates a new authenticated client for Microsoft Graph API.
// Uses client secret credentials for authentication.
//
// Parameters:
// - tenantID: Azure AD tenant ID (directory ID)
// - clientID: Application client ID
// - clientSecret: Client secret value
//
// Panics if credential creation fails to ensure invalid configurations fail fast.
func NewGraphClient(tenantID, clientID, clientSecret string) *GraphClient {
	cred, err := azidentity.NewClientSecretCredential(
		tenantID,
		clientID,
		clientSecret,
		nil, // Optional configuration
	)
	if err != nil {
		panic(fmt.Sprintf("Failed to create Azure credentials: %v", err))
	}
	return &GraphClient{cred: cred}
}

// UserExists checks if a user exists in Azure Active Directory.
// Performs a lookup using Microsoft Graph API with proper authentication.
//
// Parameters:
// - ctx: Context for cancellation and timeouts
// - email: User principal name or email address to verify
//
// Returns:
// - bool: True if user exists
// - error: Authentication, network, or API errors
//
// Note: Handles Microsoft Graph API response codes:
// - 200 OK: User exists
// - 404 Not Found: User doesn't exist
// - Other status codes: Returned as errors
func (g *GraphClient) UserExists(ctx context.Context, email string) (bool, error) {
	// Acquire OAuth2 token for Microsoft Graph API
	token, err := g.cred.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{"https://graph.microsoft.com/.default"},
	})
	if err != nil {
		return false, fmt.Errorf("failed to get access token: %w", err)
	}

	// Safely construct user lookup URL
	escapedEmail := url.PathEscape(email) // Prevent injection/encoding issues
	userURL := fmt.Sprintf("https://graph.microsoft.com/v1.0/users/%s", escapedEmail)

	// Create authenticated HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", userURL, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.Token)

	// Execute API request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close() // Ensure response body cleanup

	// Interpret API response
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil // Valid user found
	case http.StatusNotFound:
		return false, nil // User not found
	default:
		// Handle unexpected responses
		return false, fmt.Errorf("unexpected API response: %d %s",
			resp.StatusCode, http.StatusText(resp.StatusCode))
	}
}
