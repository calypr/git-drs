## Operation Guide

```markdown
# Git-DRS macOS Code Signing - Operation Guide

## Quick Start

### 1. Initial Setup (One-time)

#### Step 1: Obtain Apple Developer Certificate
1. Log in to [Apple Developer Portal](https://developer.apple.com)
2. Navigate to Certificates, Identifiers & Profiles → Certificates
3. Create a new "Developer ID Application" certificate
4. Download as `.p12` file (includes private key)

#### Step 2: Encode Certificate
```bash
# macOS
base64 -i ~/Downloads/certificate.p12 | pbcopy

# Linux
base64 ~/Downloads/certificate.p12 | xclip -selection clipboard

# Windows PowerShell
[Convert]::ToBase64String([IO.File]::ReadAllBytes("$env:UserProfile\Downloads\certificate.p12")) | Set-Clipboard
```

#### Step 3: Configure GitHub Secrets
1. Go to calypr/git-drs → Settings → Secrets and variables → Actions
2. Click "New repository secret"
3. Add each secret:

| Name | Value |
|------|-------|
| `APPLE_CERT_DATA` | Paste base64-encoded certificate (from Step 2) |
| `APPLE_CERT_PASSWORD` | Password used to protect the .p12 file |
| `APPLE_TEAM_ID` | Your Apple Developer Team ID (e.g., ABC123XYZ4) |

### 2. Creating a Release

#### Trigger Release Workflow
```bash
# Create and push a version tag
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

#### Monitor Release
1. Go to calypr/git-drs → Actions
2. Click on the "Release" workflow run
3. Watch the "goreleaser" job for signing progress

#### Verify Signed Binary
```bash
# Download the darwin binary from the release
# Then verify the code signature
codesign -v git-drs

# Check signature details
codesign -dv git-drs

# Verify hardened runtime
codesign -dv --entitlements - git-drs
```

### 3. Troubleshooting

#### Issue: Release fails with "code signing failed"

**Cause:** Incorrect certificate password or Team ID
**Solution:**
1. Verify `APPLE_TEAM_ID` matches your Developer ID Application certificate
2. Verify `APPLE_CERT_PASSWORD` is correct (no extra spaces)
3. Test certificate locally: `openssl pkcs12 -in certificate.p12 -passin pass:PASSWORD -noout`

#### Issue: macOS rejects binary with "developer cannot be verified"

**Cause:** Binary not properly signed or notarization missing
**Solution:**
1. Verify signature: `codesign -v git-drs`
2. Check if hardened runtime is applied: `codesign -dv --entitlements - git-drs`
3. If notarization is needed, see "Advanced: Notarization" below

#### Issue: "base64: illegal option" error

**Cause:** Using wrong base64 command (BSD vs GNU)
**Solution:**
- macOS: Use `base64 -i` (correct)
- Linux: Use `base64` alone (correct)
- If errors persist, install GNU coreutils: `brew install coreutils`

### 4. Advanced: Notarization (Optional)

Notarization hardening is recommended for distribution. To add notarization:

1. Get Apple Notary credentials from [App Store Connect](https://appstoreconnect.apple.com)
2. Add secrets:
   - `APPLE_NOTARY_USER`: App Store Connect email
   - `APPLE_NOTARY_PASSWORD`: App-specific password from Apple ID
3. Update `.goreleaser.yaml` `notarize` section with credentials

### 5. Certificate Renewal

Apple Developer ID certificates expire annually. Before expiration:

1. Generate new certificate (see Step 1)
2. Encode and update `APPLE_CERT_DATA` secret
3. Update `APPLE_CERT_PASSWORD` if changed
4. Test with a pre-release tag

### 6. Revoking/Changing Credentials

If credentials are compromised:

1. Revoke certificate in [Apple Developer Portal](https://developer.apple.com)
2. Delete the GitHub Secrets
3. Generate new certificate (see Step 1)
4. Re-configure secrets (see Step 3)

## Reference Files

- **Configuration**: `.goreleaser.yaml`
- **Workflow**: `.github/workflows/release.yaml`
- **Reference Project**: [anvilproject/drs_downloader](https://github.com/anvilproject/drs_downloader)

## Key Concepts

| Term | Definition |
|------|-----------|
| **Code Signing** | Cryptographically signing the binary to verify its authenticity |
| **Developer ID** | Apple's identity for non-App Store distributed applications |
| **Hardened Runtime** | macOS security feature preventing certain types of attacks |
| **Notarization** | Apple's service that scans binaries for malware |
| **Team ID** | Unique identifier for your Apple Developer account |

## Security Best Practices

1. ✅ Store certificates in .p12 format with strong password
2. ✅ Use GitHub Secrets (not hardcoded) for sensitive data
3. ✅ Rotate certificates annually
4. ✅ Verify signatures before distribution
5. ✅ Monitor certificate expiration dates
6. ❌ Never commit certificates to git
7. ❌ Never share certificate passwords
8. ❌ Never post certificate data publicly

## Support & References

- [Apple Code Signing Guide](https://developer.apple.com/documentation/security/code_signing_services)
- [GoReleaser Code Signing Docs](https://goreleaser.com/customization/sign/)
- [macOS Gatekeeper](https://support.apple.com/en-us/HT202491)
- [drs_downloader Implementation](https://github.com/anvilproject/drs_downloader/blob/main/.github/workflows/build.yml)
```
