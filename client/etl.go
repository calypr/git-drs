// Package client contains small programmatic entry points for services that
// embed git-drs rather than invoking the git-drs executable.
package client

import (
	"fmt"
	"log/slog"

	"github.com/calypr/git-drs/cmd/initialize"
	remoteadd "github.com/calypr/git-drs/cmd/remote/add"
)

// Gen3RemoteOptions describes the minimal Gen3 remote setup needed by the ETL
// worker. Token is the bearer token issued by Fence.
type Gen3RemoteOptions struct {
	RemoteName string
	Token      string
	Bucket     string
	Scope      string
	Logger     *slog.Logger
}

// ConfigureGen3Remote configures a repository-local Gen3 DRS remote without
// invoking Cobra or the git-drs executable.
func ConfigureGen3Remote(opts Gen3RemoteOptions) error {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.RemoteName == "" {
		return fmt.Errorf("remote name is required")
	}
	if opts.Token == "" {
		return fmt.Errorf("Gen3 token is required")
	}
	if opts.Scope == "" {
		return fmt.Errorf("Gen3 scope is required")
	}
	return remoteadd.ConfigureGen3(remoteadd.Gen3Options{
		RemoteName: opts.RemoteName,
		Token:      opts.Token,
		Bucket:     opts.Bucket,
		Scope:      opts.Scope,
		Logger:     opts.Logger,
	})
}

// InitializeRepository applies idempotent repository-local git-drs setup.
func InitializeRepository(logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	return initialize.InitializeRepo(logger)
}
