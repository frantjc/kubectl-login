package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	xerrors "github.com/frantjc/x/errors"
	xos "github.com/frantjc/x/os"
	"github.com/golang-jwt/jwt/v5"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cliauthv1 "k8s.io/client-go/pkg/apis/clientauthentication/v1"
	authexec "k8s.io/client-go/tools/auth/exec"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	err := xerrors.Ignore(newKubectlLogin().ExecuteContext(ctx), context.Canceled)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
	}

	stop()
	xos.ExitFromError(err)
}

const (
	EnvVarActionsIDTokenRequestURL   = "ACTIONS_ID_TOKEN_REQUEST_URL"
	EnvVarActionsIDTokenRequestToken = "ACTIONS_ID_TOKEN_REQUEST_TOKEN"
	EnvVarGitHubActions              = "GITHUB_ACTIONS"
)

func isGitHubActions() bool {
	githubActions, _ := strconv.ParseBool(os.Getenv(EnvVarGitHubActions))
	return githubActions
}

func addMask(w io.Writer, secret string) error {
	if _, err := fmt.Fprintf(w, "::add-mask::%s\n", secret); err != nil {
		return err
	}
	return nil
}

type Token jwt.Token

var (
	_ json.Unmarshaler = new(Token)
)

// UnmarshalJSON implements [json.Unmarshaler].
func (t *Token) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	token, _, err := jwt.NewParser().ParseUnverified(s, jwt.MapClaims{})
	if err != nil {
		return err
	}

	*t = Token(*token)

	return nil
}

var (
	_ json.Marshaler = Token{}
)

// MarshalJSON implements [json.Marshaler].
func (t Token) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.Raw)
}

type ActionsIDTokenResponseBody struct {
	Value *Token `json:"value"`
}

type CacheEntry struct {
	Token *Token    `json:"token"`
	Exp   time.Time `json:"exp,omitempty"`
}

type Cache interface {
	Store(*CacheEntry) error
	Load() (*CacheEntry, bool, error)
}

type CacheFile string

var (
	_ Cache = CacheFile("")
)

func (c CacheFile) String() string {
	return string(c)
}

// Load implements [Cache].
func (c CacheFile) Load() (*CacheEntry, bool, error) {
	rawCacheEntry, err := os.ReadFile(c.String())
	if err != nil {
		return nil, false, xerrors.Ignore(err, os.ErrNotExist)
	}
	ce := &CacheEntry{}
	if err := json.Unmarshal(rawCacheEntry, ce); err != nil {
		return nil, false, nil
	}
	if time.Now().After(ce.Exp) {
		return nil, false, nil
	}
	return ce, true, nil
}

// Store implements [Cache].
func (c CacheFile) Store(ce *CacheEntry) error {
	rawCacheEntry, err := json.Marshal(ce)
	if err != nil {
		return err
	}
	return os.WriteFile(c.String(), rawCacheEntry, 0600)
}

func NewCacheFile() (Cache, error) {
	home := os.Getenv("HOME")
	if home == "" {
		u, err := user.Current()
		if err != nil {
			return nil, err
		}

		home = u.HomeDir
	}

	cacheDirPath := filepath.Join(home, ".kube", "cache", "login")
	if err := os.MkdirAll(cacheDirPath, 0700); err != nil {
		return nil, err
	}

	cacheFilePath := filepath.Join(cacheDirPath, "token")

	return CacheFile(cacheFilePath), nil
}

