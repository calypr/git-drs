package credentialhelper

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	gitauth "github.com/calypr/git-drs/auth"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/gitrepo"
	"github.com/calypr/syfon/client/conf"
	"github.com/spf13/cobra"
)

// Cmd implements a git credential helper bridge for git-lfs over HTTP.
var Cmd = &cobra.Command{
	Use:    "credential-helper <get|store|erase>",
	Hidden: true,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("expected exactly one action: get, store, or erase")
		}
		switch args[0] {
		case "get", "store", "erase":
			return nil
		default:
			return fmt.Errorf("unsupported action: %s", args[0])
		}
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		action := args[0]
		if action != "get" {
			// Stateless helper: no-op for store/erase.
			return nil
		}

		req, err := readCredentialRequest(os.Stdin)
		if err != nil {
			return nil
		}

		_ = req // currently not used; remote selection is repo-default oriented.

		logg := drslog.GetLogger()
		remoteName, endpoint, err := resolveRemote()
		if err != nil {
			return nil
		}

		// Local/basic-auth remotes: prefer explicit repo credentials.
		if username, password, err := gitrepo.GetRemoteBasicAuth(remoteName); err == nil && username != "" && password != "" {
			fmt.Fprintf(os.Stdout, "username=%s\npassword=%s\n\n", username, password)
			_ = gitrepo.SetRemoteLFSURL(remoteName, endpoint)
			return nil
		}

		token := ""
		// Prefer repo-local token first to keep git-lfs local and deterministic.
		if repoToken, err := gitrepo.GetRemoteToken(remoteName); err == nil && strings.TrimSpace(repoToken) != "" {
			token = strings.TrimSpace(repoToken)
		}

		// Try global profile to refresh/validate; fall back to repo token if unavailable.
		manager := conf.NewConfigure(logg)
		cred, err := manager.Load(remoteName)
		if err == nil {
			if token != "" {
				cred.AccessToken = token
			}
			if ensureErr := gitauth.EnsureValidCredential(context.Background(), cred, logg); ensureErr == nil {
				_ = manager.Save(cred)
				token = strings.TrimSpace(cred.AccessToken)
				if token != "" {
					_ = gitrepo.SetRemoteToken(remoteName, token)
				}
			}
		}

		if token == "" {
			return nil
		}

		// Username can be arbitrary for token-based Basic auth; server reads password token.
		fmt.Fprintf(os.Stdout, "username=oauth2\npassword=%s\n\n", token)

		// Keep lfsurl synced for this remote.
		_ = gitrepo.SetRemoteLFSURL(remoteName, endpoint)
		return nil
	},
}

type credentialRequest struct {
	Protocol string
	Host     string
	Path     string
}

func readCredentialRequest(in io.Reader) (credentialRequest, error) {
	req := credentialRequest{}
	s := bufio.NewScanner(in)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			break
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "protocol":
			req.Protocol = val
		case "host":
			req.Host = val
		case "path":
			req.Path = val
		}
	}
	return req, s.Err()
}

func resolveRemote() (string, string, error) {
	remoteName, err := gitrepo.GetGitConfigString("drs.default-remote")
	if err != nil {
		return "", "", err
	}
	remoteName = strings.TrimSpace(remoteName)
	if remoteName == "" {
		return "", "", fmt.Errorf("no default remote configured")
	}
	endpoint, err := gitrepo.GetGitConfigString(fmt.Sprintf("drs.remote.%s.endpoint", remoteName))
	if err != nil {
		return "", "", err
	}
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", "", fmt.Errorf("remote %q endpoint not found", remoteName)
	}
	return remoteName, endpoint, nil
}
