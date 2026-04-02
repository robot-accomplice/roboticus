package tools

import (
	"context"
	"encoding/json"

	"goboticus/internal/core"
)

// Tool is the interface every agent tool must implement.
type Tool interface {
	Name() string
	Description() string
	Risk() RiskLevel
	ParameterSchema() json.RawMessage
	Execute(ctx context.Context, params string, tctx *Context) (*Result, error)
}

// RiskLevel classifies how dangerous a tool invocation is.
type RiskLevel int

const (
	RiskSafe      RiskLevel = iota // No side effects
	RiskCaution                    // Reads data, may expose information
	RiskDangerous                  // Writes data, executes code
	RiskForbidden                  // Never allowed without explicit creator override
)

func (r RiskLevel) String() string {
	switch r {
	case RiskSafe:
		return "safe"
	case RiskCaution:
		return "caution"
	case RiskDangerous:
		return "dangerous"
	case RiskForbidden:
		return "forbidden"
	default:
		return "unknown"
	}
}

// Context provides runtime information to tool execution.
type Context struct {
	SessionID    string
	AgentID      string
	AgentName    string
	Workspace    string
	AllowedPaths []string
	Channel      string
	FS           FileSystem         // file operations; nil defaults to OSFileSystem
	Runner       core.ProcessRunner // subprocess execution; nil defaults to OSProcessRunner
}

// GetFS returns the filesystem, defaulting to real OS operations.
func (c *Context) GetFS() FileSystem {
	if c.FS != nil {
		return c.FS
	}
	return OSFileSystem{}
}

// GetRunner returns the process runner, defaulting to real OS execution.
func (c *Context) GetRunner() core.ProcessRunner {
	if c.Runner != nil {
		return c.Runner
	}
	return core.OSProcessRunner{}
}

// Result holds the output of a tool execution.
type Result struct {
	Output   string
	Metadata json.RawMessage // optional structured data
}
