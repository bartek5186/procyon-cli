# npm publishing

The public npm package is a dependency-free launcher. Its `postinstall` script
downloads the native Go binary for the current operating system and CPU from
the same-version GitHub Release and validates it against `SHA256SUMS`.

## Regular release

1. Set the same version in `package.json` and
   `internal/buildinfo/version.go`.
2. Commit and push the release changes to `main`.
3. Create and push the matching tag, for example `v0.3.4`.

The `Release` GitHub Actions workflow tests the project, builds Linux, macOS
and Windows binaries for amd64 and arm64, publishes the GitHub Release, and
then publishes `procyon-cli` to npm.

## One-time npm setup

The package name `procyon-cli` was available when npm support was added. The
first package version must be published by an npm account that will own the
package:

```bash
npm login
npm publish --access public
```

Publish only after the matching GitHub Release exists, because npm installation
downloads its binary assets from that release.

After the first publication, open the package settings on npmjs.com and add a
GitHub Actions trusted publisher with:

- organization or user: `bartek5186`
- repository: `procyon-cli`
- workflow filename: `release.yml`
- environment: leave empty

Future tagged releases use GitHub OIDC and do not require an `NPM_TOKEN`
repository secret.
