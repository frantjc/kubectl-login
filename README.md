# kubectl-login

A [kubernetes/client-go](https://github.com/kubernetes/client-go) auth exec tool for various OIDC providers. Currently supports AWS and GitHub Actions.

## install

See [install hint](./INSTALL_HINT.md).

## use

Pass the former to your cluster's kube-apiserver's `--authentication-config` flag and use the latter's user in your `~/.kube/config`'s `current-context`. Such a 

```yaml
---
# kube-apiserver-authentication-config.yaml
apiVersion: apiserver.config.k8s.io/v1
kind: AuthenticationConfiguration
jwt:
  - issuer:
      url: # <$(aws iam get-outbound-web-identity-federation-info --output text --query IssuerIdentifier)>
      audiences:
        - # <your audience>
    claimMappings:
      username:
        claim: sub
        prefix: "aws:"
  - issuer:
      url: https://token.actions.githubusercontent.com
      audiences:
        - # <your audience>
    claimMappings:
      username:
        claim: actor
        prefix: "github-actions:"
# ...
---
# ~/.kube/config
apiVersion: v1
kind: Config
users:
  - name: kubectl-login
    user:
      exec:
        apiVersion: client.authentication.k8s.io/v1
        command: kubectl-login
        interactiveMode: IfAvailable
        installHint: https://github.com/frantjc/kubectl-login/blob/main/INSTALL_HINT.md
        args:
          - --audience
          - # <your audience>
# ...
```
