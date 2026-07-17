package catalog

// #KnownAgent is one catalog entry: the static, outside facts about an agent.
// The agent id is the map key (see agents below); there is no id field here.
// When agnostic is true the entry has no home provider and forbids provider;
// when false (the default) provider is required as the models.dev join key.
#KnownAgent: {
	name:         string & !=""
	bin:          string & !=""
	description?: string
	config: {
		global: string & !=""
		local?: string & !=""
	}
	skills?: {
		global: string & !=""
		local?: string & !=""
	}
	version?: {
		args: [string, ...string] // appended to the detected binary, e.g. ["--version"]
		pattern?: string          // optional regex to extract the version
	}
	agnostic: bool | *false
	if !agnostic {
		provider: [string, ...string] // models.dev provider ids; the join key; at least one required
	}
	homepage?: string
}

// The map key is the agent id, the single source of identity.
agents: [=~"^[a-z0-9]+(-[a-z0-9]+)*$"]: #KnownAgent
