# MCP Command Executor Server

A custom Model Context Protocol (MCP) server that allows Amazon Q to execute bash and Docker commands on your local system.

## Setup

1. Make the server executable:
```bash
chmod +x mcp-command-server.py
```

2. Configure Amazon Q to use this MCP server by adding the configuration from `mcp-config.json` to your MCP settings.

## Usage

The server provides one tool:

- **execute_command**: Execute bash or docker commands
  - `command` (required): The command to execute
  - `cwd` (optional): Working directory for execution

## Examples

```bash
# List Docker containers
docker ps

# Check disk usage
df -h

# List files
ls -la /home/user
```

## Security Note

This server executes commands with your user permissions. Only use with trusted AI assistants and be cautious about what commands are executed.
