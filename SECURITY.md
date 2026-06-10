# Security Policy

## Supported versions

Only the latest release is supported. The CLI ships `positronick self update`;
please update before reporting.

## Reporting a vulnerability

Report vulnerabilities privately via
[GitHub private vulnerability reporting](https://github.com/Positronick/cli/security/advisories/new) —
please do not open a public issue. You should get an acknowledgment within
7 days.

## Verifying releases

Every release ships `checksums.txt` (SHA-256) and sigstore-backed build
provenance:

```sh
gh attestation verify positronick_<os>_<arch>.tar.gz --repo Positronick/cli
# or, with the archive and checksums.txt in the same directory:
sha256sum -c --ignore-missing checksums.txt
```

The install script (`curl -fsSL https://positronick.com/install.sh | sh`)
verifies checksums automatically.
