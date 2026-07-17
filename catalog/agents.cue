package catalog

agents: "agy": {
	name:        "Antigravity CLI"
	bin:         "agy"
	description: "Google's terminal-based AI coding agent, successor to Gemini CLI."
	config: {
		global: "~/.gemini/antigravity-cli"
		local:  ".agents"
	}
	skills: {
		global: "~/.agents/skills"
		local:  ".agents/skills"
	}
	version: {
		args:    ["--version"]
		pattern: "([0-9]+\\.[0-9]+\\.[0-9]+)"
	}
	provider: ["google"]
	homepage: "https://github.com/google-antigravity/antigravity-cli"
}

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

agents: "opencode": {
	name:        "opencode"
	bin:         "opencode"
	description: "Open-source, provider-agnostic AI coding agent for the terminal."
	config: {
		global: "~/.config/opencode"
		local:  ".opencode"
	}
	skills: {
		global: "~/.agents/skills"
		local:  ".agents/skills"
	}
	version: {
		args:    ["--version"]
		pattern: "([0-9]+\\.[0-9]+\\.[0-9]+)"
	}
	agnostic: true
	homepage: "https://opencode.ai"
}
