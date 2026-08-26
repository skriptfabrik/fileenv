#!/bin/sh

set -e

# Update mise to the latest version
sudo mise self-update --yes

# Activate mise in bash
cat << 'EOF' >> ~/.bashrc

# Activate mise
eval "$(mise activate bash)"
EOF
