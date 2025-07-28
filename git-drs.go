package main

import (
	"log"
	"os"

	"github.com/calypr/git-drs/cmd"
)

func main() {
	if err := cmd.RootCmd.Execute(); err != nil {
		log.Println("Root Error:", err.Error())
		os.Exit(1)
	}
}
