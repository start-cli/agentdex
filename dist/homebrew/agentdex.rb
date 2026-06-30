# Homebrew formula for agentdex, destined for the org tap start-cli/homebrew-tap.
#
# Authored in project 04 with placeholder release fields. Project 05 (gated) fills
# the real `url`, `sha256`, and `:revision` for the tagged release and publishes it
# to the tap. Do not fill those fields here; this copy is the source of truth that
# project 05 copies into the tap repository.
class Agentdex < Formula
  desc "Detect AI coding agents installed on the local machine"
  homepage "https://github.com/start-cli/agentdex"
  license "MPL-2.0"
  head "https://github.com/start-cli/agentdex.git", branch: "main"

  # Release fields are placeholders until project 05 cuts the tagged release.
  url "https://github.com/start-cli/agentdex/archive/refs/tags/vREPLACE_ME.tar.gz"
  version "REPLACE_ME"
  sha256 "REPLACE_ME"

  depends_on "go" => :build

  def install
    # Commit is a release field: project 05 replaces REPLACE_ME with the tagged
    # commit SHA. Version comes from the placeholder `version` above; Date is the
    # build time.
    ldflags = %W[
      -s -w
      -X github.com/start-cli/agentdex/internal/cli.Version=#{version}
      -X github.com/start-cli/agentdex/internal/cli.Commit=REPLACE_ME
      -X github.com/start-cli/agentdex/internal/cli.Date=#{time.iso8601}
    ]
    # CGO_ENABLED=0 keeps the binary pure Go, matching the project's build contract.
    ENV["CGO_ENABLED"] = "0"
    system "go", "build", *std_go_args(ldflags: ldflags), "./cmd/agentdex"
  end

  test do
    assert_match "agentdex", shell_output("#{bin}/agentdex version")
  end
end
