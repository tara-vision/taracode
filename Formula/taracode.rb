class Taracode < Formula
  desc "AI-powered CLI assistant for software development (Claude Code-like tool)"
  homepage "https://github.com/tara-vision/taracode"
  url "https://github.com/tara-vision/taracode/archive/refs/tags/v0.1.2.tar.gz"
  sha256 "8d1ea9c9c4d2b51d64e64438ad4529ac7f67c9283f7bae570431aa2c35a33e14"
  license "Apache-2.0"

  depends_on "go" => :build

  def install
    tag_version = "v#{version}"
    pkg = "github.com/tara-vision/taracode/cmd"

    ldflags = [
      "-s -w",
      "-X #{pkg}.Version=#{tag_version}"
    ].join(" ")

    system "go", "build", *std_go_args(ldflags: ldflags)
  end

  def caveats
    <<~EOS
      To use taracode, you need to set your vLLM server host:
        export TARACODE_HOST=http://your-vllm-server:8000
      ...
    EOS
  end

  test do
    shell_output("#{bin}/taracode --version")
  end
end
