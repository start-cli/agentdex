// Package modelsdev is a reusable client for models.dev, the community database
// of model specifications, pricing, and capabilities. It fetches the static
// catalog.json, merges the per-provider and provider-agnostic maps into a single
// enriched view, validates against gross schema drift, and caches with
// stale-on-failure semantics. The package is a leaf: it imports no agentdex
// internal package, the agent catalog, or the root package, so external
// consumers can depend on it directly.
package modelsdev

// Catalog is the merged result of fetching models.dev catalog.json: the
// provider-agnostic model map plus the per-provider map, mirroring the upstream
// { models, providers } shape. It is distinct from agentdex.Catalog (the set of
// known agents); package qualification keeps the two unambiguous.
type Catalog struct {
	Models    map[string]Model    `json:"models"`    // provider-agnostic, keyed by path-style model id
	Providers map[string]Provider `json:"providers"` // keyed by provider id
}

// Provider is one models.dev provider and the models it offers.
type Provider struct {
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	Doc    string           `json:"doc"`
	NPM    string           `json:"npm"`
	API    string           `json:"api"`
	Env    []string         `json:"env"` // API-key env var names, e.g. ["ANTHROPIC_API_KEY"]
	Models map[string]Model `json:"models"`
}

// Model is one model entry. The same type serves both maps: in Catalog.Models
// its ID is the path-style provider-agnostic id; within a Provider.Models it is
// the short id local to that provider. ID is never normalised across the two.
type Model struct {
	ID               string      `json:"id"` // source id: path-style in the agnostic map, short within a provider map
	Name             string      `json:"name"`
	Family           string      `json:"family"`
	Attachment       bool        `json:"attachment"`
	Reasoning        bool        `json:"reasoning"`
	ToolCall         bool        `json:"tool_call"`
	StructuredOutput bool        `json:"structured_output"`
	Temperature      bool        `json:"temperature"`
	Knowledge        string      `json:"knowledge"` // YYYY-MM or YYYY-MM-DD
	ReleaseDate      string      `json:"release_date"`
	LastUpdated      string      `json:"last_updated"`
	Modalities       Modalities  `json:"modalities"`
	OpenWeights      bool        `json:"open_weights"`
	Limit            Limit       `json:"limit"`      // token limits; a zero value is legitimate for media-generation models
	Cost             *Cost       `json:"cost"`       // USD per 1,000,000 tokens; nil if unknown
	Status           string      `json:"status"`     // alpha|beta|deprecated
	Benchmarks       []Benchmark `json:"benchmarks"` // merged in from the agnostic map; absent on provider models upstream
	Weights          []Weight    `json:"weights"`    // merged in from the agnostic map; absent on provider models upstream
}

// Modalities lists the input and output media a model accepts and produces,
// each element one of text|audio|image|video|pdf.
type Modalities struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

// Limit holds a model's token limits. An absent upstream limit decodes to the
// zero value, which is legitimate for models that carry no token limit, such as
// image and other media-generation models.
type Limit struct {
	Context int `json:"context"`
	Input   int `json:"input"`
	Output  int `json:"output"`
}

// Cost is per-token pricing in USD per 1,000,000 tokens.
type Cost struct {
	Input           float64 `json:"input"`
	Output          float64 `json:"output"`
	Reasoning       float64 `json:"reasoning"`
	CacheRead       float64 `json:"cache_read"`
	CacheWrite      float64 `json:"cache_write"`
	InputAudio      float64 `json:"input_audio"`
	OutputAudio     float64 `json:"output_audio"`
	ContextOver200K *Cost   `json:"context_over_200k"` // pricing once context exceeds 200k tokens; nil when flat
	Tiers           []Tier  `json:"tiers"`             // tiered pricing; nil when flat
}

// Tier is one entry in a model's tiered pricing: a per-token cost subset plus
// the dimension and threshold at which it applies. Upstream nests the dimension
// under a "tier" object.
type Tier struct {
	Input      float64       `json:"input"`
	Output     float64       `json:"output"`
	CacheRead  float64       `json:"cache_read"`
	CacheWrite float64       `json:"cache_write"`
	Tier       TierDimension `json:"tier"`
}

// TierDimension is the nested "tier" object on each tiered-pricing entry: the
// dimension and the threshold at which the tier takes effect.
type TierDimension struct {
	Type string `json:"type"` // tier dimension, e.g. "context"
	Size int    `json:"size"` // threshold at which this tier takes effect, e.g. 200000
}

// Benchmark is a published benchmark result for a model. It lives only in the
// provider-agnostic map upstream and is merged onto the matching provider model.
type Benchmark struct {
	Name    string  `json:"name"`
	Score   float64 `json:"score"`
	Metric  string  `json:"metric"`
	Source  string  `json:"source"`
	Harness string  `json:"harness"`
	Dataset string  `json:"dataset"`
	Version string  `json:"version"`
	Date    string  `json:"date"`
	Variant string  `json:"variant"`
}

// Weight is a link to a model's published weights. Like Benchmark it lives only
// in the provider-agnostic map and is merged onto the matching provider model.
type Weight struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}
