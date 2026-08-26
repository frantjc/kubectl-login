package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/frantjc/kubectl-login/.dagger/internal/dagger"
)

type KubectlLoginDev struct{}

// +check
func (m *KubectlLoginDev) Binary(
	ctx context.Context,
	workspace *dagger.Workspace,
	// +optional
	// +default="v0.0.0-unknown"
	version string,
	// +optional
	goos string,
	// +optional
	goarch string,
) *dagger.File {
	return dag.Go(dagger.GoOpts{
		Workspace: workspace,
	}).
		Build(dagger.GoBuildOpts{
			Ldflags:   "-s -w -X main.version=" + version,
			Pkg:       "./cmd/kubectl-login",
			Goos:      goos,
			Goarch:    goarch,
		})
}

func (m *KubectlLoginDev) Release(
	ctx context.Context,
	workspace *dagger.Workspace,
	githubToken *dagger.Secret,
) error {
	archive := dag.Archive()
	gh := dag.Gh(githubToken)
	upx := dag.Upx()
	assets := []*dagger.File{}

	ref, err := workspace.Directory(".", dagger.WorkspaceDirectoryOpts{
		Gitignore: true,
	}).
		AsGit().
		LatestVersion().
		Ref(ctx)
	if err != nil {
		return err
	}

	tag := strings.TrimPrefix(ref, "refs/tags/")

	for _, goos := range []string{"darwin", "linux"} {
		for _, goarch := range []string{"amd64", "arm64"} {
			bin := dag.KubectlLoginDev().
				Binary(dagger.KubectlLoginDevBinaryOpts{
					Workspace: workspace,
					Version:   tag,
					Goos:      goos,
					Goarch:    goarch,
				})

			if goos == "linux" {
				bin = upx.Pack(bin)
			}

			file := fmt.Sprintf("kubectl-login-%s-%s-%s.tar.gz", tag, goos, goarch)
			asset := archive.
				Tar(
					workspace.Directory(".", dagger.WorkspaceDirectoryOpts{
						Include: []string{
							"README.md",
							"LICENSE",
						},
					}).
						WithFile(
							"kubectl-login",
							bin,
						),
					dagger.ArchiveTarOpts{
						Gzip: true,
					},
				).WithName(file)

			assets = append(assets, asset)
		}
	}

	release := gh.Release("frantjc/kubectl-login", tag)

	if err := release.Create(ctx, dagger.GhReleaseCreateOpts{
		Draft:         true,
		GenerateNotes: true,
	}); err != nil {
		return err
	}

	if err := release.Upload(ctx, assets, dagger.GhReleaseUploadOpts{
		Clobber: true,
	}); err != nil {
		return err
	}

	if err := release.Edit(ctx, dagger.GhReleaseEditOpts{
		Latest: true,
	}); err != nil {
		return err
	}

	return nil
}
