# Homebrew formula for agentdex, destined for the org tap start-cli/homebrew-tap.
#
# This copy is the source of truth. The release fields below (url, version, sha256,
# and the Commit ldflag) are filled for the tagged v0.0.1 release; project 05 copies
# this completed formula into the tap repository.
class Agentdex < Formula
  desc "Detect AI coding agents installed on the local machine"
  homepage "https://github.com/start-cli/agentdex"
  license "MPL-2.0"
  head "https://github.com/start-cli/agentdex.git", branch: "main"

  # Release fields for the tagged v0.0.1 release.
  url "https://github.com/start-cli/agentdex/archive/refs/tags/v0.0.1.tar.gz"
  version "0.0.1"
  sha256 "e0e8d4b1e9fe16ab171c578188a47c1a764c6adbcec006e93ecb579161721f65"

  depends_on "go" => :build

  def install
    # Commit is the tagged release commit SHA. Version comes from the `version`
    # field above; Date is the build time.
    ldflags = %W[
      -s -w
      -X github.com/start-cli/agentdex/internal/cli.Version=#{version}
      -X github.com/start-cli/agentdex/internal/cli.Commit=56aaa3ce8d047d3af17fe275e1c1c3ecc5e4db86
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
