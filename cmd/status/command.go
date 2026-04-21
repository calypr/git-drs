package status

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var deep bool

var Cmd = &cobra.Command{
	Use:   "status",
	Short: "Show quick git and DRS remote status",
	Long:  "Lightweight status summary. Use --deep to include remote auth/ping checks.",
	RunE: func(cmd *cobra.Command, args []string) error {
		myLogger := drslog.GetLogger()

		fmt.Println(color.CyanString("=== Git Status ==="))
		if err := printGitStatusFast(); err != nil {
			myLogger.Debug(fmt.Sprintf("Git status error: %v", err))
		}
		fmt.Println()

		fmt.Println(color.CyanString("=== DRS Config ==="))
		if err := printDRSConfig(); err != nil {
			myLogger.Debug(fmt.Sprintf("DRS config error: %v", err))
			fmt.Println(color.RedString("❌ Error loading DRS config: %v", err))
		}
		fmt.Println()

		if deep {
			fmt.Println(color.CyanString("=== DRS Server Ping ==="))
			pingServer(myLogger)
			fmt.Println()
		}

		return nil
	},
}

func init() {
	Cmd.Flags().BoolVar(&deep, "deep", false, "Include remote auth/ping checks (slower)")
}

func printGitStatusFast() error {
	cmd := exec.Command("git", "status", "-sb")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func printDRSConfig() error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}

	if len(cfg.Remotes) == 0 {
		fmt.Println("No DRS remotes configured.")
		return nil
	}

	remoteNames := make([]string, 0, len(cfg.Remotes))
	for name := range cfg.Remotes {
		remoteNames = append(remoteNames, string(name))
	}
	sort.Strings(remoteNames)

	for _, nameStr := range remoteNames {
		name := config.Remote(nameStr)
		r := cfg.GetRemote(name)
		if r == nil {
			fmt.Printf("  %s (invalid config)\n", name)
			continue
		}

		marker := " "
		if name == cfg.DefaultRemote {
			marker = "*"
		}

		remoteType := "unknown"
		if rs := cfg.Remotes[name]; rs.Gen3 != nil {
			remoteType = "gen3"
		} else if rs.Anvil != nil {
			remoteType = "anvil"
		}

		bucket := r.GetBucketName()
		if bucket == "" {
			bucket = "(auto)"
		}

		fmt.Printf("%s %s\n", marker, name)
		fmt.Printf("    type: %s\n", remoteType)
		fmt.Printf("    endpoint: %s\n", valueOrNA(r.GetEndpoint()))
		fmt.Printf("    project: %s\n", valueOrNA(r.GetProjectId()))
		fmt.Printf("    bucket: %s\n", bucket)
	}

	return nil
}

func valueOrNA(v string) string {
	if strings.TrimSpace(v) == "" {
		return "N/A"
	}
	return v
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

	remoteNames := make([]string, 0, len(cfg.Remotes))
	for name := range cfg.Remotes {
		remoteNames = append(remoteNames, string(name))
	}
	sort.Strings(remoteNames)

	for _, nameStr := range remoteNames {
		name := config.Remote(nameStr)
		rc := cfg.GetRemote(name)

		fmt.Printf("Remote: %s\n", name)

		drsClient, err := cfg.GetRemoteClient(name, logger)
		if err != nil {
			errStr := err.Error()
			if strings.Contains(errStr, "profile not found in config file") ||
				strings.Contains(errStr, "no gen3 project specified") ||
				strings.Contains(errStr, "no gen3 bucket specified") ||
				strings.Contains(errStr, "automatic bucket resolution failed") {

				fmt.Println(color.RedString("  ❌ Failed: Remote profile '%s' is not fully configured.", name))

				proj := "<PROJECT>"
				en := "<URL>"

				if rc != nil {
					if p := rc.GetProjectId(); p != "" {
						proj = p
					}
					if e := rc.GetEndpoint(); e != "" {
						en = e
					}
				}

				fmt.Println(color.RedString("     Fix with: git drs remote add gen3 %s --project %s --url %s --cred <CRED_PATH>", name, proj, en))
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
		if projID == "" && rc != nil {
			projID = rc.GetProjectId()
		}

		if projID == "" {
			fmt.Println(color.YellowString("  ⚠️  Could not determine Project ID for %s", name))
			continue
		}

		parts := strings.SplitN(projID, "-", 2)
		expectedPath := fmt.Sprintf("/programs/%s", projID)
		if len(parts) == 2 {
			expectedPath = fmt.Sprintf("/programs/%s/projects/%s", parts[0], parts[1])
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
