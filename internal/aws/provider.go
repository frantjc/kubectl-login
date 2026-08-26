package aws

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"

	"github.com/Masterminds/semver"
	kubectllogin "github.com/frantjc/kubectl-login"
	"github.com/golang-jwt/jwt/v5"
)

func Register() {
	kubectllogin.RegisterProvider(new(Provider), "aws")
}

type Provider struct{}

var (
	_ kubectllogin.Provider = new(Provider)
)

var (
	awsCliVersionRegexp = regexp.MustCompile(`aws-cli/([0-9]+(?:\.[0-9]+)+)`)
)

// Available implements [kubectllogin.Provider].
func (p *Provider) Available(ctx context.Context) (bool, error) {
	if _, err := exec.LookPath("aws"); err != nil {
		return false, fmt.Errorf("look path aws: %w", err)
	}

	rawVersion, err := exec.CommandContext(ctx, "aws", "--version").Output()
	if err != nil {
		return false, fmt.Errorf("get aws version: %w", err)
	}

	matches := awsCliVersionRegexp.FindStringSubmatch(string(rawVersion))
	if lenMatches := len(matches); lenMatches < 2 {
		return false, fmt.Errorf("parse aws-cli version")
	}

	rawAwsCliVersion := matches[1]

	awsCliVersion, err := semver.NewVersion(rawAwsCliVersion)
	if err != nil {
		return false, fmt.Errorf("parse aws-cli version")
	}

	minAwsCliVersion, err := semver.NewVersion("2.32.5")
	if err != nil {
		return false, fmt.Errorf("parse minimum aws-cli version")
	}

	if minVersionMet := awsCliVersion.GreaterThan(minAwsCliVersion) || awsCliVersion.Equal(minAwsCliVersion); !minVersionMet {
		return false, fmt.Errorf("requires aws-cli version >= %s", minAwsCliVersion.String())
	}

	return true, nil
}

// Token implements [kubectllogin.Provider].
func (p *Provider) Token(ctx context.Context, audience string) (*kubectllogin.Token, error) {
	cmd := exec.CommandContext(ctx, "aws",
		"sts", "get-web-identity-token",
		"--audience", audience,
		"--signing-algorithm", "RS256",
		"--query", "WebIdentityToken",
		"--output", "text",
	)

	rawIDToken, err := cmd.Output()
	if exitErr := new(exec.ExitError{}); errors.As(err, &exitErr) {
		return nil, fmt.Errorf("aws exited %d: %s", exitErr.ExitCode(), string(bytes.TrimSpace(exitErr.Stderr)))
	} else if err != nil {
		return nil, err
	}

	value, _, err := jwt.NewParser().ParseUnverified(string(bytes.TrimSpace(rawIDToken)), jwt.MapClaims{})
	if err != nil {
		return nil, err
	}

	return (*kubectllogin.Token)(value), nil
}
