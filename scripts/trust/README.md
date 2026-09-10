# Build input trust

`microfat-v0.2.2.json` is the repository-controlled trust anchor for six release
archives and their extracted executables. Normal builds verify this data without
fetching checksums or requiring Cosign. A cache is never its own trust anchor.

The initial manifest was derived from the v0.2.2 checksum file after Cosign
verification with these exact constraints:

- Identity: `https://github.com/EpicBlackWolfZ/microfat/.github/workflows/release.yml@refs/tags/v0.2.2`
- OIDC issuer: `https://token.actions.githubusercontent.com`
- Checksum file SHA-256: `a82c4827b0386a702a51506f1036972d3763cf15c37eaddf1f4b27683ccf754f`
- Signature bundle SHA-256: `b2ef4f9d17aabcd4b0876eb957eadbe7bd37fd37b5aec97124c8cea26e9ddc2e`

## Updating microfat

1. Choose an upstream release and inspect its source changes and release workflow.
2. Obtain `checksums.txt` and `checksums.txt.sig`. Verify the signature using a
   trusted Cosign installation, the exact release workflow/tag identity, and the
   GitHub OIDC issuer above. A missing or invalid signature stops the update.
3. Download both host CLI archives and the four full/minimal launcher archives.
   Verify each archive against the authenticated checksums **before opening it**.
4. Inspect the expected regular executable member without executing it. Record
   the archive name/digest/size and member name/digest/size in a new manifest.
   Review all six rows; do not copy hashes from an unverified cache.
5. Update the default version intentionally, run cold/warm release rehearsals
   and script contracts, and submit the manifest and provenance change for review.

Do not add environment switches that replace trust metadata or download origins.
`MICROFAT_VERSION` selects only an already committed complete manifest. Build
machines trust the reviewed repository, their Python/system toolchain, and their
current user; same-user hostile process isolation is outside this contract.

The ShellCheck bootstrap separately pins its v0.11.0 archive digest in the
installer. Action SHAs and explicit installer inputs are reviewed together.
Go-installed scanners use exact module versions and the Go checksum database.
