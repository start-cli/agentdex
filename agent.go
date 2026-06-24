package agentdex

// Catalog is the loaded set of known agents, keyed by catalog id.
type Catalog struct {
	Agents map[string]KnownAgent
}

// KnownAgent is one catalog entry: the static facts about an agent. The ID is
// populated from the catalog map key by the loader; #KnownAgent has no id field.
type KnownAgent struct {
	ID          string
	Name        string
	Bin         string
	Description string // "" when the catalog entry omits it
	Config      PathPair
	Skills      *PathPair     // nil if the agent has no skills concept
	Version     *VersionProbe // nil if version is not resolvable
	Provider    []string
	Homepage    string
}

// PathPair is a catalog global/local directory pair before any expansion.
type PathPair struct {
	Global string // e.g. "~/.claude"
	Local  string // e.g. ".claude"; "" when the catalog defines no local scope
}

// VersionProbe describes how to read an agent's version from its binary.
type VersionProbe struct {
	Args    []string // arguments appended to the detected binary, e.g. ["--version"]
	Pattern string   // optional regex to extract the version from combined stdout+stderr
}
