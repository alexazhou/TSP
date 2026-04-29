#!/bin/bash
set -e

# Change to the project directory
cd "$(dirname "$0")/.."

# Clean previous builds
rm -rf dist build *.egg-info

# Install build tools if not present
python3 -m pip install --upgrade build twine

# Build the package
python3 -m build

# Upload to PyPI
# Note: You will need a PyPI API token
echo "Uploading to PyPI..."
python3 -m twine upload dist/*
