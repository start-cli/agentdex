package catalog

// Synthetic agents spanning more than one provider, with the optional fields
// (description, config.local, skills, version, homepage) present and absent in
// different combinations, so map iteration, id-from-key, and optional-field
// decoding are all exercised across entries.

agents: "alpha-cli": {
	name:        "Alpha CLI"
	bin:         "alpha"
	description: "Synthetic Anthropic-backed agent."
	config: {
		global: "~/.alpha"
		local:  ".alpha"
	}
	skills: {
		global: "~/.alpha/skills"
		local:  ".alpha/skills"
	}
	version: {
		args:    ["--version"]
		pattern: "v([0-9.]+)"
	}
	provider: ["anthropic"]
	homepage: "https://example.com/alpha"
}

agents: "beta-tool": {
	name: "Beta Tool"
	bin:  "beta"
	config: {
		global: "~/.config/beta"
	}
	version: {
		args: ["version"]
	}
	provider: ["openai"]
}

agents: "gamma-agent": {
	name:        "Gamma Agent"
	bin:         "gamma"
	description: "Synthetic multi-provider agent."
	config: {
		global: "~/.gamma"
		local:  ".gamma"
	}
	skills: {
		global: "~/.agents/skills"
		local:  ".agents/skills"
	}
	provider: ["google", "openai"]
	homepage: "https://example.com/gamma"
}

agents: "delta-agent": {
	name:        "Delta Agent"
	bin:         "delta"
	description: "Synthetic provider-agnostic agent."
	config: {
		global: "~/.delta"
		local:  ".delta"
	}
	skills: {
		global: "~/.agents/skills"
		local:  ".agents/skills"
	}
	version: {
		args:    ["--version"]
		pattern: "v([0-9.]+)"
	}
	agnostic: true
	homepage: "https://example.com/delta"
}
