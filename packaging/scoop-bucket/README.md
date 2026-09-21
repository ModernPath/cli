# ModernPath Scoop bucket

Scoop manifest for installing `modernpath` on Windows.

## User install

```powershell
scoop bucket add modernpath https://github.com/ModernPath/scoop-bucket
scoop install modernpath
modernpath --version
```

## Maintainer: update on CLI release

When `ModernPath/cli` publishes a new tag, the release workflow triggers `update-formula.yml` in this repo with the new version and Windows zip SHA256.

Manual update:

```bash
gh workflow run update-formula.yml -R ModernPath/scoop-bucket \
  -f version=0.1.2 \
  -f windows_sha256=<sha256 from checksums.txt>
```
