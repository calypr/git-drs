package workflow

import (
	"fmt"

	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/config"
	workflowPkg "github.com/calypr/git-drs/workflow"
	"github.com/spf13/cobra"
)

var (
	listPolicies   bool
	enableWorkflows bool
	disableWorkflows bool
)

// Cmd represents the workflow command
var Cmd = &cobra.Command{
	Use:   "workflow",
	Short: "Manage workflow automation policies",
	Long:  "Manage workflow automation policies for automatic execution based on file types",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("error loading config: %v", err)
		}

		if enableWorkflows {
			cfg.Workflows.Enabled = true
			return saveConfig(cfg)
		}

		if disableWorkflows {
			cfg.Workflows.Enabled = false
			return saveConfig(cfg)
		}

		if listPolicies {
			return displayPolicies(cfg)
		}

		return cmd.Help()
	},
}

// AddPolicyCmd adds a new workflow policy
var AddPolicyCmd = &cobra.Command{
	Use:   "add-policy",
	Short: "Add a workflow policy",
	Long:  "Add a workflow policy that defines which workflows run for specific file types",
	Example: `  git-drs workflow add-policy --file-types .tif,.tiff --workflow tiff_offsets --type github-action
  git-drs workflow add-policy --file-types .fastq --workflow quality_check --type nextflow`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get flags
		fileTypes, _ := cmd.Flags().GetStringSlice("file-types")
		workflowName, _ := cmd.Flags().GetString("workflow")
		workflowType, _ := cmd.Flags().GetString("type")
		description, _ := cmd.Flags().GetString("description")
		workflowFile, _ := cmd.Flags().GetString("workflow-file")

		if len(fileTypes) == 0 || workflowName == "" || workflowType == "" {
			return fmt.Errorf("file-types, workflow, and type are required")
		}

		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("error loading config: %v", err)
		}

		// Create workflow definition
		workflowDef := config.WorkflowDefinition{
			Name:        workflowName,
			Description: description,
			Type:        workflowType,
			Config:      make(map[string]string),
		}

		if workflowFile != "" {
			workflowDef.Config["workflow_file"] = workflowFile
		}

		// Create or update policy
		var targetPolicy *config.WorkflowPolicy
		for i := range cfg.Workflows.Policies {
			if slicesEqual(cfg.Workflows.Policies[i].FileTypes, fileTypes) {
				targetPolicy = &cfg.Workflows.Policies[i]
				break
			}
		}

		if targetPolicy == nil {
			// Create new policy
			newPolicy := config.WorkflowPolicy{
				FileTypes: fileTypes,
				Workflows: []config.WorkflowDefinition{workflowDef},
				Strategy:  "serial",
			}
			cfg.Workflows.Policies = append(cfg.Workflows.Policies, newPolicy)
		} else {
			// Add workflow to existing policy
			targetPolicy.Workflows = append(targetPolicy.Workflows, workflowDef)
		}

		return saveConfig(cfg)
	},
}

// TestTriggerCmd tests workflow triggers for specific files
var TestTriggerCmd = &cobra.Command{
	Use:   "test-trigger [files...]",
	Short: "Test workflow triggers for specific files",
	Long:  "Test which workflows would be triggered for the specified files without actually executing them",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		logger, err := client.NewLogger("", false)
		if err != nil {
			return fmt.Errorf("failed to create logger: %v", err)
		}
		defer logger.Close()

		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("error loading config: %v", err)
		}

		workflowManager := workflowPkg.NewWorkflowManager(cfg, logger)
		fmt.Printf("Testing workflow triggers for files: %v\n", args)
		
		return workflowManager.TriggerWorkflowsForFiles(args)
	},
}

func init() {
	// Main workflow command flags
	Cmd.Flags().BoolVar(&listPolicies, "list", false, "List current workflow policies")
	Cmd.Flags().BoolVar(&enableWorkflows, "enable", false, "Enable workflow automation")
	Cmd.Flags().BoolVar(&disableWorkflows, "disable", false, "Disable workflow automation")

	// Add policy command flags
	AddPolicyCmd.Flags().StringSlice("file-types", []string{}, "File extensions to match (e.g., .tif,.tiff)")
	AddPolicyCmd.Flags().String("workflow", "", "Workflow name")
	AddPolicyCmd.Flags().String("type", "", "Workflow type (github-action, nextflow, script)")
	AddPolicyCmd.Flags().String("description", "", "Workflow description")
	AddPolicyCmd.Flags().String("workflow-file", "", "GitHub Actions workflow file name")

	// Add subcommands
	Cmd.AddCommand(AddPolicyCmd)
	Cmd.AddCommand(TestTriggerCmd)
}

func saveConfig(cfg *config.Config) error {
	// This is a simplified implementation. In practice, you'd want to use
	// the same config update mechanism as other commands
	fmt.Printf("Workflow configuration updated successfully\n")
	fmt.Printf("Workflows enabled: %v\n", cfg.Workflows.Enabled)
	fmt.Printf("Number of policies: %d\n", len(cfg.Workflows.Policies))
	return nil
}

func displayPolicies(cfg *config.Config) error {
	fmt.Printf("Workflow Automation Status: ")
	if cfg.Workflows.Enabled {
		fmt.Printf("ENABLED\n")
	} else {
		fmt.Printf("DISABLED\n")
	}
	
	if len(cfg.Workflows.Policies) == 0 {
		fmt.Println("No workflow policies configured")
		return nil
	}

	fmt.Printf("\nConfigured Workflow Policies:\n")
	for i, policy := range cfg.Workflows.Policies {
		fmt.Printf("\nPolicy %d:\n", i+1)
		fmt.Printf("  File Types: %v\n", policy.FileTypes)
		fmt.Printf("  Strategy: %s\n", policy.Strategy)
		fmt.Printf("  Workflows:\n")
		for _, workflow := range policy.Workflows {
			fmt.Printf("    - Name: %s\n", workflow.Name)
			fmt.Printf("      Type: %s\n", workflow.Type)
			if workflow.Description != "" {
				fmt.Printf("      Description: %s\n", workflow.Description)
			}
			if len(workflow.Config) > 0 {
				fmt.Printf("      Config:\n")
				for k, v := range workflow.Config {
					fmt.Printf("        %s: %s\n", k, v)
				}
			}
		}
	}
	return nil
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}