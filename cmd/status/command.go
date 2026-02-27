package status

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/lfs"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "status",
	Short: "Show working tree status, LFS config, remote info, and drs-server ping.",
	Long:  "Show a summary of tracked/untracked staged files, currently selected LFS profile, git remotes, and check permissions against the drs-server.",
	RunE: func(cmd *cobra.Command, args []string) error {
		myLogger := drslog.GetLogger()

		fmt.Println(color.CyanString("=== Git Status (Staged files) ==="))
		if err := printGitStatus(); err != nil {
			myLogger.Debug(fmt.Sprintf("Git status error: %v", err))
		}
		fmt.Println()

		fmt.Println(color.CyanString("=== Git LFS Configuration ==="))
		if err := printLFSConfig(); err != nil {
			myLogger.Debug(fmt.Sprintf("LFS config error: %v", err))
		}
		fmt.Println()

		fmt.Println(color.CyanString("=== Git Remotes ==="))
		if err := printGitRemotes(); err != nil {
			myLogger.Debug(fmt.Sprintf("Git remotes error: %v", err))
		}
		fmt.Println()

		fmt.Println(color.CyanString("=== DRS Server Ping ==="))
		pingServer(myLogger)
		fmt.Println()

		return nil
	},
}

func printGitStatus() error {
	cmd := exec.Command("git", "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return err
	}

	tracked := make(map[string][]string)   // LFS Tracked
	untracked := make(map[string][]string) // Not LFS Tracked
	gitAttributesStaged := false

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if len(line) < 4 {
			continue
		}

		x := line[0] // index status
		if x == ' ' || x == '?' || x == '!' {
			continue // not staged
		}

		pathStr := line[3:]
		// if rename, it's 'new -> old', just taking prefix or full string is fine for dir grouping
		parts := strings.Split(pathStr, " -> ")
		if len(parts) > 1 {
			pathStr = parts[0] // taking the new path for rename
		}

		// Remove quotes if path is quoted
		pathStr = strings.Trim(pathStr, "\"")

		if pathStr == ".gitattributes" {
			gitAttributesStaged = true
		}

		dir := filepath.Dir(pathStr)
		if dir == "." {
			dir = ""
		} else {
			dir = dir + string(filepath.Separator)
		}

		isLFSTracked, _ := lfs.IsLFSTracked(pathStr)

		if isLFSTracked {
			tracked[dir] = append(tracked[dir], pathStr)
		} else {
			untracked[dir] = append(untracked[dir], pathStr)
		}
	}

	if len(tracked) > 0 && !gitAttributesStaged {
		fmt.Println(color.RedString("⚠️  WARNING: LFS/DRS files are staged, but .gitattributes is NOT staged!"))
		fmt.Println(color.RedString("   ACTION REQUIRED: Run 'git add .gitattributes' to ensure your files are handled correctly."))
		fmt.Println()
	}

	printGroup := func(title string, groups map[string][]string, c *color.Color) {
		if len(groups) == 0 {
			fmt.Printf("No %s files.\n", strings.ToLower(title))
			return
		}

		fmt.Printf("%s:\n", title)

		var dirs []string
		for d := range groups {
			dirs = append(dirs, d)
		}
		sort.Strings(dirs)

		for _, d := range dirs {
			files := groups[d]
			sort.Strings(files)
			count := len(files)
			if count > 5 {
				pathDisp := d
				if pathDisp == "" {
					pathDisp = "(root)"
				}
				c.Printf("  %s* (%d files)\n", pathDisp, count)
			} else {
				for _, f := range files {
					c.Printf("  %s\n", f)
				}
			}
		}
	}

	printGroup("Tracked Staged", tracked, color.New(color.FgGreen))
	printGroup("Untracked Staged", untracked, color.New(color.FgYellow))

	return nil
}

func printLFSConfig() error {
	cmd := exec.Command("git", "config", "-l")
	out, err := cmd.Output()
	if err != nil {
		return err
	}

	lines := strings.Split(string(out), "\n")
	hasLfs := false
	for _, line := range lines {
		if strings.Contains(line, "lfs.") {
			// Skip remote-specific DRS configs, they will be shown in the Ping section
			if strings.Contains(line, "lfs.customtransfer.drs.remote.") {
				continue
			}
			fmt.Printf("  %s\n", line)
			hasLfs = true
		}
	}

	if !hasLfs {
		fmt.Println("  (No LFS config found in git config)")
	}
	return nil
}

