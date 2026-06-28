// Package match holds the shared none/one/many selector rule used to resolve a
// fuzzy query against a set of id/name candidates. The root package's
// ResolveModel and the CLI's selectors both import it so the model selector and
// the CLI selectors cannot drift. It operates on a plain id/name set
// and depends on no agentdex types, so it introduces no import cycle and stays off
// the public API.
package match

import "strings"

// Item is one candidate in a selector set: its canonical id and display name.
type Item struct {
	ID   string
	Name string
}

// Outcome is the result kind of a Match.
type Outcome int

const (
	// None means no candidate matched the query.
	None Outcome = iota
	// Unique means exactly one candidate matched; the returned index identifies it.
	Unique
	// Ambiguous means several candidates matched at the resolving stage; their
	// ids are returned as candidates.
	Ambiguous
)

// Match resolves query against items using the shared rule, in order:
//
//  1. exact id (case-sensitive)
//  2. exact name, case-insensitive
//  3. substring (a prefix is a substring) on id or name, case-insensitive
//
// A stage short-circuits as soon as it matches at least one candidate; later
// stages are not consulted. Every stage applies the same none/one/many rule, so
// duplicate exact ids or names across providers are reported as Ambiguous rather
// than silently resolving to the first candidate. Within a winning stage a single
// match returns Unique with the matching item's index in items; several return
// Ambiguous with their ids in item order and an index of -1; an empty final stage
// returns None with index -1.
func Match(query string, items []Item) (Outcome, int, []string) {
	var exactID []int
	for i, it := range items {
		if it.ID == query {
			exactID = append(exactID, i)
		}
	}
	if len(exactID) > 0 {
		return resolve(items, exactID)
	}

	q := strings.ToLower(query)
	var exactName []int
	for i, it := range items {
		if strings.ToLower(it.Name) == q {
			exactName = append(exactName, i)
		}
	}
	if len(exactName) > 0 {
		return resolve(items, exactName)
	}

	var substring []int
	for i, it := range items {
		if strings.Contains(strings.ToLower(it.ID), q) || strings.Contains(strings.ToLower(it.Name), q) {
			substring = append(substring, i)
		}
	}
	return resolve(items, substring)
}

// resolve maps a winning stage's matched item indices to the none/one/many
// outcome: zero matches is None, exactly one is Unique with that index, and
// several is Ambiguous with the matched ids in item order.
func resolve(items []Item, matches []int) (Outcome, int, []string) {
	switch len(matches) {
	case 0:
		return None, -1, nil
	case 1:
		return Unique, matches[0], nil
	default:
		ids := make([]string, len(matches))
		for i, idx := range matches {
			ids[i] = items[idx].ID
		}
		return Ambiguous, -1, ids
	}
}
