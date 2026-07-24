package agentdex

import (
	"sort"

	"github.com/start-cli/agentdex/modelsdev"
)

// newerModel reports whether a sorts before b in a newest-first listing: later
// release date first (ISO dates compare lexically), undated models last, ties
// broken by id so the order is deterministic. This is the library's one model
// order (R14), applied wherever a model list is attached.
func newerModel(a, b modelsdev.Model) bool {
	if a.ReleaseDate != b.ReleaseDate {
		if a.ReleaseDate == "" {
			return false
		}
		if b.ReleaseDate == "" {
			return true
		}
		return a.ReleaseDate > b.ReleaseDate
	}
	return a.ID < b.ID
}

// sortModelsNewest orders models newest release first, in place.
func sortModelsNewest(models []modelsdev.Model) {
	sort.SliceStable(models, func(i, j int) bool { return newerModel(models[i], models[j]) })
}

// sortModels orders the wrapped Model list newest release first, in place, by the
// same comparator agent Models use so one order reaches every model surface (R14).
func sortModels(models []Model) {
	sort.SliceStable(models, func(i, j int) bool { return newerModel(models[i].Model, models[j].Model) })
}

// sortedKeys returns a map's string keys in ascending order, for deterministic
// iteration over a provider set or a provider's model map.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// dedupeIDs drops duplicate ids preserving first-seen order, so a repeated
// provider id cannot double model candidates or coverage probes downstream.
func dedupeIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
