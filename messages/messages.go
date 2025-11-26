package messages

import "github.com/calypr/git-drs/s3_utils"

const (
	ADDURL_HELP_MSG     = "See git-drs add-url --help for more details."
	AWS_CREDS_ERROR_MSG = "Incomplete credentials provided as environment variables. Please run `export " + s3_utils.AWS_KEY_ENV_VAR + "=<key>` and `export " + s3_utils.AWS_SECRET_ENV_VAR + "=<secret>` to configure both."
)
