param(
    [int[]]$Clients = @(1, 2, 4, 8),
    [int]$DurationSeconds = 180,
    [int]$PayloadBytes = 64,
    [string]$ResultsDir = "resultados",
    [string]$GoBin = "go"
)

$repoRoot = (Resolve-Path -Path (Join-Path $PSScriptRoot "..")).Path
$nodeSpecs = @(
    @{ Id = 1; Addr = "http://127.0.0.1:9001" },
    @{ Id = 2; Addr = "http://127.0.0.1:9002" },
    @{ Id = 3; Addr = "http://127.0.0.1:9003" }
)
$peerList = ($nodeSpecs | ForEach-Object { "$($_.Id)=$($_.Addr)" }) -join ","
$global:NodeProcesses = @()

function Start-RaftNodes {
    Write-Host "iniciando réplicas locais..."
    foreach ($spec in $nodeSpecs) {
        $cmd = "$GoBin run ./cmd/raftnode --id $($spec.Id) --addr $($spec.Addr) --peers `"$peerList`""
        $pwshCommand = "& { Set-Location -LiteralPath `"$repoRoot`"; $cmd }"
        $proc = Start-Process `
            -FilePath "powershell.exe" `
            -ArgumentList @("-NoExit", "-Command", $pwshCommand) `
            -PassThru
        $global:NodeProcesses += $proc
        Start-Sleep -Milliseconds 300
    }
    Write-Host "réplicas iniciadas em três terminais separados."
}

function Stop-RaftNodes {
    foreach ($proc in $global:NodeProcesses) {
        if ($proc -and !$proc.HasExited) {
            Write-Host "encerrando processo $($proc.Id)..."
            Stop-Process -Id $proc.Id -Force
        }
    }
    $global:NodeProcesses = @()
}

function Run-LoadGen {
    param(
        [int]$Clients,
        [int]$RunIndex
    )
    $targets = ($nodeSpecs | ForEach-Object { $_.Addr }) -join ","
    $jsonPath = Join-Path $ResultsDir ("run-{0:00}.json" -f $RunIndex)
    $cdfPath = Join-Path $ResultsDir ("run-{0:00}-cdf.csv" -f $RunIndex)
    $args = @(
        "run", "./cmd/loadgen",
        "--targets", $targets,
        "--clients", $Clients,
        "--duration", "${DurationSeconds}s",
        "--payload-bytes", $PayloadBytes,
        "--out-json", $jsonPath,
        "--out-latencies", $cdfPath
    )
    Write-Host "executando carga com $Clients clientes por $DurationSeconds segundos..."
    Push-Location $repoRoot
    try {
        & $GoBin @args
    } finally {
        Pop-Location
    }
}

try {
    New-Item -ItemType Directory -Path $ResultsDir -Force | Out-Null
    $runIndex = 1
    foreach ($c in $Clients) {
        Write-Host "==== início da execução $runIndex (clientes = $c) ===="
        Stop-RaftNodes
        Start-RaftNodes
        Write-Host "aguardando estabilização do cluster..."
        Start-Sleep -Seconds 5
        Run-LoadGen -Clients $c -RunIndex $runIndex
        Stop-RaftNodes
        Write-Host "==== execução $runIndex finalizada ===="
        $runIndex++
    }
    Write-Host "todas as execuções foram concluídas. arquivos salvos em $ResultsDir."
    Write-Host "gere o gráfico com: python experiments/plot.py --folder $ResultsDir --output $ResultsDir/vazao_vs_latencia.png"
}
finally {
    Stop-RaftNodes
}

