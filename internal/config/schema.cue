// #Config is the closed schema for $XDG_CONFIG_HOME/agentdex/config.cue. It is
// closed so an unknown field is a load-time error rather than a silent typo, and
// the fields with a built-in default (catalog.module, color) are non-optional
// with a default so the default materialises on decode even when the user omits
// the field. The remaining fields are optional: absent means "not set", and the
// library or the per-cache TTL resolution supplies the effective value.
#Config: {
	cache_ttl?: string
	catalog: {
		module: string | *"github.com/start-cli/agentdex/catalog@v1"
		ttl?:   string
	}
	models: {
		url?: string
		ttl?: string
	}
	search_dirs?: [...string]
	bin_paths?: [string]: string
	disabled_agents?: [...string]
	color: "auto" | "always" | "never" | *"auto"
}
