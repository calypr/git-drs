package list

import (
	"fmt"
	"io"
	"os"

	"github.com/bytedance/sonic"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/drs/hash"
	"github.com/calypr/git-drs/drslog"
	"github.com/spf13/cobra"
)

var (
	outJson     bool = false
	outFile     string
	listOutFile string
	remote      string
)

var checksumPref = []hash.ChecksumType{hash.ChecksumTypeSHA256, hash.ChecksumTypeMD5, hash.ChecksumTypeETag}

func getChecksumPos(q hash.ChecksumType, a []hash.ChecksumType) int {
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
	for checksumType, checksum := range hash.ConvertHashInfoToMap(obj.Checksums) {
		c := getChecksumPos(hash.ChecksumType(checksumType), checksumPref)
		if c != -1 && c < curPos {
			curPos = c
			curVal = checksumType + ":" + checksum
		}
	}
	return curVal
}

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "list",
	Short: "List DRS entities from server",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: accepts no arguments, received %d\n\nUsage: %s\n\nSee 'git drs list --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := drslog.GetLogger()

		var outWriter io.Writer
		if listOutFile != "" {
			f, err := os.Create(listOutFile)
			if err != nil {
				return err
			}
			defer f.Close()
			outWriter = f
		} else {
			outWriter = os.Stdout
		}

		conf, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("error loading config: %v", err)
		}

		remoteName, err := conf.GetRemoteOrDefault(remote)
		if err != nil {
			return fmt.Errorf("error getting default remote: %v", err)
		}

		client, err := conf.GetRemoteClient(remoteName, logger)
		if err != nil {
			logger.Debug("Client failed")
			return err
		}
		objChan, err := client.ListObjects()
		if err != nil {
			return err
		}
		if !outJson {
			fmt.Fprintf(outWriter, "%-55s\t%-15s\t%-75s\t%s\n", "URI", "Size", "Checksum", "Name")
		}

		// for each result, check for error and print
		for objResult := range objChan {
			if objResult.Error != nil {
				return objResult.Error
			}
			obj := objResult.Object
			if outJson {
				out, err := sonic.ConfigFastest.Marshal(*obj)
				if err != nil {
					return err
				}
				fmt.Fprintf(outWriter, "%s\n", string(out))
			} else {
				fmt.Fprintf(outWriter, "%s\t%-15d\t%-75s\t%s\n", obj.SelfURI, obj.Size, getCheckSumStr(*obj), obj.Name)
			}
		}
		return nil
	},
}
var ListProjectCmd = &cobra.Command{
	Use:   "list-project <project-id>",
	Short: "List DRS entities from server",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: requires exactly 1 argument (project ID), received %d\n\nUsage: %s\n\nSee 'git drs list-project --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := drslog.GetLogger()

		conf, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("error loading config: %v", err)
		}

		remoteName, err := conf.GetRemoteOrDefault(remote)
		if err != nil {
			return fmt.Errorf("error getting default remote: %v", err)
		}

		client, err := conf.GetRemoteClient(remoteName, logger)
		if err != nil {
			return err
		}
		objChan, err := client.ListObjectsByProject(args[0])
		if err != nil {
			return err
		}

		var f *os.File
		var outWriter io.Writer
		if outFile != "" {
			f, err = os.Create(outFile)
			if err != nil {
				return err
			}
			defer f.Close()
			outWriter = f
		} else {
			outWriter = os.Stdout
		}
		for objResult := range objChan {
			if objResult.Error != nil {
				return objResult.Error
			}
			obj := objResult.Object
			out, err := sonic.ConfigFastest.Marshal(*obj)
			if err != nil {
				return err
			}
			_, err = outWriter.Write(out)
			if err != nil {
				return err
			}
			_, err = outWriter.Write([]byte("\n"))
			if err != nil {
				return err
			}
		}
		return nil
	},
}

func init() {
	ListProjectCmd.Flags().StringVarP(&remote, "remote", "r", "", "target remote DRS server (default: default_remote)")
	ListProjectCmd.Flags().StringVarP(&outFile, "out", "o", outFile, "File path to save output to")
	Cmd.Flags().StringVarP(&remote, "remote", "r", "", "target remote DRS server (default: default_remote)")
	Cmd.Flags().StringVarP(&listOutFile, "out", "o", listOutFile, "File path to save output to")
	Cmd.Flags().BoolVarP(&outJson, "json", "j", outJson, "Output formatted as JSON")
}
