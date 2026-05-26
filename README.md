# ModernPath CLI

A portable command-line tool for working with ModernPath system documentation and analysis.

> **Canonical reference:** See [`docs/cli.md`](../../docs/cli.md) for the full command tree, authentication flows, MCP integration, CI/CD examples, configuration, and troubleshooting. This README is a short binary-level overview for contributors building from source.

## Features

- **Initialize projects** with architecture data from ModernPath platform
- **Search** documentation and code analysis offline
- **Build context** for AI-assisted development
- **Review** git changes against architecture documentation
- **Sync** updates from the platform
- **Dev integration** with AI coding agents (OpenCode, Cursor, Claude Code)
- **Task implementation** by launching AI agents with full context
- **Iterative development loops** (Ralph Wiggum technique) for autonomous implementation

## Installation

### Build from source

```bash
cd tools/modernpath
go build -o modernpath .
```

### Install globally

```bash
go install github.com/modernpath/cli@latest
```

Or copy the binary to your PATH:

```bash
cp tools/modernpath/modernpath /usr/local/bin/
```

## Quick Start

```bash
# By default, connects to production (beta.modernpath.ai)
# For local development, use: modernpath init --local

# Initialize in your project directory
modernpath init

# Select an architecture from the list, or specify directly:
modernpath init --arch-id=3
modernpath init --arch="my-project"

# Search documentation
modernpath search "authentication"

# Ask questions about the codebase
modernpath ask "How does authentication work?"

# Review your git changes
modernpath review
modernpath review --staged
modernpath review --base=main

# Check status
modernpath status

# Sync documentation from platform
modernpath docs sync

# Generate documentation via AI analysis
modernpath docs generate
```

## Commands

### `modernpath ask <question>`

Ask a natural language question about your codebase using the ModernPath platform's agentic search.

```bash
modernpath ask "How does authentication work?"
modernpath ask "What are the main components?"
modernpath ask "Where is error handling implemented?"
```

Uses the platform's agentic search to find relevant documentation and code, then synthesizes an answer.

### `modernpath new`

Create a new project from scratch using the Builder API. Creates a project folder (like `git clone`).

```bash
# Interactive - creates project folder
modernpath new
# → Creates: my-awesome-project/

# Quick create with name
modernpath new "My API"
# → Creates: my-api/

# With profile (partial name match)
modernpath new "Backend" --profile="Python" --desc="REST API for..."

# Let AI recommend the tech stack based on your description
modernpath new "My Project" --profile=ai --desc="Modern SaaS for..."

# From markdown file
modernpath new --desc-file=./PROJECT.md

# Initialize in CURRENT directory (don't create folder)
modernpath new --in-place
```

### `modernpath import`

Import an existing codebase to create a new ModernPath architecture.

```bash
modernpath import                    # Interactive - detects git and prompts
modernpath import --git              # Force import via git URL
modernpath import --local            # Force import local files (zip upload)
modernpath import --name="My App"    # Specify architecture name
```

### `modernpath init`

Initialize ModernPath in the current directory for an **existing** architecture.

```bash
modernpath init                    # Interactive - select from list
modernpath init --arch-id=3        # Specify by ID
modernpath init --arch="my-app"    # Specify by name (partial match)
modernpath init --force            # Reinitialize
modernpath init --local            # Use localhost:4000 instead of production
```

### `modernpath search <query>`

Search through documentation and analyzed code files.

```bash
modernpath search "authentication"
modernpath search "error handling" --docs-only
modernpath search "API client" --files-only
modernpath search "config" --limit=20
```

### `modernpath review`

Analyze git diff and provide architecture context for code review.

```bash
modernpath review                  # Review uncommitted changes
modernpath review --staged         # Staged changes only
modernpath review --base=main      # Compare against branch
modernpath review --files          # Just list affected files
modernpath review -v               # Include full diff
```

### `modernpath docs`

Manage codebase documentation and analysis.

#### `modernpath docs sync`

Download the latest documentation and analysis data from the ModernPath platform.

```bash
modernpath docs sync
```

#### `modernpath docs generate`

Run AI analysis to generate codebase documentation.

```bash
modernpath docs generate
```

### `modernpath status`

Show current project status and database statistics.

```bash
modernpath status
```

### `modernpath env`

View and manage which ModernPath environment the CLI connects to.

```bash
modernpath env                    # Show current environment
modernpath env list               # List all available environments
modernpath env test               # Test connection to current environment
modernpath env --set=production   # Switch to production (beta.modernpath.ai)
modernpath env --set=local        # Switch to local development (localhost:4000)
```

### `modernpath auth`

Authenticate with the ModernPath platform.

```bash
modernpath auth                    # Interactive browser login
modernpath auth --local            # Login to localhost:4000
modernpath auth --token=<token>    # Provide token directly
modernpath auth --logout           # Clear credentials
```

### `modernpath dev`

Launch AI coding agents with automatic ModernPath MCP configuration.

```bash
# List available tools
modernpath dev list

# Setup MCP for a tool
modernpath dev setup opencode
modernpath dev setup cursor

# Simple one-shot task execution
modernpath dev opencode "implement user authentication"
modernpath dev cursor "fix the login bug"
```

**Subcommands:**

#### `modernpath dev task [task_id]`

Start implementing a task with full context (one-shot execution).

```bash
# Interactive - select task from list
modernpath dev task

# Specify task ID
modernpath dev task abc123-uuid-here

# Use specific tool
modernpath dev task --tool=opencode
modernpath dev task abc123 --tool=cursor
```

This command:
1. Fetches the task details and user stories
2. Retrieves related specifications
3. Builds a comprehensive implementation prompt
4. Saves it to `.modernpath/current-task.md`
5. Launches your chosen AI coding agent once

