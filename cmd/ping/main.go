package ping

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/spf13/cobra"
)

type statusInfo struct {
	Remote        config.Remote
	IsDefault     bool
	RemoteType    string
	Endpoint      string
	Organization  string
	Project       string
	Bucket        string
	StoragePrefix string
	AuthMode      string
}

var pingHealth = func(ctx context.Context, gc *config.GitContext) error {
	return gc.Client.Health().Ping(ctx)
}

var Cmd = &cobra.Command{
	Use:   "ping [remote-name]",
	Short: "Show effective remote setup and verify the remote responds",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: accepts at most 1 argument (remote name), received %d\n\nUsage: %s\n\nSee 'git drs ping --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := drslog.GetLogger()
		status, gc, err := resolveStatus(args, logger)
		if err != nil {
			return err
		}
		printStatus(status)

		if err := pingHealth(cmd.Context(), gc); err != nil {
			return fmt.Errorf("remote health check failed for %q (%s): %w", status.Remote, status.Endpoint, err)
		}
		fmt.Println("health: ok")
		return nil
	},
}

func resolveStatus(args []string, logger *slog.Logger) (statusInfo, *config.GitContext, error) {
	cfg, err := config.LoadConfig()
	if err != nil {
		return statusInfo{}, nil, err
	}

	var remoteArg string
	if len(args) == 1 {
		remoteArg = args[0]
	}
	remoteName, err := cfg.GetRemoteOrDefault(remoteArg)
	if err != nil {
		return statusInfo{}, nil, err
	}

	remoteCfg := cfg.GetRemote(remoteName)
	if remoteCfg == nil {
		return statusInfo{}, nil, fmt.Errorf("no remote configuration found for %q", remoteName)
	}

	gc, err := cfg.GetRemoteClient(remoteName, logger)
	if err != nil {
		return statusInfo{}, nil, err
	}

	status := statusInfo{
		Remote:        remoteName,
		IsDefault:     remoteName == cfg.DefaultRemote,
		Endpoint:      remoteCfg.GetEndpoint(),
		Organization:  remoteCfg.GetOrganization(),
		Project:       remoteCfg.GetProjectId(),
		Bucket:        gc.BucketName,
		StoragePrefix: gc.StoragePrefix,
		AuthMode:      authMode(gc),
	}
	switch remoteCfg.(type) {
	case *config.Gen3Remote:
		status.RemoteType = string(config.Gen3ServerType)
	case *config.LocalRemote:
		status.RemoteType = string(config.LocalServerType)
	default:
		status.RemoteType = "unknown"
	}

	return status, gc, nil
}

func printStatus(status statusInfo) {
	def := ""
	if status.IsDefault {
		def = " (default)"
	}
	fmt.Printf("remote: %s%s\n", status.Remote, def)
	fmt.Printf("type: %s\n", status.RemoteType)
	fmt.Printf("endpoint: %s\n", status.Endpoint)
	fmt.Printf("organization: %s\n", blankIfEmpty(status.Organization))
	fmt.Printf("project: %s\n", blankIfEmpty(status.Project))
	fmt.Printf("bucket: %s\n", blankIfEmpty(status.Bucket))
	fmt.Printf("storage_prefix: %s\n", blankIfEmpty(status.StoragePrefix))
	fmt.Printf("auth: %s\n", status.AuthMode)
}

func authMode(gc *config.GitContext) string {
	if gc == nil || gc.Credential == nil {
		return "none"
	}
	if strings.TrimSpace(gc.Credential.AccessToken) != "" {
		return "bearer"
	}
	if strings.TrimSpace(gc.Credential.KeyID) != "" || strings.TrimSpace(gc.Credential.APIKey) != "" {
		return "basic"
	}
	return "none"
}

func blankIfEmpty(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "-"
	}
	return v
}
