package list

import (
	"encoding/json"
	"fmt"

	"github.com/bmeg/git-drs/client"
	"github.com/bmeg/git-drs/drs"
	"github.com/spf13/cobra"
)

var outJson = false

var checksumPref = []string{"sha256", "md5", "etag"}

func getStringPos(q string, a []string) int {
	for i, s := range a {
		if q == s {
			return i
		}
	}
	return -1
}

// Pick out the most preferred checksum to display
func getCheckSumStr(obj drs.DRSObject) string {
	curPos := len(checksumPref) + 1
	curVal := ""
	for _, e := range obj.Checksums {
		c := getStringPos(e.Type, checksumPref)
		if c != -1 && c < curPos {
			curPos = c
			curVal = e.Type + ":" + e.Checksum
		}
	}
	return curVal
}

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "list",
	Short: "List DRS entities from server",
	Args:  cobra.MinimumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := client.NewIndexDClient()
		if err != nil {
			return err
		}
		objChan, err := client.ListObjects()
		if err != nil {
			return err
		}
		for obj := range objChan {
			if outJson {
				out, err := json.Marshal(*obj)
				if err != nil {
					return err
				}
				fmt.Printf("%s\n", string(out))
			} else {
				fmt.Printf("%s\t%15d\t%-45s\t%s\n", obj.SelfURL, obj.Size, getCheckSumStr(*obj), obj.Name)
			}

		}
		return nil
	},
}

func init() {
	Cmd.Flags().BoolVarP(&outJson, "json", "j", outJson, "Specify the profile to use")
}
