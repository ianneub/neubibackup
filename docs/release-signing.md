# Release signing

NeubiBackup releases are signed with a long-lived **self-signed**
code-signing cert (not Apple Developer ID). Every release uses the same
cert so the macOS Keychain ACL on user-stored repository passwords stays
valid across auto-updates.

This document is for maintainers. If you only build the project locally,
your `go build` / `scripts/build-dev-app.sh` workflow continues to use
ad-hoc signing — nothing here applies.

## What signing buys us

- macOS releases have a stable [Designated Requirement][dr]
  (`identifier "com.neubibackup.app" and certificate root = H"<sha1>"`).
- The `use_keychain` feature (see `README.md`) creates Keychain entries
  whose ACL is bound to that DR. The same cert across releases means
  the ACL keeps matching when users auto-update.
- Lose the cert and every existing user's keychain entry stops being
  silently usable on the next release: they get one prompt, click
  Always Allow, done. But re-using the same cert avoids that prompt
  entirely.

[dr]: https://developer.apple.com/library/archive/documentation/Security/Conceptual/CodeSigningGuide/RequirementLang/RequirementLang.html

## What signing does **not** buy

- Gatekeeper acceptance. macOS still says "from an unidentified
  developer" on first launch — same as today's ad-hoc state. Users
  right-click → Open the first time.
- Notarization (requires Apple Developer ID, which we deliberately do
  not use).

## Cert facts

- **Common Name:** `NeubiBackup Code Signing`
- **Type:** RSA 2048, self-signed root, code-signing EKU
- **Validity:** 100 years from generation (issued 2026-05-09, expires 2126-04-15)
- **SHA-1 fingerprint:** `D607A51412D9A0FDB9301806C5C99E9F26A73AFF`

If you ever rebuild the cert, replace the SHA-1 above and warn users:
the next release will trigger a one-time keychain prompt.

## How releases use it

`.github/workflows/release.yml` imports the cert from a temporary
keychain in CI and signs the `.app` bundle with it. See the
`Import code-signing cert` and `Sign app bundle` steps.

## Required GitHub secrets

- `MACOS_CERT_P12_BASE64` — base64-encoded `.p12` (the cert + private
  key bundle).
- `MACOS_CERT_PASSWORD` — the export password set when the `.p12` was
  produced.

Until both secrets are populated, the release workflow will fail at the
"Import code-signing cert" step with a clear error message.

## Cert generation procedure (one-time, by the maintainer)

Run these commands on your Mac (or any machine with `openssl` and
`base64`):

```bash
mkdir -p /tmp/nb-signing && cd /tmp/nb-signing

# 1. Generate a private key.
openssl genrsa -out neubibackup.key 2048

# 2. Generate a self-signed code-signing cert valid for 100 years.
cat > openssl.cnf <<'EOF'
[ req ]
distinguished_name = dn
prompt = no
x509_extensions = v3_codesign

[ dn ]
CN = NeubiBackup Code Signing
O  = NeubiBackup
C  = US

[ v3_codesign ]
basicConstraints = critical, CA:false
keyUsage = critical, digitalSignature
extendedKeyUsage = critical, codeSigning
EOF

openssl req -new -x509 \
  -key neubibackup.key \
  -days 36500 \
  -config openssl.cnf \
  -out neubibackup.crt

# 3. Bundle into a .p12 (set a strong export password; remember it).
openssl pkcs12 -export \
  -inkey neubibackup.key \
  -in neubibackup.crt \
  -out neubibackup.p12 \
  -name "NeubiBackup Code Signing"

# 4. Capture the cert SHA-1 (the value baked into every user's Keychain ACL).
openssl x509 -in neubibackup.crt -noout -fingerprint -sha1 | sed 's/.*=//; s/://g'

# 5. Encode the .p12 for the GitHub secret.
base64 -i neubibackup.p12 > neubibackup.p12.base64
```

Save:

- `neubibackup.p12` and the export password → 1Password / your password manager.
- `neubibackup.p12.base64` contents → GitHub repo secret `MACOS_CERT_P12_BASE64`.
- The export password → GitHub repo secret `MACOS_CERT_PASSWORD`.
- The SHA-1 from step 4 → write it into the **Cert facts** section above
  (replace the `<TO BE FILLED IN AFTER CERT GENERATION>` placeholder)
  and commit the change.
- An offline backup copy of `neubibackup.p12` (encrypted USB or printed
  base64) somewhere durable.

After saving, securely delete the `/tmp/nb-signing/` directory:

```bash
rm -rf /tmp/nb-signing
```

## Backup and rotation

The `.p12` exists in three places:

1. GitHub repo secret (used by CI).
2. Maintainer's password manager.
3. Offline backup (encrypted USB / printed base64).

Losing all three means rotating to a new self-signed cert, which
invalidates every user's existing keychain entry. Don't lose all three.

## Regeneration procedure

If you must rotate: regenerate per the commands above, update both
GitHub secrets, update the SHA-1 in this file, and call out the cert
change in the next release's notes.
