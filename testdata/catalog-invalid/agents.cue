package catalog

// This entry violates #KnownAgent: name is empty (fails string & !="") and the
// required provider list is omitted. Evaluating the module against its bundled
// schema must fail the load rather than yield a partial catalog.
agents: "broken-agent": {
	name: ""
	bin:  "broken"
	config: {
		global: "~/.broken"
	}
}
