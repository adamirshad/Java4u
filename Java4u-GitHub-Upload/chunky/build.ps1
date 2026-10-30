param(
    [string]$OutputPath = (Join-Path $PSScriptRoot 'build/libs/chunky-loader.jar')
)

$ErrorActionPreference = 'Stop'
$javac = if ($env:JAVA_HOME) { Join-Path $env:JAVA_HOME 'bin/javac.exe' } else { (Get-Command javac.exe -ErrorAction SilentlyContinue).Source }
$jar = if ($env:JAVA_HOME) { Join-Path $env:JAVA_HOME 'bin/jar.exe' } else { (Get-Command jar.exe -ErrorAction SilentlyContinue).Source }
if (-not $javac -or -not (Test-Path -LiteralPath $javac) -or -not $jar -or -not (Test-Path -LiteralPath $jar)) {
    throw 'Chunky requires a JDK 17 or newer. Set JAVA_HOME or add javac.exe and jar.exe to PATH.'
}

$classes = Join-Path $PSScriptRoot ('build/classes-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $classes, (Split-Path -Parent $OutputPath) | Out-Null
$sources = @(Get-ChildItem -LiteralPath (Join-Path $PSScriptRoot 'src/main/java') -Filter '*.java' -Recurse | ForEach-Object FullName)
if ($sources.Count -eq 0) { throw 'No Chunky Java source files were found.' }
& $javac --release 17 -encoding UTF-8 -d $classes @sources
if ($LASTEXITCODE -ne 0) { throw "javac failed with exit code $LASTEXITCODE" }
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'runtime.properties') -Destination $classes
& $jar --create --file $OutputPath --main-class chunky.bootstrap.ChunkyBootstrap -C $classes .
if ($LASTEXITCODE -ne 0) { throw "jar failed with exit code $LASTEXITCODE" }
Write-Output "Built Chunky loader: $OutputPath"
