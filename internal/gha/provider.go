package gha

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"

	kubectllogin "github.com/frantjc/kubectl-login"
)

func Register(w io.Writer, httpClient *http.Client) {
	kubectllogin.RegisterProvider(&Provider{w, httpClient}, "gha")
}

type Provider struct {
	Writer     io.Writer
	HTTPClient *http.Client
}

var (
	_ kubectllogin.Provider = new(Provider)
)

const (
	EnvVarActionsIDTokenRequestURL   = "ACTIONS_ID_TOKEN_REQUEST_URL"
	EnvVarActionsIDTokenRequestToken = "ACTIONS_ID_TOKEN_REQUEST_TOKEN"
	EnvVarGitHubActions              = "GITHUB_ACTIONS"
)

func (p *Provider) AddMask(secret string) error {
	if p.Writer == nil {
		return nil
	}
	_, err := fmt.Fprintf(p.Writer, "::add-mask::%s\n", secret)
	return err
}

// Available implements [kubectllogin.Provider].
func (p *Provider) Available(context.Context) (bool, error) {
	if githubActions, err := strconv.ParseBool(os.Getenv(EnvVarGitHubActions)); err != nil {
		return false, err
	} else if !githubActions {
		return false, fmt.Errorf("env var %s is not true", EnvVarGitHubActions)
	}

	rawActionsIDTokenRequestURL := os.Getenv(EnvVarActionsIDTokenRequestURL)
	if rawActionsIDTokenRequestURL == "" {
		return false, fmt.Errorf("env var %s is unset - did you give id-token write permissions to your GitHub Actions workflow?", EnvVarActionsIDTokenRequestURL)
	}

	if _, err := url.Parse(rawActionsIDTokenRequestURL); err != nil {
		return false, fmt.Errorf("env var %s is not a valid URL", EnvVarActionsIDTokenRequestURL)
	}

	if idToken := os.Getenv(EnvVarActionsIDTokenRequestToken); idToken == "" {
		return false, fmt.Errorf("env var %s is not set", EnvVarActionsIDTokenRequestToken)
	} else {
		_ = p.AddMask(idToken)
	}

	return true, nil
}

type ActionsIDTokenResponseBody struct {
	Value *kubectllogin.Token `json:"value"`
}

// Token implements [kubectllogin.Provider].
func (p *Provider) Token(ctx context.Context, audience string) (*kubectllogin.Token, error) {
	actionsIDTokenRequestURL, err := url.Parse(os.Getenv(EnvVarActionsIDTokenRequestURL))
	if err != nil {
		return nil, fmt.Errorf("env var %s is not a valid URL", EnvVarActionsIDTokenRequestURL)
	}

	actionsIDTokenRequestURL.RawQuery = url.Values{"audience": []string{audience}}.Encode()

	idToken := os.Getenv(EnvVarActionsIDTokenRequestToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, actionsIDTokenRequestURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", idToken))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	httpClient := p.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received HTTP status %d %s from %s", res.StatusCode, res.Status, EnvVarActionsIDTokenRequestURL)
	}

	body := ActionsIDTokenResponseBody{}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil || body.Value == nil {
		return nil, fmt.Errorf("received malformed response body from %s", EnvVarActionsIDTokenRequestURL)
	}

	_ = p.AddMask(body.Value.Raw)

	return body.Value, nil
}
