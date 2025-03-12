package graph

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

// GraphClient is a wrapper around Azure AD Graph API authentication.
type GraphClient struct {
	cred *azidentity.ClientSecretCredential // Azure AD credentials for authentication
}

// NewGraphClient initializes a new GraphClient instance.
func NewGraphClient(cred *azidentity.ClientSecretCredential) *GraphClient {
	return &GraphClient{cred: cred}
}

// UserExists checks whether a given user (by UPN/email) exists in Azure AD.
func (g *GraphClient) UserExists(ctx context.Context, upn string) (bool, error) {
	// Ensure only @statcan.gc.ca email addresses are checked
	if !strings.HasSuffix(strings.ToLower(upn), "@statcan.gc.ca") {
		return false, nil // Automatically return false for non-StatCan users
	}

	// Obtain an access token for Microsoft Graph API
	token, err := g.cred.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{"https://graph.microsoft.com/.default"},
	})
	if err != nil {
		return false, fmt.Errorf("failed to get token: %w", err)
	}

	// Encode the UPN to safely include it in the URL
	escapedUPN := url.PathEscape(upn)
	url := fmt.Sprintf("https://graph.microsoft.com/v1.0/users/%s", escapedUPN)

	// Create an HTTP GET request to query user existence
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.Token)

	// Execute the request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Handle response codes
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil // User exists
	case http.StatusNotFound:
		return false, nil // User does not exist
	default:
		return false, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
}
