package push

import (
	"context"
	"fmt"
	"strings"
)

type syncRefState struct {
	LocalRef  string
	RemoteRef string
	TargetOID string
	AckRef    string
	AckOID    string
	RemoteOID string
}

func resolveSyncRefState(ctx context.Context, remote string) (syncRefState, error) {
	localRef, err := gitOutput(ctx, "symbolic-ref", "--quiet", "HEAD")
	if err != nil || strings.TrimSpace(localRef) == "" {
		return syncRefState{}, fmt.Errorf("git drs push requires an attached branch")
	}
	localRef = strings.TrimSpace(localRef)
	targetOID, err := gitOutput(ctx, "rev-parse", localRef)
	if err != nil {
		return syncRefState{}, err
	}

	remoteRef := strings.TrimPrefix(localRef, "refs/heads/")
	if upstream, upstreamErr := gitOutput(ctx, "rev-parse", "--symbolic-full-name", "@{upstream}"); upstreamErr == nil {
		prefix := "refs/remotes/" + remote + "/"
		if strings.HasPrefix(upstream, prefix) {
			remoteRef = strings.TrimPrefix(upstream, prefix)
		}
	}
	remoteRef = "refs/heads/" + strings.TrimPrefix(remoteRef, "refs/heads/")
	ackRef := "refs/git-drs/synced/" + strings.TrimPrefix(remoteRef, "refs/")

	remoteOID, ackOID, err := readRemoteSyncRefs(ctx, remote, remoteRef, ackRef)
	if err != nil {
		return syncRefState{}, err
	}
	if ackOID != "" {
		if _, err := gitOutput(ctx, "cat-file", "-e", ackOID+"^{commit}"); err != nil {
			if _, fetchErr := gitOutput(ctx, "fetch", "--no-tags", remote, ackRef); fetchErr != nil {
				return syncRefState{}, fmt.Errorf("fetch synchronization ref %s: %w", ackRef, fetchErr)
			}
		}
	}
	return syncRefState{
		LocalRef:  localRef,
		RemoteRef: remoteRef,
		TargetOID: targetOID,
		AckRef:    ackRef,
		AckOID:    ackOID,
		RemoteOID: remoteOID,
	}, nil
}

func readRemoteSyncRefs(ctx context.Context, remote, remoteRef, ackRef string) (string, string, error) {
	out, err := gitOutput(ctx, "ls-remote", "--refs", remote, remoteRef, ackRef)
	if err != nil {
		return "", "", fmt.Errorf("read remote refs: %w", err)
	}
	return readRemoteSyncRefsOutput(out, remoteRef, ackRef)
}

func readRemoteSyncRefsOutput(out, remoteRef, ackRef string) (string, string, error) {
	var remoteOID, ackOID string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		switch fields[1] {
		case remoteRef:
			remoteOID = fields[0]
		case ackRef:
			ackOID = fields[0]
		}
	}
	return remoteOID, ackOID, nil
}

func pushSyncAcknowledgment(ctx context.Context, remote string, state syncRefState, skippedUnavailable int) error {
	if skippedUnavailable > 0 {
		return nil
	}
	lease := "--force-with-lease=" + state.AckRef + ":" + state.AckOID
	source := state.TargetOID + ":" + state.AckRef
	if _, err := gitOutputFn(ctx, "push", "--no-verify", lease, remote, source); err != nil {
		return fmt.Errorf("advance DRS synchronization ref %s: %w", state.AckRef, err)
	}
	return nil
}
