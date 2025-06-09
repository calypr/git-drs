module github.com/bmeg/git-drs

go 1.24.0

require (
	github.com/git-lfs/git-lfs/v3 v3.6.1
	github.com/spf13/cobra v1.9.1
	github.com/uc-cdis/gen3-client v0.0.23
	github.com/google/uuid v1.6.0
	sigs.k8s.io/yaml v1.4.0
)

require (
	github.com/avast/retry-go v2.4.2+incompatible // indirect
	github.com/git-lfs/gitobj/v2 v2.1.1 // indirect
	github.com/git-lfs/pktline v0.0.0-20210330133718-06e9096e2825 // indirect
	github.com/git-lfs/wildmatch/v2 v2.0.1 // indirect
	github.com/google/go-github v17.0.0+incompatible // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/hashicorp/go-version v1.4.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/leonelquinteros/gotext v1.5.0 // indirect
	github.com/mattn/go-runewidth v0.0.13 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/pkg/errors v0.0.0-20170505043639-c605e284fe17 // indirect
	github.com/rivo/uniseg v0.2.0 // indirect
	github.com/rubyist/tracerx v0.0.0-20170927163412-787959303086 // indirect
	github.com/spf13/pflag v1.0.6 // indirect
	github.com/tcnksm/go-latest v0.0.0-20170313132115-e3007ae9052e // indirect
	golang.org/x/net v0.23.0 // indirect
	golang.org/x/sys v0.18.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	gopkg.in/cheggaaa/pb.v1 v1.0.28 // indirect
	gopkg.in/ini.v1 v1.66.3 // indirect
)

replace github.com/uc-cdis/gen3-client => ../cdis-data-client
