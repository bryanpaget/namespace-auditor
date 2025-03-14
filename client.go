package main

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
	token, err := g.cred.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{"https://graph.microsoft.com/.default"},
	})
	if err != nil {
		return false, fmt.Errorf("failed to get token: %w", err)
	}

	escapedEmail := url.PathEscape(email)
	url := fmt.Sprintf("https://graph.microsoft.com/v1.0/users/%s", escapedEmail)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("request failed: %w", err)
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
