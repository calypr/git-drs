module github.com/bmeg/git-drs

go 1.24.0

require (
	github.com/google/uuid v1.6.0
	github.com/spf13/cobra v1.9.1
	github.com/uc-cdis/gen3-client v0.0.23
	sigs.k8s.io/yaml v1.4.0
)

require (
	cloud.google.com/go/compute/metadata v0.3.0 // indirect
	github.com/fatih/color v1.18.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/stretchr/testify v1.10.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

require (
	github.com/google/go-github v17.0.0+incompatible // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/hashicorp/go-version v1.4.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/mattn/go-runewidth v0.0.13 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/rivo/uniseg v0.2.0 // indirect
	github.com/spf13/pflag v1.0.6 // indirect
	github.com/tcnksm/go-latest v0.0.0-20170313132115-e3007ae9052e // indirect
	golang.org/x/net v0.41.0 // indirect
	golang.org/x/oauth2 v0.30.0
	golang.org/x/sys v0.33.0 // indirect
	gopkg.in/cheggaaa/pb.v1 v1.0.28 // indirect
	gopkg.in/ini.v1 v1.66.3 // indirect
)

replace github.com/uc-cdis/gen3-client => ./cdis-data-client
