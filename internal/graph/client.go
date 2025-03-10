package graph

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

type GraphClient struct {
	cred *azidentity.ClientSecretCredential
}

func NewGraphClient(cred *azidentity.ClientSecretCredential) *GraphClient {
	return &GraphClient{cred: cred}
}

func (g *GraphClient) UserExists(ctx context.Context, upn string) (bool, error) {
	token, err := g.cred.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{"https://graph.microsoft.com/.default"},
	})
	if err != nil {
		return false, fmt.Errorf("failed to get token: %w", err)
	}

	escapedUPN := url.PathEscape(upn)
	url := fmt.Sprintf("https://graph.microsoft.com/v1.0/users/%s", escapedUPN)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
}
