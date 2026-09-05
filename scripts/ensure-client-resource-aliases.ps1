[CmdletBinding()]
param(
    [Parameter(Mandatory)] [string] $ClientRoot
)

$ErrorActionPreference = 'Stop'
$clientRootPath = [IO.Path]::GetFullPath($ClientRoot)
if (-not (Test-Path -LiteralPath $clientRootPath -PathType Container)) {
    throw "Client root is missing: $clientRootPath"
}

# QQTSection v110 initializes CPropCombineForge with the runtime cache name
# config/combineforge.xml, while the complete 5.2 client package contains the
# same encrypted resource under config/combineforge.ini. FileLen.ini supplies
# the expanded length and QQTSection performs the decode; this alias therefore
# preserves the canonical bytes instead of generating or patching client code.
$aliases = @(
    [pscustomobject]@{
        Source = 'config\combineforge.ini'
        Target = 'config\combineforge.xml'
    },
    # The categorized shop resolves the resource ID from itemCFG, but both
    # ordinary backpack views format item%d.img with the actual inventory ID.
    # Materialize the canonical item-99 bytes under those required names;
    # this is a filesystem alias, not another authored DIMG resource.
    [pscustomobject]@{
        Source = 'res\uiRes\icon\item\item99.img'
        Target = 'res\uiRes\icon\item\item30098.img'
    },
    [pscustomobject]@{
        Source = 'res\uiRes\icon\item\item99.img'
        Target = 'res\uiRes\icon\item\item30099.img'
    }
)

foreach ($alias in $aliases) {
    $source = Join-Path $clientRootPath $alias.Source
    $target = Join-Path $clientRootPath $alias.Target
    if (-not (Test-Path -LiteralPath $source -PathType Leaf)) {
        throw "Canonical client resource is missing: $source"
    }
    $sourceHash = (Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash
    $targetCurrent = Test-Path -LiteralPath $target -PathType Leaf
    if ($targetCurrent) {
        $targetCurrent = (Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash -eq $sourceHash
    }
    if (-not $targetCurrent) {
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $target) | Out-Null
        Copy-Item -LiteralPath $source -Destination $target -Force
    }
    $targetHash = (Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash
    if ($targetHash -ne $sourceHash) {
        throw "Client resource alias verification failed: $target"
    }
}
