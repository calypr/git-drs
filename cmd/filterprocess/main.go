package filterprocess

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/git-lfs/git-lfs/v3/git"
	"github.com/spf13/cobra"
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "filter-process",
	Short: "filter proces",
	Long:  ``,
	Args:  cobra.MinimumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		s := git.NewFilterProcessScanner(os.Stdin, os.Stdout)
		err := s.Init()
		if err != nil {
			return err
		}

		caps, err := s.NegotiateCapabilities()
		if err != nil {
			return err
		}
		log.Printf("Caps: %#v\n", caps)
		log.Printf("Running filter-process: %s\n", args)

		for s.Scan() {
			req := s.Request()
			switch req.Header["command"] {
			case "clean":
				log.Printf("Request to clean %#v %s\n", req.Payload, req.Header["pathname"])

				clean(os.Stdout, req.Payload, req.Header["pathname"], -1)

			case "smudge":
				log.Printf("Request to smudge %s %s\n", req.Payload, req.Header["pathname"])
			case "list_available_blobs":
				log.Printf("Request for list_available_blobs\n")

			default:
				return fmt.Errorf("don't know what to do: %s", req.Header["command"])
			}
			log.Printf("Request: %#v\n", req)
		}

		return nil
	},
}

func clean(to io.Writer, from io.Reader, fileName string, fileSize int64) error {

	return nil
}