func printGitRemotes() error {
	cmd := exec.Command("git", "remote", "-v")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func pingServer(logger *slog.Logger) {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Println(color.RedString("❌ Error loading config: %v", err))
		return
	}

	if len(cfg.Remotes) == 0 {
		fmt.Println("No DRS remotes configured.")
		return
	}

	// Sort remotes for consistent output
	var remoteNames []string
	for name := range cfg.Remotes {
		remoteNames = append(remoteNames, string(name))
	}
	sort.Strings(remoteNames)

	for _, nameStr := range remoteNames {
		name := config.Remote(nameStr)
		rc := cfg.GetRemote(name)

		fmt.Printf("Remote: %s\n", name)

		// Print the raw git config for this specific remote
		prefix := fmt.Sprintf("lfs.customtransfer.drs.remote.%s.", name)
		cmd := exec.Command("git", "config", "-l")
		out, _ := cmd.Output()
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, prefix) {
				fmt.Printf("  %s\n", line)
			}
		}

		drsClient, err := cfg.GetRemoteClient(name, logger)
		if err != nil {
			errStr := err.Error()
			if strings.Contains(errStr, "profile not found in config file") ||
				strings.Contains(errStr, "no gen3 project specified") ||
				strings.Contains(errStr, "no gen3 bucket specified") {

				fmt.Println(color.RedString("  ❌ Failed: Remote profile '%s' is not fully configured.", name))

				// Build a helpful suggestion with pre-filled fields
				proj := "<PROJECT>"
				buck := "<BUCKET>"
				en := "<URL>"

				if rc != nil {
					if p := rc.GetProjectId(); p != "" {
						proj = p
					}
					if b := rc.GetBucketName(); b != "" {
						buck = b
					}
					if e := rc.GetEndpoint(); e != "" {
						en = e
					}
				}

				fmt.Println(color.RedString("     Fix with: git drs remote add gen3 %s --bucket %s --project %s --url %s --cred <CRED_PATH>", name, buck, proj, en))
			} else {
				fmt.Println(color.RedString("  ❌ Failed to get client: %v", err))
			}
			continue
		}

		if drsClient == nil || drsClient.GetGen3Interface() == nil || drsClient.GetGen3Interface().Fence() == nil {
			fmt.Println(color.YellowString("  ⚠️  Remote %s does not support Gen3/Fence Interface", name))
			continue
		}

		ctx := context.Background()
		userPing, err := drsClient.GetGen3Interface().Fence().UserPing(ctx)
		if err != nil {
			fmt.Println(color.RedString("  ❌ Ping Failed (Auth error?): %v", err))
			continue
		}

		projID := drsClient.GetProjectId()
		if projID == "" {
			rc := cfg.GetRemote(name)
			if rc != nil {
				projID = rc.GetProjectId()
			}
		}

		if projID == "" {
			fmt.Println(color.YellowString("  ⚠️  Could not determine Project ID for %s", name))
			continue
		}

		// e.g. /programs/BMEG/projects/TEST
		parts := strings.SplitN(projID, "-", 2)
		var expectedPath string
		if len(parts) == 2 {
			expectedPath = fmt.Sprintf("/programs/%s/projects/%s", parts[0], parts[1])
		} else {
			expectedPath = fmt.Sprintf("/programs/%s", projID)
		}

		fmt.Printf("  Authenticated as: %s\n", userPing.Username)
		fmt.Printf("  Checking access for project: %s (Path: %s)\n", projID, expectedPath)

		hasAccess := false
		for path := range userPing.YourAccess {
			if strings.HasPrefix(path, expectedPath) {
				hasAccess = true
				break
			}
		}

		if !hasAccess {
			fmt.Println(color.RedString("  ❌ Permission Denied: user `%s` does not have access to '%s'", userPing.Username, expectedPath))
		} else {
			fmt.Println(color.GreenString("  ✅ Access Verified"))
		}
	}
}
