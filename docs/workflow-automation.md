# Git DRS Workflow Automation

Git DRS now supports automatic workflow triggering based on file types. This feature allows you to automatically run workflows (like TIFF offset calculation) when DRS objects are created for specific file types.

## Configuration

Workflow automation is configured through the git-drs config file (`.drs/config.yaml`). Here's an example configuration:

```yaml
current_server: gen3
servers:
  gen3:
    endpoint: "https://your-gen3-server.com"
    profile: "default"
    project_id: "your-project"
    bucket: "your-bucket"

workflows:
  enabled: true
  policies:
    - file_types: [".tif", ".tiff"]
      strategy: "serial"
      workflows:
        - name: "tiff_offsets"
          type: "github-action"
          description: "Calculate TIFF file offsets for Avivator"
          config:
            workflow_file: "tiff_offsets.yml"
    - file_types: [".fastq", ".fq"]
      strategy: "parallel"
      workflows:
        - name: "quality_check"
          type: "nextflow"
          description: "Quality control for FASTQ files"
```

## Commands

### Enable/Disable Workflow Automation

```bash
# Enable workflow automation
git drs workflow --enable

# Disable workflow automation
git drs workflow --disable

# Check current status and policies
git drs workflow --list
```

### Add Workflow Policies

```bash
# Add a policy for TIFF files to trigger offset calculation
git drs workflow add-policy \
  --file-types .tif,.tiff \
  --workflow tiff_offsets \
  --type github-action \
  --description "Calculate TIFF offsets for Avivator" \
  --workflow-file tiff_offsets.yml

# Add a policy for FASTQ files to trigger quality control
git drs workflow add-policy \
  --file-types .fastq,.fq \
  --workflow quality_check \
  --type nextflow \
  --description "Quality control for sequencing data"
```

### Test Workflow Triggers

```bash
# Test which workflows would be triggered for specific files
git drs workflow test-trigger sample.tif data.fastq
```

## How It Works

1. **During Commit**: When you run `git commit`, the pre-commit hook (`git drs precommit`) is triggered
2. **DRS Object Creation**: The hook creates DRS objects for any new LFS files
3. **Workflow Triggering**: After DRS objects are created, the system checks workflow policies
4. **File Type Matching**: Files are matched against the `file_types` patterns in each policy
5. **Workflow Execution**: Matching workflows are triggered based on their type:
   - `github-action`: Triggers a GitHub Actions workflow_dispatch event
   - `nextflow`: Executes a Nextflow pipeline
   - `script`: Runs a custom script

## Workflow Types

### GitHub Actions (`github-action`)
- Triggers a workflow_dispatch event on the configured workflow file
- Passes the list of matching files as input parameters
- Example use case: TIFF offset calculation for Avivator

### Nextflow (`nextflow`)
- Executes a Nextflow pipeline with the matching files as input
- Example use case: Bioinformatics quality control pipelines

### Script (`script`)
- Runs a custom script with the matching files as arguments
- Example use case: Custom data processing scripts

## Example: TIFF Offset Calculation

The included `tiff_offsets.yml` GitHub Actions workflow demonstrates automatic TIFF offset calculation:

1. When TIFF files are committed, the workflow policy triggers the GitHub Action
2. The action processes the TIFF files and calculates offset information
3. Results are saved to `.drs/offsets/tiff_offsets.json`
4. The offset file is optionally committed back to the repository

This addresses the use case mentioned in the issue where "Avivator offsets files shouldn't be user-generated" by automating their creation.

## Integration with Existing Git DRS Workflow

The workflow automation integrates seamlessly with the existing git-drs workflow:

1. `git lfs track *.tif` - Track TIFF files with LFS
2. `git add sample.tif` - Stage a TIFF file
3. `git commit` - Triggers pre-commit hook
   - Creates DRS object for the TIFF file
   - **NEW**: Triggers TIFF offset calculation workflow
4. `git push` - Pushes changes and any generated artifacts

The workflow triggering happens automatically without disrupting the existing git-drs functionality.