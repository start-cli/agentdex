package agentdex

import (
	"bytes"
	"context"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// versionTimeout bounds a single version exec. Version resolution is a
// best-effort convenience, so a slow or hung binary must not stall detection.
const versionTimeout = 5 * time.Second

// maxVersionOutput caps how much of each output stream the version probe retains.
// A version banner is a line or two; the timeout bounds how long a binary runs
// but not how much it writes, so this bounds memory and keeps a misbehaving or
// wrongly matched binary from inflating a detection scan. It is far larger than
// any real version string, so it never truncates a genuine result.
const maxVersionOutput = 64 << 10 // 64 KiB per stream

// probeVersion execs the detected binary with the catalog's version args under a
// short timeout and extracts the version from the combined stdout and stderr,
// since some CLIs print their version to stderr. The binary path is used as given
// (already absolute), never re-resolved through PATH. Any failure is non-fatal:
// output produced before the failure is still parsed, and an unparseable result
// yields an empty version rather than a detection error. Each stream is capped so
// a binary that floods output cannot grow the buffers without bound.
func probeVersion(ctx context.Context, binPath string, vp VersionProbe) string {
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, vp.Args...)
	stdout := &cappedBuffer{cap: maxVersionOutput}
	stderr := &cappedBuffer{cap: maxVersionOutput}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	_ = cmd.Run() // non-fatal; whatever output exists is parsed below

	return extractVersion(stdout.String()+stderr.String(), vp.Pattern)
}

// cappedBuffer accumulates up to cap bytes and silently discards the rest. It
// always reports a full write so the child process is never blocked or faulted by
// the cap; the excess is simply dropped, bounding memory while letting the probe
// keep whatever prefix it has captured.
type cappedBuffer struct {
	buf bytes.Buffer
	cap int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.cap - c.buf.Len(); room > 0 {
		if len(p) > room {
			c.buf.Write(p[:room])
		} else {
			c.buf.Write(p)
		}
	}
	return len(p), nil
}

func (c *cappedBuffer) String() string { return c.buf.String() }

// extractVersion applies the catalog pattern to a binary's combined output. With
// no pattern the trimmed output is the version. With a pattern, the first capture
// group is returned when present, otherwise the whole match; an empty string
// means the pattern did not match or failed to compile (both non-fatal).
func extractVersion(output, pattern string) string {
	output = strings.TrimSpace(output)
	if pattern == "" {
		return output
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		// A pattern that fails to compile is a catalog authoring error, treated
		// here as a non-match so version resolution stays non-fatal. The library
		// has no logger; pattern validity is enforced by the catalog module's
		// own validation at publish time, not at runtime.
		return ""
	}
	m := re.FindStringSubmatch(output)
	if m == nil {
		return ""
	}
	if len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return strings.TrimSpace(m[0])
}
