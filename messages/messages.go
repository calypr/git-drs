package messages

import (
	"github.com/calypr/git-drs/cloud"
)

const (
	ADDURL_HELP_MSG     = "See git-drs add-url --help for more details."
	AWS_CREDS_ERROR_MSG = "Incomplete credentials provided as environment variables. Please run `export " + cloud.AWS_KEY_ENV_VAR + "=<key>` and `export " + cloud.AWS_SECRET_ENV_VAR + "=<secret>` to configure both."
)
