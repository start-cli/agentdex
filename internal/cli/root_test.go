package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBareNounIsUsageFault(t *testing.T) {
	// Every noun group invoked without a verb is a usage fault (exit 2). In text
	// mode it prints the group help so the verbs are discoverable.
	newScenario(t, "", "alpha-cli")

	for _, noun := range []string{"agents", "providers", "models"} {
		got := runCLI(noun)
		if got.code != codeUsage {
			t.Errorf("bare %q exit = %d, want 2; stderr=%q", noun, got.code, got.stderr)
		}
		// Text mode: the group help lists the two verbs.
		if !strings.Contains(got.stdout, "list") || !strings.Contains(got.stdout, "get") {
			t.Errorf("bare %q text should print group help naming list and get:\n%s", noun, got.stdout)
		}
	}
}

func TestBareNounJSONIsEnvelopeAlone(t *testing.T) {
	// Under --json the bare-noun fault emits the error envelope alone on stdout,
	// naming the missing verb, with no help text mixed in, so stdout parses as JSON.
	newScenario(t, "", "alpha-cli")

	got := runCLI("--json", "agents")
	if got.code != codeUsage {
		t.Fatalf("bare agents --json exit = %d, want 2; stderr=%q", got.code, got.stderr)
	}
	var env envelope
	if err := json.Unmarshal([]byte(got.stdout), &env); err != nil {
		t.Fatalf("bare-noun --json stdout is not pure JSON: %v\nstdout=%q", err, got.stdout)
	}
	if env.Status != "error" || !strings.Contains(env.Error, "subcommand") {
		t.Errorf("envelope = %+v, want an error naming the missing subcommand", env)
	}
}

func TestSingularNounAliasIsSynonym(t *testing.T) {
	// The singular alias selects the same operation as the plural.
	srv := modelsServer(t, []string{"anthropic"})
	newScenario(t, srv.URL, "alpha-cli")

	plural := runCLI("--json", "agents", "get", "alpha-cli")
	singular := runCLI("--json", "agent", "get", "alpha-cli")
	if plural.code != codeOK || singular.code != codeOK {
		t.Fatalf("agents/agent get exits = %d/%d, want 0/0", plural.code, singular.code)
	}
	if plural.stdout != singular.stdout {
		t.Errorf("agent get differs from agents get:\nplural:\n%s\nsingular:\n%s", plural.stdout, singular.stdout)
	}

	// The other two nouns accept their singular alias too.
	if got := runCLI("--json", "provider", "list"); got.code != codeOK {
		t.Errorf("provider list exit = %d, want 0; stderr=%q", got.code, got.stderr)
	}
	if got := runCLI("--json", "model", "list", "--provider", "anthropic"); got.code != codeOK {
		t.Errorf("model list exit = %d, want 0; stderr=%q", got.code, got.stderr)
	}
}

func TestRemovedFlatCommandsAreGone(t *testing.T) {
	// The old flat commands are gone, split by failure mode. get/list were top-level
	// commands and are now unknown (exit 2); providers/models are noun groups, so a
	// bare or verbless invocation is the usage fault, never the old listing.
	srv := modelsServer(t, []string{"anthropic", "google", "openai"})
	newScenario(t, srv.URL, "alpha-cli")

	// Unknown top-level commands.
	for _, args := range [][]string{{"get", "alpha-cli"}, {"list"}} {
		got := runCLI(args...)
		if got.code != codeUsage {
			t.Errorf("%v exit = %d, want 2 (unknown command); stderr=%q", args, got.code, got.stderr)
		}
	}

	// "agentdex providers" no longer lists: it is the bare-noun usage fault, and no
	// provider row is rendered.
	prov := runCLI("providers")
	if prov.code != codeUsage {
		t.Errorf("providers exit = %d, want 2; stderr=%q", prov.code, prov.stderr)
	}
	if strings.Contains(prov.stdout, "ANTHROPIC_API_KEY") {
		t.Errorf("bare providers should not render the old listing:\n%s", prov.stdout)
	}

	// "agentdex models <id>" no longer lists an agent's models: it is the
	// unknown-verb usage fault.
	mod := runCLI("models", "alpha-cli")
	if mod.code != codeUsage {
		t.Errorf("models <id> exit = %d, want 2; stderr=%q", mod.code, mod.stderr)
	}
	if strings.Contains(mod.stdout, "claude-sonnet") {
		t.Errorf("models <id> should not render the old model listing:\n%s", mod.stdout)
	}
}
