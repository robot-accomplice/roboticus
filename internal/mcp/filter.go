package mcp

// FilterByAllowlist returns only tools whose names are in the allowlist.
// If the allowlist is empty, all tools are returned unchanged.
func FilterByAllowlist(tools []ToolDescriptor, allowlist []string) []ToolDescriptor {
	if len(allowlist) == 0 {
		return tools
	}

	allowed := make(map[string]bool, len(allowlist))
	for _, name := range allowlist {
		allowed[name] = true
	}

	var filtered []ToolDescriptor
	for _, t := range tools {
		if allowed[t.Name] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}
