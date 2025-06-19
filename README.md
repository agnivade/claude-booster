# Claude Booster

A faster, cheaper and more tunable Claude Code.

## Features

- **Disable Haiku Generation**: Optionally suppress those one word messages that appear when it's processing a request.
- **Token Count Caching**: Cache `/v1/messages/count_tokens` responses to avoid redundant API calls.
- **Temperature Control**: Set custom temperature values for Claude Sonnet 4 requests.
- **Tunable Prompts**: You set your own prompts. Tune everything to your desire.
- **Improved Prompt Caching**: It re-arranges the prompt to places "tools", and "system" block before "messages" block, leading to much better caching of input tokens.

## Installation

```bash
git clone <repository-url>
cd claude-booster
go build -o claude-booster .
```

## Usage

### Basic Usage

```bash
./claude-booster -target https://api.anthropic.com -root-dir /path/to/your/project
```

And then type `ANTHROPIC_BASE_URL=http://localhost:8080 claude`

### Command Line Flags

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `-target` | Yes | - | Target URL to proxy to (e.g., `https://api.anthropic.com`) |
| `-root-dir` | Yes | - | Root directory for project files containing CLAUDE.md files |
| `-addr` | No | `localhost` | Listen address |
| `-port` | No | `8080` | Listen port |
| `-suppress-haiku` | No | `false` | Enable haiku generation suppression |
| `-temperature` | No | `0.1` | Temperature for Claude Sonnet 4 requests |

## Template System

Claude Booster includes a powerful templating system that allows you to inject dynamic content into your prompts based on project context.

### Template Files

The system loads content from three sources:

1. **User Private Instructions**: `$HOME/.claude/CLAUDE.md`
   - Your personal global instructions for all projects
   - Always loaded if present

2. **Project Public Instructions**: `{root-dir}/CLAUDE.md`
   - Project-specific instructions checked into version control
   - Shared with all team members

3. **Project Private Instructions**: `{root-dir}/CLAUDE.local.md`
   - Project-specific instructions not checked into version control
   - Personal project customizations

### Template Variables

Use these template variables in your `assets/user_prompt.txt` file:

| Variable | Description | Source File |
|----------|-------------|-------------|
| `{{.UserPrivate}}` | User's global private instructions | `$HOME/.claude/CLAUDE.md` |
| `{{.ProjectPublic}}` | Project's public instructions | `{root-dir}/CLAUDE.md` |
| `{{.ProjectPrivate}}` | Project's private instructions | `{root-dir}/CLAUDE.local.md` |

### Template Example

```html
<system-reminder>
As you answer the user's questions, you can use the following context:
Codebase and user instructions are shown below. Be sure to adhere to these instructions.

{{if .UserPrivate}}Contents of $HOME/.claude/CLAUDE.md (user's private global instructions for all projects):

{{.UserPrivate}}
{{end}}

{{if .ProjectPublic}}Contents of project CLAUDE.md (project instructions, checked into the codebase):

{{.ProjectPublic}}
{{end}}

{{if .ProjectPrivate}}Contents of project CLAUDE.local.md (user's private project instructions, not checked in):

{{.ProjectPrivate}}
{{end}}

# important-instruction-reminders
Do what has been asked; nothing more, nothing less.
NEVER create files unless they're absolutely necessary for achieving your goal.
</system-reminder>
```

### Conditional Rendering

The template system supports conditional rendering using Go template syntax:

```html
{{if .UserPrivate}}
This content only appears if UserPrivate is not empty
{{.UserPrivate}}
{{end}}
```
