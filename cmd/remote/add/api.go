package add

import "log/slog"

// Gen3Options contains the repository-local values needed to configure a
// Gen3 DRS remote programmatically.
type Gen3Options struct {
	RemoteName string
	Token      string
	Bucket     string
	Scope      string
	Logger     *slog.Logger
}

// ConfigureGen3 configures a Gen3 remote without requiring a Cobra command or
// a git-drs subprocess. It is intentionally small so embedded services can
// reuse the same setup path as the CLI.
func ConfigureGen3(opts Gen3Options) error {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	previousToken := fenceToken
	previousBucket := selectedBucket
	defer func() {
		fenceToken = previousToken
		selectedBucket = previousBucket
	}()
	fenceToken = opts.Token
	selectedBucket = opts.Bucket
	return gen3Init(opts.RemoteName, "", opts.Token, opts.Scope, logger)
}
