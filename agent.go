package agentdex

import "github.com/start-cli/agentdex/modelsdev"

// Agent is the result of detecting one known agent on this machine. It is the
// catalog's static facts joined with what detection found on disk and, when a
// models.dev client is attached, enriched provider and model data.
type Agent struct {
	ID          string            // catalog id, e.g. "claude-code"
	Name        string            // display name
	Bin         string            // binary name from the catalog
	Found       bool              // binary located on PATH or a search dir
	BinaryPath  string            // absolute path when Found
	Version     string            // resolved version, "" if unknown or skipped
	Config      ResolvedPaths     // resolved global/local config dirs, existence per scope
	Skills      ResolvedPaths     // resolved global/local skills dirs; zero value if the agent has no skills concept
	Providers   []string          // models.dev provider id(s)
	ProviderEnv map[string]bool   // provider API-key env var -> present in env; nil when models.dev was not consulted or degraded, non-nil (possibly empty) once consulted
	Models      []modelsdev.Model // enriched; nil unless EnrichModels() was passed to WithModels
	Homepage    string
}

// ResolvedPaths is a catalog PathPair after tilde, environment, and
// working-directory expansion, with existence recorded per scope. Global and
// Local hold the resolved paths whether or not they exist on disk; GlobalExists
// and LocalExists report existence. Local is "" when the catalog defines no
// local scope, and the zero value of the whole struct means the agent has no
// such concept (the Skills field uses it when there are no skills).
type ResolvedPaths struct {
	Global       string
	GlobalExists bool
	Local        string // "" when the catalog defines no local scope
	LocalExists  bool
}

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
