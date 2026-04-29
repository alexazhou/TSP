#!/bin/bash

# gTSP Build Script

set -e

# Project root directory
PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST_DIR="${PROJECT_ROOT}/dist"
BINARY_NAME="gtsp"
SOURCE_FILE="${PROJECT_ROOT}/src/main.go"

echo "🔨 Building ${BINARY_NAME}..."

# Create dist directory if it doesn't exist
mkdir -p "${DIST_DIR}"

# Compile
go build -v -o "${DIST_DIR}/${BINARY_NAME}" "${SOURCE_FILE}"

echo "✅ Build successful!"
echo "📍 Binary located at: ${DIST_DIR}/${BINARY_NAME}"
ls -lh "${DIST_DIR}/${BINARY_NAME}"
