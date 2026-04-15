package runtime

import (
	"github.com/mark3labs/mcp-go/mcp"
)

// createToolCallRequest builds a CallToolRequest for testing.
func createToolCallRequest(name string, args map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      name,
			Arguments: args,
		},
	}
}
