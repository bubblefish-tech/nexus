# Downloads nomic-embed-text-v1.5 Q4_K_S GGUF and llama-server binary.
# Run once during development. nexus install will handle this for users.

$ModelDir = Join-Path $PSScriptRoot ".." "models"
New-Item -ItemType Directory -Force -Path $ModelDir | Out-Null

$ModelURL = "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.Q4_K_S.gguf"
$ModelFile = Join-Path $ModelDir "nomic-embed-text-v1.5.Q4_K_S.gguf"

if (-not (Test-Path $ModelFile)) {
    Write-Host "Downloading nomic-embed-text-v1.5 Q4_K_S (78MB)..."
    Invoke-WebRequest -Uri $ModelURL -OutFile $ModelFile
    Write-Host "Done: $ModelFile"
} else {
    Write-Host "Model already exists: $ModelFile"
}

# llama-server binary
$LlamaVersion = "b8907"
$LlamaURL = "https://github.com/ggml-org/llama.cpp/releases/download/$LlamaVersion/llama-$LlamaVersion-bin-win-cpu-x64.zip"
$LlamaZip = Join-Path $ModelDir "llama-server.zip"
$LlamaExe = Join-Path $ModelDir "llama-server.exe"

if (-not (Test-Path $LlamaExe)) {
    Write-Host "Downloading llama-server $LlamaVersion..."
    Invoke-WebRequest -Uri $LlamaURL -OutFile $LlamaZip
    Expand-Archive -Path $LlamaZip -DestinationPath $ModelDir -Force
    # The ZIP contains build/bin/llama-server.exe — move it up
    $extracted = Get-ChildItem -Recurse -Path $ModelDir -Filter "llama-server.exe" | Select-Object -First 1
    if ($extracted) {
        Move-Item $extracted.FullName $LlamaExe -Force
    }
    Remove-Item $LlamaZip -Force
    Write-Host "Done: $LlamaExe"
} else {
    Write-Host "llama-server already exists: $LlamaExe"
}
