#!/bin/sh
# One-time setup: a local self-signed code-signing certificate named "CCS Local Signing".
# build.sh signs with it when present. A stable identity keeps macOS from asking again for
# Documents access after every rebuild (an ad-hoc signature changes on each build).
# Run it yourself; it adds a certificate and key to your login keychain and asks for your password.
set -eu
NAME="CCS Local Signing"
KC="$HOME/Library/Keychains/login.keychain-db"
if security find-identity -p codesigning "$KC" | grep -q "$NAME"; then
  echo "$NAME already exists"; exit 0
fi
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
cd "$T"
openssl req -x509 -newkey rsa:2048 -nodes -keyout key.pem -out cert.pem -days 3650 -subj "/CN=$NAME" \
  -addext "keyUsage=critical,digitalSignature" -addext "extendedKeyUsage=critical,codeSigning" 2>/dev/null
openssl pkcs12 -export -inkey key.pem -in cert.pem -out ccs.p12 -passout pass:ccs
security import ccs.p12 -k "$KC" -P ccs -T /usr/bin/codesign
# trust it for code signing (macOS asks for your password)
security add-trusted-cert -r trustRoot -p codeSign -k "$KC" cert.pem
security find-identity -p codesigning "$KC" | grep "$NAME" || { echo "identity not found"; exit 1; }
echo "done. Rebuild with app/build.sh; grant Documents access once more and it will stick."
