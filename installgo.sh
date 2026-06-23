#!/bin/bash
set -euo pipefail

VERSION="1.25.0"
OS="linux"
ARCH="amd64"
TARBALL="go${VERSION}.${OS}-${ARCH}.tar.gz"
URL="https://go.dev/dl/${TARBALL}"

echo "Downloading Go ${VERSION} for ${OS}/${ARCH}..."
wget -q --show-progress "${URL}"

echo "Extracting to /usr/local/go..."
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf "${TARBALL}"
rm "${TARBALL}"

echo "Updating PATH..."
if ! grep -q '/usr/local/go/bin' ~/.profile 2>/dev/null; then
  echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.profile
fi

export PATH=$PATH:/usr/local/go/bin
echo "Go $(go version) installed. Run 'source ~/.profile' or re-login to make it permanent."
