package catalog

// A standalone copy of the #KnownAgent schema, so the fixture module exercises
// the identical load-evaluate-validate-decode path the published catalog uses:
// the schema travels with the data and is unified with it at build time.
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
		args: [string, ...string]
		pattern?: string
	}
	provider: [string, ...string]
	homepage?: string
}

agents: [=~"^[a-z0-9]+(-[a-z0-9]+)*$"]: #KnownAgent
