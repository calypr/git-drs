package addref

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	syclient "github.com/calypr/syfon/client"
	"github.com/calypr/syfon/client/hash"
	"github.com/spf13/cobra"
)

var remote string
var remoteType string
var Cmd = &cobra.Command{
	Use:   "add-ref <drs_uri> <dst path>",
	Short: "Add a reference to an existing DRS object via URI",
	Long:  "Add a reference to an existing DRS object via URI. Requires that the sha256 of the file is already in the cache",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		drsUri := args[0]
		dstPath := args[1]

		logger := drslog.GetLogger()

		logger.Debug(fmt.Sprintf("Adding reference to DRS object %s to %s", drsUri, dstPath))

		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}

		remoteName, err := cfg.GetRemoteOrDefault(remote)
		if err != nil {
			logger.Error(fmt.Sprintf("Error getting remote: %v", err))
			return err
		}

		client, err := remoteruntime.New(cfg, remoteName, logger)
		if err != nil {
			return err
		}

		obj, err := resolveAddRefObject(context.Background(), cfg, remoteName, client, drsUri)
		if err != nil {
			return err
		}
		dirPath := filepath.Dir(dstPath)
		_, err = os.Stat(dirPath)
		if os.IsNotExist(err) {
			// The directory does not exist
			os.MkdirAll(dirPath, os.ModePerm)
		}

		oid := addRefLocalOID(drsUri, remoteName, &obj)
		if hasContentSHA256(&obj) {
			if err := lfs.CreateLfsPointerWithOID(&obj, dstPath, oid); err != nil {
				return err
			}
		} else if err := lfs.CreateDRSPointer(&obj, dstPath, drsUri); err != nil {
			return err
		}
		if obj.SelfUri == "" {
			obj.SelfUri = drsUri
		}
		if err := drsobject.WriteObject(gitrepo.DRSObjectsPath, &obj, oid); err != nil {
			return fmt.Errorf("write source DRS metadata: %w", err)
		}
		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&remote, "remote", "r", "", "target remote DRS server (default: default_remote)")
	Cmd.Flags().StringVar(&remoteType, "remote-type", "", "resolver remote type for DRS references (for example: terra)")
}

func hasContentSHA256(obj *drsapi.DrsObject) bool {
	return obj != nil && drsobject.NormalizeChecksum(hash.ConvertDrsChecksumsToHashInfo(obj.Checksums).SHA256) != ""
}

func addRefLocalOID(sourceURI string, remoteName config.Remote, obj *drsapi.DrsObject) string {
	if obj != nil {
		if sha := drsobject.NormalizeChecksum(hash.ConvertDrsChecksumsToHashInfo(obj.Checksums).SHA256); sha != "" {
			return sha
		}
	}
	return derivedSourceOID(sourceURI, string(remoteName))
}

func derivedSourceOID(sourceURI, remoteName string) string {
	sum := sha256.Sum256([]byte("git-drs-source-ref:v1\nsource_uri=" + sourceURI + "\nremote=" + remoteName + "\n"))
	return hex.EncodeToString(sum[:])
}

type drsObjectGetter interface {
	GetObject(context.Context, string) (drsapi.DrsObject, error)
}

func resolveAddRefObject(ctx context.Context, cfg *config.Config, primaryRemote config.Remote, primary *remoteruntime.GitContext, drsURI string) (drsapi.DrsObject, error) {
	objectID, sourceRemote, ok := sourceRemoteForDRSURI(cfg, primaryRemote, drsURI)
	if !ok {
		return primary.Client.DRS().GetObject(ctx, drsURI)
	}
	if sourceRemote == primaryRemote {
		if sourceEndpoint, sourceOK := sourceEndpointForDRSURI(drsURI); sourceOK && !endpointMatchesDRSURI(primary.Endpoint, drsURI) {
			getter, err := newAnonymousSourceDRSGetter(sourceEndpoint)
			if err != nil {
				return drsapi.DrsObject{}, err
			}
			return getter.GetObject(ctx, objectID)
		}
		return primary.Client.DRS().GetObject(ctx, objectID)
	}
	client, err := remoteruntime.New(cfg, sourceRemote, primary.Logger)
	if err != nil {
		return drsapi.DrsObject{}, err
	}
	return client.Client.DRS().GetObject(ctx, objectID)
}

func sourceRemoteForDRSURI(cfg *config.Config, primaryRemote config.Remote, drsURI string) (objectID string, remoteName config.Remote, ok bool) {
	u, err := url.Parse(strings.TrimSpace(drsURI))
	if err != nil || !strings.EqualFold(u.Scheme, "drs") || strings.TrimSpace(u.Host) == "" {
		return drsURI, primaryRemote, false
	}
	objectID = strings.TrimPrefix(u.EscapedPath(), "/")
	if objectID == "" {
		return drsURI, primaryRemote, false
	}
	for name, selected := range cfg.Remotes {
		remote := config.Config{Remotes: map[config.Remote]config.RemoteSelect{name: selected}}.GetRemote(name)
		if remote == nil {
			continue
		}
		endpoint, err := url.Parse(remote.GetEndpoint())
		if err != nil {
			continue
		}
		if strings.EqualFold(endpoint.Host, u.Host) {
			return objectID, name, true
		}
	}
	return objectID, primaryRemote, true
}

func sourceEndpointForDRSURI(drsURI string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(drsURI))
	if err != nil || !strings.EqualFold(u.Scheme, "drs") || strings.TrimSpace(u.Host) == "" {
		return "", false
	}
	return "https://" + u.Host, true
}

func endpointMatchesDRSURI(endpoint string, drsURI string) bool {
	u, err := url.Parse(strings.TrimSpace(drsURI))
	if err != nil || !strings.EqualFold(u.Scheme, "drs") || strings.TrimSpace(u.Host) == "" {
		return false
	}
	endpointURL, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return false
	}
	return strings.EqualFold(endpointURL.Host, u.Host)
}

func newAnonymousSourceDRSGetter(endpoint string) (drsObjectGetter, error) {
	raw, err := syclient.New(endpoint)
	if err != nil {
		return nil, err
	}
	client, ok := raw.(*syclient.Client)
	if !ok {
		return nil, fmt.Errorf("unexpected syfon client type %T", raw)
	}
	return client.DRS(), nil
}
