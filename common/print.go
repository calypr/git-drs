package common

import (
	"fmt"

	"github.com/bytedance/sonic"
	"github.com/calypr/data-client/drs"
)

// PrintDRSObject marshals and prints a DRS object based on the pretty flag
func PrintDRSObject(obj drs.DRSObject, pretty bool) error {
	var out []byte
	var err error

	if pretty {
		out, err = sonic.ConfigFastest.MarshalIndent(obj, "", "  ")
	} else {
		out, err = sonic.ConfigFastest.Marshal(obj)
	}

	if err != nil {
		return err
	}

	fmt.Printf("%s\n", string(out))
	return nil
}
