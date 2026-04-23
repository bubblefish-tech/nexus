#!/bin/bash
set -euo pipefail

MODEL_DIR="$(dirname "$0")/../models"
mkdir -p "$MODEL_DIR"

MODEL_FILE="$MODEL_DIR/nomic-embed-text-v1.5.Q4_K_S.gguf"
if [ ! -f "$MODEL_FILE" ]; then
    echo "Downloading nomic-embed-text-v1.5 Q4_K_S (78MB)..."
    curl -L -o "$MODEL_FILE" \
        "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.Q4_K_S.gguf"
fi

# llama-server binary — detect platform
LLAMA_VERSION="b8907"
case "$(uname -s)-$(uname -m)" in
    Linux-x86_64)  LLAMA_ARCHIVE="llama-${LLAMA_VERSION}-bin-ubuntu-x64.tar.gz" ;;
    Linux-aarch64) LLAMA_ARCHIVE="llama-${LLAMA_VERSION}-bin-ubuntu-arm64.tar.gz" ;;
    Darwin-arm64)  LLAMA_ARCHIVE="llama-${LLAMA_VERSION}-bin-macos-arm64.tar.gz" ;;
    Darwin-x86_64) LLAMA_ARCHIVE="llama-${LLAMA_VERSION}-bin-macos-x64.tar.gz" ;;
    *) echo "Unsupported platform: $(uname -s)-$(uname -m)"; exit 1 ;;
esac

LLAMA_EXE="$MODEL_DIR/llama-server"
if [ ! -f "$LLAMA_EXE" ]; then
    echo "Downloading llama-server ${LLAMA_VERSION}..."
    curl -L -o "$MODEL_DIR/llama-server.tar.gz" \
        "https://github.com/ggml-org/llama.cpp/releases/download/${LLAMA_VERSION}/${LLAMA_ARCHIVE}"
    mkdir -p "$MODEL_DIR/llama-tmp"
    tar -xzf "$MODEL_DIR/llama-server.tar.gz" -C "$MODEL_DIR/llama-tmp"
    find "$MODEL_DIR/llama-tmp" -name "llama-server" -type f -exec mv {} "$LLAMA_EXE" \;
    chmod +x "$LLAMA_EXE"
    rm -rf "$MODEL_DIR/llama-tmp" "$MODEL_DIR/llama-server.tar.gz"
fi

echo "Ready: $MODEL_FILE ($( du -h "$MODEL_FILE" | cut -f1 ))"
echo "Ready: $LLAMA_EXE"
