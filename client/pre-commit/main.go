package main

import (
	"fmt"
	"log"

	"github.com/bmeg/git-drs/client"
)

// should this be a main method or a separate command?
// TODO: might need to split this up into command and indexd-specific client code
func main() {
	myLogger, err := client.NewLogger("")
	if err != nil {
		// Handle error (e.g., print to stderr and exit)
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer myLogger.Close() // Ensures cleanup

	myLogger.Log("~~~~~~~~~~~~~ START: pre-commit ~~~~~~~~~~~~~")
	myLogger.Log(" started")

	err = client.UpdateDrsMap()

	// reopen log file
	if err != nil {
		fmt.Println("updateDrsMap failed:", err)
		log.Fatalf("updateDrsMap failed: %v", err)
	}
	myLogger.Log("~~~~~~~~~~~~~ COMPLETED: pre-commit ~~~~~~~~~~~~~")
}