#### `modernpath dev ralph [task_id]`

Run an AI coding agent in an **iterative loop** until the task is complete.

Based on the [Ralph Wiggum technique](https://ghuntley.com/ralph/), this runs the AI agent repeatedly with the same prompt until it outputs a completion promise.

```bash
# Interactive - select task
modernpath dev ralph

# Specific task with options
modernpath dev ralph abc123-uuid --max-iterations 10

# Use specific tool
modernpath dev ralph --tool opencode

# Work through ALL pending tasks in logical order
modernpath dev ralph --all

# Check loop status (from another terminal)
modernpath dev ralph status
```

**Options:**
- `--max-iterations N` - Stop after N iterations (default: 20)
- `--min-iterations N` - Minimum iterations before completion allowed (default: 1)
- `--completion-promise TEXT` - Phrase that signals completion (default: "COMPLETE")
- `--tool TOOL` - AI tool to use (opencode, cursor, claude)
- `--no-commit` - Don't auto-commit after iterations
- `--all` - Work through ALL pending tasks in logical order
- `--yes-all` - Tell AI to auto-approve all actions

**Supported Tools:**
- **opencode** - [OpenCode](https://opencode.ai) - Open source AI coding agent
- **cursor** - [Cursor](https://cursor.sh) - AI-powered code editor
- **claude** - [Claude Code](https://claude.ai) - Anthropic's coding assistant CLI
- **codex** - OpenAI Codex CLI

### `modernpath work`

View and manage work items (epics, tasks, subtasks).

```bash
# List epics for current architecture
modernpath work list

# Select an epic to work on (interactive)
modernpath work select

# Select a specific epic by ID
modernpath work select 34

# Show comprehensive status (specs + tasks)
modernpath work status

# List tasks for current epic
modernpath work tasks

# List tasks for specific epic
modernpath work tasks 34

# List subtasks for a task
modernpath work subtasks abc123-uuid-here
```

#### `modernpath work new [description]`

Create a new epic from a description.

```bash
# Create from command line argument
modernpath work new "Add user authentication with OAuth2"

# Create from stdin
echo "Add user authentication" | modernpath work new

# Create from file
modernpath work new < feature.md

# Specify idea type
modernpath work new --type=innovation "Implement AI-powered code review"
```

**What it does:**
1. Uses LLM to generate a structured idea from your description
2. Creates an epic linked to the idea
3. Automatically selects the new epic as active in `.modernpath/config.json`

#### `modernpath work specs`

Manage specifications for an epic.

```bash
# Generate specifications (triggers pipeline)
modernpath work specs generate

# Download specifications to the active epic workspace under .modernpath/tasks/<epic-slug>/
modernpath work specs sync

# Push local specifications back to the platform
modernpath work specs push
```

Specifications are organized by category:
- `discovery/` - Requirements & User Stories
- `architecture/` - Component specs & C4 diagrams
- `data/` - ERD & Data dictionary
- `ui_ux/` - Wireframes & Design system
- `testing/` - Test plans & Coverage matrix
- `validation/` - Cross-check outputs

#### `modernpath work derive`

Derive tasks and subtasks from specifications.

```bash
modernpath work derive
```

This analyzes your specifications and creates:
- **Tasks** from architecture components
- **Stories** from requirements and flows
- **Subtasks** from interfaces and data entities
- **Dependencies** between work items

**Note:** When you select an epic with `modernpath work select`, it automatically:
- Updates `.modernpath/config.json` with the selected epic
- Syncs specifications to `.modernpath/tasks/<epic-slug>/` (category subfolders)
- Downloads task context files into the same epic folder (`.md` files at the root)

## Configuration

Configuration is stored in `.modernpath/`:

```
.modernpath/
├── config.json           # Project settings
├── auth.json             # Credentials (gitignored)
├── {slug}.sqlite         # SQLite database
├── docs/                 # Markdown documentation
└── tasks/                # Epic workspaces (synced when selecting epic)
    └── {id}-{slug}/      # One folder per epic
        ├── architecture/ # Spec categories (subfolders)
        ├── requirements/
        ├── {uuid}-*.md   # Task context files (at epic root)
        └── ...
```

### config.json

```json
{
  "api_url": "https://beta.modernpath.ai",
  "system_id": 3,
  "system_name": "my-project",
  "system_slug": "my-project",
  "initiative_id": 5,
  "initiative_name": "New Feature",
  "last_sync_at": "2024-01-15T10:30:00Z"
}
```

### Environments

ModernPath CLI supports multiple environments:

| Environment | URL | Description |
|-------------|-----|-------------|
| `production` | https://beta.modernpath.ai | Production server (default) |
| `local` | http://localhost:4000 | Local development server |
| `custom` | (user-defined) | Any custom URL |

**Switch environments:**
```bash
modernpath env --set=local        # Use local development server
modernpath env --set=production   # Switch back to production
modernpath env test               # Test current connection
```

## Global Flags

```
--api-url string   Override API URL
-v, --verbose      Verbose output
-h, --help         Help for command
--version          Show version
```

## Cross-Platform Builds

```bash
# macOS (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o modernpath-darwin-arm64 .

# macOS (Intel)
GOOS=darwin GOARCH=amd64 go build -o modernpath-darwin-amd64 .

# Linux
GOOS=linux GOARCH=amd64 go build -o modernpath-linux-amd64 .

# Windows
GOOS=windows GOARCH=amd64 go build -o modernpath-windows-amd64.exe .
```

## Development

```bash
# Run without building
go run . --help

# Run tests
go test ./...

# Format code
go fmt ./...
```

## License

MIT
