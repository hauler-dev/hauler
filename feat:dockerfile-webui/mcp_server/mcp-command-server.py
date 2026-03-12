#!/usr/bin/env python3
import asyncio
import json
import sys
import subprocess
from typing import Any

class MCPServer:
    def __init__(self):
        self.tools = [
            {
                "name": "execute_command",
                "description": "Execute bash or docker commands on the local system",
                "inputSchema": {
                    "type": "object",
                    "properties": {
                        "command": {
                            "type": "string",
                            "description": "The command to execute (e.g., 'docker ps', 'ls -la')"
                        },
                        "cwd": {
                            "type": "string",
                            "description": "Working directory for command execution (optional)"
                        }
                    },
                    "required": ["command"]
                }
            }
        ]

    async def handle_message(self, message: dict) -> dict:
        method = message.get("method")
        
        if method == "initialize":
            return {
                "protocolVersion": "2024-11-05",
                "capabilities": {"tools": {}},
                "serverInfo": {"name": "command-executor", "version": "1.0.0"}
            }
        
        elif method == "tools/list":
            return {"tools": self.tools}
        
        elif method == "tools/call":
            return await self.execute_tool(message["params"])
        
        return {"error": "Unknown method"}

    async def execute_tool(self, params: dict) -> dict:
        tool_name = params.get("name")
        args = params.get("arguments", {})
        
        if tool_name == "execute_command":
            command = args.get("command")
            cwd = args.get("cwd", None)
            
            try:
                result = subprocess.run(
                    command,
                    shell=True,
                    capture_output=True,
                    text=True,
                    cwd=cwd,
                    timeout=30
                )
                
                output = f"Exit Code: {result.returncode}\n\nStdout:\n{result.stdout}\n\nStderr:\n{result.stderr}"
                
                return {
                    "content": [{"type": "text", "text": output}]
                }
            except subprocess.TimeoutExpired:
                return {
                    "content": [{"type": "text", "text": "Error: Command timed out after 30 seconds"}],
                    "isError": True
                }
            except Exception as e:
                return {
                    "content": [{"type": "text", "text": f"Error: {str(e)}"}],
                    "isError": True
                }
        
        return {"error": "Unknown tool"}

    async def run(self):
        while True:
            try:
                line = await asyncio.get_event_loop().run_in_executor(None, sys.stdin.readline)
                if not line:
                    break
                
                message = json.loads(line)
                response = await self.handle_message(message)
                
                output = json.dumps({"jsonrpc": "2.0", "id": message.get("id"), "result": response})
                print(output, flush=True)
                
            except json.JSONDecodeError:
                continue
            except Exception as e:
                error_response = json.dumps({
                    "jsonrpc": "2.0",
                    "id": message.get("id") if 'message' in locals() else None,
                    "error": {"code": -32603, "message": str(e)}
                })
                print(error_response, flush=True)

if __name__ == "__main__":
    server = MCPServer()
    asyncio.run(server.run())
