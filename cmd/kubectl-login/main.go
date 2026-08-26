package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	kubectllogin "github.com/frantjc/kubectl-login"
	"github.com/frantjc/kubectl-login/internal/aws"
	"github.com/frantjc/kubectl-login/internal/directory"
	"github.com/frantjc/kubectl-login/internal/gha"
	xerrors "github.com/frantjc/x/errors"
	xos "github.com/frantjc/x/os"
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

func newKubectlLogin() *cobra.Command {
	var (
		audience string
		rawCache string
		cmd      = &cobra.Command{
			Use:           "kubectl-login",
			Version:       semVer(),
			SilenceErrors: true,
			SilenceUsage:  true,
			PreRun: func(cmd *cobra.Command, args []string) {
				// Caches.
				directory.Register()
				// Providers.
				aws.Register()
				gha.Register(cmd.ErrOrStderr(), http.DefaultClient)
			},
			RunE: func(cmd *cobra.Command, args []string) error {
				ctx := cmd.Context()

				cache, err := kubectllogin.NewCache(ctx, rawCache)
				if err != nil {
					return err
				}

				token, expirationTimestamp, err := kubectllogin.NewToken(ctx, audience, cache)
				if err != nil {
					return err
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
					Spec: cliauthv1.ExecCredentialSpec{
						// TODO
						Cluster: nil,
						// TODO
						Interactive: false,
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

	cmd.Flags().StringVarP(&rawCache, "cache", "c", "file://~/.kube/cache/login", "Token cache")

	return cmd
}
