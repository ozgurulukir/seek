$ErrorActionPreference = "Stop"

$SemanticDir = $PSScriptRoot
$VenvDir = Join-Path $SemanticDir ".venv"
$Python = Join-Path $VenvDir "Scripts\python.exe"

if (-not (Get-Command uv -ErrorAction SilentlyContinue)) {
    throw "uv not found in PATH (https://docs.astral.sh/uv/)"
}

if (Test-Path $Python) {
    Write-Host "==> venv already exists (tools/semantic/.venv) - reusing"
} else {
    Write-Host "==> Creating venv (tools/semantic/.venv)"
    & uv venv $VenvDir --python 3.11
    if ($LASTEXITCODE -ne 0) { throw "uv venv failed" }
}

Write-Host "==> Installing full NLP stack"
& uv pip install --python $Python -r (Join-Path $SemanticDir "requirements.txt")
if ($LASTEXITCODE -ne 0) { throw "dependency installation failed" }

$env:VIRTUAL_ENV = $VenvDir
$env:PATH = "$(Join-Path $VenvDir 'Scripts');$env:PATH"
& $Python (Join-Path $SemanticDir "bootstrap_models.py")
if ($LASTEXITCODE -ne 0) { throw "model bootstrap failed" }

Write-Host ""
Write-Host "Setup complete. Start full mode with:"
Write-Host '  $env:SEMANTIC_WARMUP="1"'
Write-Host '  & tools\semantic\.venv\Scripts\python.exe tools\semantic\server.py'
Write-Host "Verify all capabilities with:"
Write-Host '  $env:REQUIRE_FULL_SEMANTIC="1"'
Write-Host '  & tools\semantic\.venv\Scripts\python.exe tools\semantic\test_server.py'
