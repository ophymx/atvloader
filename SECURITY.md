# Security

atvloader is a small self-hosted service for a home network. It installs
software on TVs, so bugs in it matter more than its size suggests.

## Reporting

Please report vulnerabilities privately rather than in a public issue: use
GitHub's "Report a vulnerability" button on the Security tab of this
repository, or email abic@ophymx.com. Include the version
(`GET /healthz` returns it), what you found and how to reproduce it. You will
get a reply within a week. Fixes ship as a new release with the issue
credited to you unless you prefer otherwise.

## What is in scope

- Anything that lets a TV or an unauthenticated client read or change the
  store: the device pairing flow, device tokens, API tokens and their scopes.
- Anything that lets an uploaded APK do more than be stored and served:
  the manifest, icon and signature parsers run on untrusted files.
- The TV app accepting a file the store did not publish.

## What the design assumes

- The admin UI and `/api/v1/admin/*` sit behind a reverse proxy that
  authenticates users and sets the identity header. With
  `ATVLOADER_REQUIRE_TRUSTED_HEADER=false` (the default for local
  development) anyone who can reach the port is an admin; the README says
  so, and the Debian package ships with it set to `true`.
- TVs talk to the server over your LAN or through the same proxy over
  TLS. Downloads are checked against the catalog's SHA-256; the catalog
  itself is not signed, so plain HTTP across an untrusted network is not a
  supported deployment.
- Android verifies each APK's own signature at install time. The store
  records signers and refuses a silent key change, but it does not vouch
  for what an APK does.
