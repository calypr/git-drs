package workflow

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/calypr/git-drs/config"
)

// WorkflowManager handles workflow triggering and execution
type WorkflowManager struct {
	config *config.Config
	logger Logger
}

// Logger interface for workflow operations
type Logger interface {
	Log(msg ...interface{})
	Logf(format string, args ...interface{})
}

// NewWorkflowManager creates a new workflow manager
func NewWorkflowManager(cfg *config.Config, logger Logger) *WorkflowManager {
	return &WorkflowManager{
		config: cfg,
		logger: logger,
	}
}

// TriggerWorkflowsForFiles determines which workflows should run for the given files
func (wm *WorkflowManager) TriggerWorkflowsForFiles(filePaths []string) error {
	if !wm.config.Workflows.Enabled {
		wm.logger.Log("Workflows are disabled, skipping workflow triggers")
		return nil
	}

	if len(wm.config.Workflows.Policies) == 0 {
		wm.logger.Log("No workflow policies configured")
		return nil
	}

	// Group files by workflow policies
	workflowsToRun := make(map[string][]string) // workflow name -> files

	for _, filePath := range filePaths {
		fileExt := strings.ToLower(filepath.Ext(filePath))
		
		for _, policy := range wm.config.Workflows.Policies {
			// Check if file extension matches any in the policy
			for _, policyExt := range policy.FileTypes {
				if fileExt == strings.ToLower(policyExt) {
					// File matches this policy, add all workflows
					for _, workflow := range policy.Workflows {
						if workflowsToRun[workflow.Name] == nil {
							workflowsToRun[workflow.Name] = []string{}
						}
						workflowsToRun[workflow.Name] = append(workflowsToRun[workflow.Name], filePath)
					}
					break
				}
			}
		}
	}

	// Execute workflows
	for workflowName, matchingFiles := range workflowsToRun {
		wm.logger.Logf("Triggering workflow '%s' for files: %v", workflowName, matchingFiles)
		err := wm.executeWorkflow(workflowName, matchingFiles)
		if err != nil {
			wm.logger.Logf("Error executing workflow '%s': %v", workflowName, err)
			return err
		}
	}

	return nil
}

// executeWorkflow executes a specific workflow for the given files
func (wm *WorkflowManager) executeWorkflow(workflowName string, files []string) error {
	// Find the workflow definition
	var workflowDef *config.WorkflowDefinition
	for _, policy := range wm.config.Workflows.Policies {
		for _, workflow := range policy.Workflows {
			if workflow.Name == workflowName {
				workflowDef = &workflow
				break
			}
		}
		if workflowDef != nil {
			break
		}
	}

	if workflowDef == nil {
		return fmt.Errorf("workflow definition not found for '%s'", workflowName)
	}

	wm.logger.Logf("Executing workflow '%s' of type '%s'", workflowName, workflowDef.Type)

	switch workflowDef.Type {
	case "github-action":
		return wm.triggerGitHubAction(workflowDef, files)
	case "nextflow":
		return wm.triggerNextflow(workflowDef, files)
	case "script":
		return wm.runScript(workflowDef, files)
	default:
		return fmt.Errorf("unsupported workflow type: %s", workflowDef.Type)
	}
}

// triggerGitHubAction triggers a GitHub Actions workflow
func (wm *WorkflowManager) triggerGitHubAction(workflow *config.WorkflowDefinition, files []string) error {
	wm.logger.Logf("Triggering GitHub Action workflow: %s", workflow.Name)
	// For now, just log the action. In a full implementation, this would
	// use GitHub API to trigger a workflow_dispatch event
	workflowFile := workflow.Config["workflow_file"]
	if workflowFile == "" {
		workflowFile = fmt.Sprintf("%s.yml", workflow.Name)
	}
	
	wm.logger.Logf("Would trigger GitHub Action: %s with files: %v", workflowFile, files)
	// TODO: Implement actual GitHub API call to trigger workflow_dispatch
	return nil
}

// triggerNextflow triggers a Nextflow pipeline
func (wm *WorkflowManager) triggerNextflow(workflow *config.WorkflowDefinition, files []string) error {
	wm.logger.Logf("Triggering Nextflow pipeline: %s", workflow.Name)
	// TODO: Implement Nextflow pipeline execution
	wm.logger.Logf("Would run Nextflow pipeline for files: %v", files)
	return nil
}

// runScript executes a custom script
func (wm *WorkflowManager) runScript(workflow *config.WorkflowDefinition, files []string) error {
	wm.logger.Logf("Running script workflow: %s", workflow.Name)
	// TODO: Implement script execution
	scriptPath := workflow.Config["script_path"]
	wm.logger.Logf("Would execute script: %s with files: %v", scriptPath, files)
	return nil
}