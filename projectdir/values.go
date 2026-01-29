package projectdir

const (
	LFS_OBJS_PATH = ".git/lfs/objects"
	DRS_DIR       = ".git/drs"
	// FIXME: should this be /lfs/objects or just /objects?
	DRS_OBJS_PATH = DRS_DIR + "/lfs/objects"
	CONFIG_YAML   = "config.yaml"

	DRS_REF_DIR  string = ".git/drs/objects"
	DRS_LOG_FILE string = ".git/drs/drs.log"
)
