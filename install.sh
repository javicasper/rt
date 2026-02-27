#!/bin/sh
set -e
cd "$(dirname "$0")"

go build -o rt .

# Create symlink if not already pointing here
TARGET="/usr/local/bin/rt"
if [ ! -L "$TARGET" ] || [ "$(readlink "$TARGET")" != "$(pwd)/rt" ]; then
    sudo ln -sf "$(pwd)/rt" "$TARGET"
    echo "symlinked $TARGET -> $(pwd)/rt"
fi

echo "$(./rt --version) ready"
