// internal/azure/client.go
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

// TokenCredential is the interface we require
type TokenCredential interface {
	GetToken(ctx context.Context, options policy.TokenRequestOptions) (azcore.AccessToken, error)
}

// GraphClient encapsulates the Azure client credentials
type GraphClient struct {
	cred TokenCredential
}

// NewGraphClient creates a new GraphClient with ClientSecretCredential
func NewGraphClient(tenantID, clientID, clientSecret string) *GraphClient {
	cred, err := azidentity.NewClientSecretCredential(
		tenantID,
		clientID,
		clientSecret,
		nil,
	)
	if err != nil {
		panic(fmt.Sprintf("Failed to create Azure credentials: %v", err))
	}
	return &GraphClient{cred: cred}
}

func (g *GraphClient) UserExists(ctx context.Context, email string) (bool, error) {
	// Get OAuth2 token for Microsoft Graph API
	token, err := g.cred.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{"https://graph.microsoft.com/.default"},
	})
	if err != nil {
		return false, fmt.Errorf("failed to get access token: %w", err)
	}

	// Construct user lookup URL with proper escaping
	escapedEmail := url.PathEscape(email) // Prevent URL injection/formatting issues
	userURL := fmt.Sprintf("https://graph.microsoft.com/v1.0/users/%s", escapedEmail)

	// Create HTTP request with authentication header
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
	defer resp.Body.Close()

	// Handle response status codes
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil // User exists
	case http.StatusNotFound:
		return false, nil // User not found
	default:
		// Handle unexpected status codes
		return false, fmt.Errorf("unexpected API response: %d %s",
			resp.StatusCode, http.StatusText(resp.StatusCode))
	}
}
