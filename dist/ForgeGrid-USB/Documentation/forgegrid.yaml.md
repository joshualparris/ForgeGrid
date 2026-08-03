# forgegrid.yaml

The `forgegrid.yaml` manifest defines the tasks available for a project.

## Structure
- `project`: Name of the project.
- `tasks`: A map of task definitions.

## Task Properties
- `description`: Optional human-readable description.
- `requirements`: Constraints for the scheduler.
  - `min_ram_gb`: Minimum RAM required in GB.
  - `os`: Target operating system (`windows` or `linux`).
- `commands`: The specific commands to run based on the worker's OS.
  - `windows`: Command for Windows workers.
  - `linux`: Command for Linux workers.
- `artefacts`: A list of glob patterns for files to upload back to the coordinator upon completion (e.g. `build/**`).
