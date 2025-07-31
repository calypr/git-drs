package list

import (
	"encoding/json"

	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/drs"
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
	Args:  cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {

		logger, err := client.NewLogger("")
		if err != nil {
			return err
		}
		defer logger.Close()

		client, err := client.NewIndexDClient(logger)
		if err != nil {
			return err
		}
		objChan, err := client.ListObjects()
		if err != nil {
			return err
		}
		if !outJson {
			logger.Logf("%-55s\t%-15s\t%-75s\t%s\n", "URI", "Size", "Checksum", "Name")
		}

		// for each result, check for error and print
		for objResult := range objChan {
			if objResult.Error != nil {
				return objResult.Error
			}
			obj := objResult.Object
			if outJson {
				out, err := json.Marshal(*obj)
				if err != nil {
					return err
				}
				logger.Logf("%s\n", string(out))
			} else {
				logger.Logf("%s\t%-15d\t%-75s\t%s\n", obj.SelfURI, obj.Size, getCheckSumStr(*obj), obj.Name)
			}
		}
		return nil
	},
}

func init() {
	Cmd.Flags().BoolVarP(&outJson, "json", "j", outJson, "Output formatted as JSON")
}
