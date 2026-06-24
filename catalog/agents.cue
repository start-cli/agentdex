package catalog

agents: "claude-code": {
	name:        "Claude Code"
	bin:         "claude"
	description: "Anthropic's agentic coding tool that runs in the terminal."
	config: {
		global: "~/.claude"
		local:  ".claude"
	}
	skills: {
		global: "~/.claude/skills"
		local:  ".claude/skills"
	}
	version: {
		args:    ["--version"]
		pattern: "([0-9]+\\.[0-9]+\\.[0-9]+)"
	}
	provider: ["anthropic"]
	homepage: "https://github.com/anthropics/claude-code"
}