func newKubectlLogin() *cobra.Command {
	var (
		audience string
		cmd      = &cobra.Command{
			Use:           "kubectl-login",
			Version:       semVer(),
			SilenceErrors: true,
			SilenceUsage:  true,
			RunE: func(cmd *cobra.Command, args []string) error {
				ctx := cmd.Context()
				token := new(Token)
				cache, err := NewCacheFile()
				if err != nil {
					return err
				}

				hit := false

				if ce, ok, err := cache.Load(); err != nil {
					return err
				} else if ok {
					hit = true
					token = ce.Token
				} else if isGitHubActions() {
					rawActionsIDTokenRequestURL := os.Getenv(EnvVarActionsIDTokenRequestURL)
					if rawActionsIDTokenRequestURL == "" {
						return fmt.Errorf("env var %s is unset - did you give id-token write permissions to your GitHub Actions workflow?", EnvVarActionsIDTokenRequestURL)
					}

					actionsIDTokenRequestURL, err := url.Parse(rawActionsIDTokenRequestURL)
					if err != nil {
						return fmt.Errorf("env var %s is not a valid url - are you running in GitHub Actions?", EnvVarActionsIDTokenRequestURL)
					}

					if audience != "" {
						actionsIDTokenRequestURL.RawQuery = url.Values{"audience": []string{audience}}.Encode()
					}

					idToken := os.Getenv(EnvVarActionsIDTokenRequestToken)
					if idToken == "" {
						return fmt.Errorf("env var %s is unset - are you running in GitHub Actions?", EnvVarActionsIDTokenRequestToken)
					}

					if err := addMask(cmd.ErrOrStderr(), idToken); err != nil {
						return err
					}

					req, err := http.NewRequestWithContext(ctx, http.MethodGet, actionsIDTokenRequestURL.String(), nil)
					if err != nil {
						return err
					}
					req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", idToken))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("Accept", "application/json")

					res, err := http.DefaultClient.Do(req)
					if err != nil {
						return err
					}
					defer res.Body.Close()

					if res.StatusCode != http.StatusOK {
						return fmt.Errorf("received HTTP status %d %s", res.StatusCode, res.Status)
					}

					body := ActionsIDTokenResponseBody{}
					if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
						return err
					}

					if err := addMask(cmd.ErrOrStderr(), body.Value.Raw); err != nil {
						return err
					}

					token = body.Value
				} else {
					rawIDToken, err := exec.CommandContext(ctx, "aws",
						"sts", "get-web-identity-token",
						"--audience", audience,
						"--signing-algorithm", "RS256",
						"--query", "WebIdentityToken",
						"--output", "text",
					).
						Output()
					if err != nil {
						return err
					}

					value, _, err := jwt.NewParser().ParseUnverified(string(bytes.TrimSpace(rawIDToken)), jwt.MapClaims{})
					if err != nil {
						return err
					}

					token = (*Token)(value)
				}

				claims, ok := token.Claims.(jwt.MapClaims)
				if !ok {
					return fmt.Errorf("get token claims")
				}

				exp, ok := claims["exp"].(float64)
				if !ok {
					return fmt.Errorf("get token exp")
				}

				expirationTimestamp := time.Unix(int64(exp), 0)

				if !hit {
					if err := cache.Store(&CacheEntry{
						Token: token,
						Exp:   expirationTimestamp,
					}); err != nil {
						return err
					}
				}

				obj, _, err := authexec.LoadExecCredentialFromEnv()
				if err != nil {
					obj = &cliauthv1.ExecCredential{
						TypeMeta: metav1.TypeMeta{
							APIVersion: cliauthv1.SchemeGroupVersion.String(),
							Kind:       "ExecCredential",
						},
					}
				}

				gvk := obj.GetObjectKind().GroupVersionKind()

				execCredential := &cliauthv1.ExecCredential{
					TypeMeta: metav1.TypeMeta{
						APIVersion: gvk.GroupVersion().String(),
						Kind:       gvk.Kind,
					},
					Status: &cliauthv1.ExecCredentialStatus{
						Token:               token.Raw,
						ExpirationTimestamp: &metav1.Time{Time: expirationTimestamp},
					},
				}

				p, err := json.MarshalIndent(execCredential, "", "  ")
				if err != nil {
					return err
				}

				if _, err = cmd.OutOrStdout().Write(p); err != nil {
					return err
				}

				return nil
			},
		}
	)
	cmd.PersistentFlags().BoolP("help", "h", false, "Help for "+cmd.Name())
	cmd.Flags().Bool("version", false, "Version for "+cmd.Name())
	cmd.SetVersionTemplate("{{ .Name }}{{ .Version }}")

	cmd.Flags().StringVarP(&audience, "audience", "a", "", "Token audience")
	_ = cmd.MarkFlagRequired("audience")

	return cmd
}
