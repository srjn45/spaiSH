class Spai < Formula
  desc "Claude-Code-style AI agent for your terminal"
  homepage "https://github.com/srjn45/spaiSH"
  version "0.1.0"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/srjn45/spaiSH/releases/download/v0.1.0/spai_v0.1.0_darwin_arm64.tar.gz"
      sha256 "f4dce00a15c1bc3c92955dc4738a91be7a4df927b8722b58d9f1c6a1bfcc9e02"
    end

    on_intel do
      url "https://github.com/srjn45/spaiSH/releases/download/v0.1.0/spai_v0.1.0_darwin_amd64.tar.gz"
      sha256 "becab77cb48682a3337f90068014251611f96e4f95f7a32f4f389f290c2c43a2"
    end
  end

  def install
    bin.install "spai"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/spai --version")
  end
end
