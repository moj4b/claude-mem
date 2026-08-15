### Install

```sh
curl -fsSL https://raw.githubusercontent.com/moj4b/claude-mem/main/install.sh | sh
```

Or take the binary straight out of the archive:

```sh
curl -fsSL https://github.com/moj4b/claude-mem/releases/latest/download/mem_linux_amd64.tar.gz | tar -xz mem
install -m 0755 mem ~/.local/bin/mem
```

Swap `linux_amd64` for `linux_arm64`, `darwin_amd64` or `darwin_arm64`. Each archive holds a single
static `mem` — no runtime, no dependencies, nothing installed alongside it.

Verify a download before you run it:

```sh
sha256sum -c checksums.txt --ignore-missing
```
