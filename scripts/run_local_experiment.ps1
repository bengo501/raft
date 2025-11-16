param( # parâmetros para o script 
    [int[]]$Clients = @(1, 2, 4, 8), # número de clientes para cada execução
    [int]$DurationSeconds = 180,
    [int]$PayloadBytes = 64, # tamanho do payload em bytes
    [string]$ResultsDir = "resultados", # diretório para salvar os resultados
    [string]$GoBin = "go" # binário do Go para executar o raftnode
)

$repoRoot = (Resolve-Path -Path (Join-Path $PSScriptRoot "..")).Path #diretório do repositório
$nodeSpecs = @( # especificações dos nós
    @{ Id = 1; Addr = "http://127.0.0.1:9001" }, # nó 1
    @{ Id = 2; Addr = "http://127.0.0.1:9002" }, # nó 2
    @{ Id = 3; Addr = "http://127.0.0.1:9003" } #nó 3
)
$peerList = ($nodeSpecs | ForEach-Object { "$($_.Id)=$($_.Addr)" }) -join "," #lista de peers
$global:NodeProcesses = @() #processos dos nós

function Start-RaftNodes { # função para iniciar os nós
    Write-Host "iniciando réplicas locais..." 
    foreach ($spec in $nodeSpecs) { # para cada nó
        $cmd = "$GoBin run ./cmd/raftnode --id $($spec.Id) --addr $($spec.Addr) --peers `"$peerList`"" # comando para iniciar o raftnode
        $pwshCommand = "& { Set-Location -LiteralPath `"$repoRoot`"; $cmd }"
        $proc = Start-Process ` # processo para iniciar o raftnode 
            -FilePath "powershell.exe" ` # caminho para o powershell
            -ArgumentList @("-NoExit", "-Command", $pwshCommand) ` # argumentos para o powershell   
            -PassThru # passa o processo para a variável global
        $global:NodeProcesses += $proc  # adiciona o processo à lista global
        Start-Sleep -Milliseconds 300 # espera 300 milissegundos
    }
    Write-Host "réplicas iniciadas em três terminais separados."
}

function Stop-RaftNodes {
    foreach ($proc in $global:NodeProcesses) { # para cada processo
        if ($proc -and !$proc.HasExited) { # se o processo existe e não foi encerrado
            Write-Host "encerrando processo $($proc.Id)..."
            Stop-Process -Id $proc.Id -Force # encerra o processo
        }
    }
    $global:NodeProcesses = @()
}

function Run-LoadGen { #função para executar o gerador de carga
    param(
        [int]$Clients,
        [int]$RunIndex
    )
    $targets = ($nodeSpecs | ForEach-Object { $_.Addr }) -join "," # lista de endereços dos nós
    # caminho para o arquivo JSON de resultados (run-01.json, run-02.json, etc.)
    $jsonPath = Join-Path $ResultsDir ("run-{0:00}.json" -f $RunIndex)
                 # caminho para o arquivo CSV de distribuição cumulativa de latências
    $cdfPath = Join-Path $ResultsDir ("run-{0:00}-cdf.csv" -f $RunIndex)
    #       argumentos para o comando do gerador de carga
    $args = @(
        "run", "./cmd/loadgen", # comando para executar o gerador de carga
        "--targets", $targets,
        "--clients", $Clients, # número de clientes
        "--duration", "${DurationSeconds}s", # duração
        "--payload-bytes", $PayloadBytes, # tamanho do payload
        "--out-json", $jsonPath, # caminho para o arquivo JSON de resultados
        "--out-latencies", $cdfPath # caminho para o arquivo CSV de distribuição cumulativa de latências
    )
    Write-Host "executando carga com $Clients clientes por $DurationSeconds segundos..." 
    Push-Location $repoRoot #     push para o diretório de repositório
    try {
        & $GoBin @args
    } finally { #finally para garantir que o diretório de repositório seja retornado mesmo que ocorra um erro
    } finally {
        Pop-Location #retorna para o diretório de repositório
    }
}

try { #         try-finally para garantir que os processos sejam encerrados mesmo que ocorra um erro
    # cria o  o    diretório de resultados se não existir
    New-Item -ItemType Directory -Path $ResultsDir -Force | Out-Null
    #indice da execução
    $runIndex = 1
    #para cada número de clientes
    foreach ($c in $Clients) {
        # início da execução
        Write-Host "==== início da execução $runIndex (clientes = $c) ===="
        # encerra os processos de réplicas
        Stop-RaftNodes
        # inicia os processos de réplicas
        Start-RaftNodes
        Write-Host "aguardando estabilização do cluster..."
        # espera 5 segundos para a estabilização do cluster
        Start-Sleep -Seconds 5
        #executa o gerador de carga
        Run-LoadGen -Clients $c -RunIndex $runIndex
        #encerra os processos de réplicas
        Stop-RaftNodes
        #   fim da    execução
        Write-Host "==== execução $runIndex finalizada ===="
        $runIndex++
    }
    Write-Host "todas as execuções foram concluídas. arquivos salvos em $ResultsDir."
    Write-Host "gere o gráfico com: python experiments/plot.py --folder $ResultsDir --output $ResultsDir/vazao_vs_latencia.png"
}
finally {
    Stop-RaftNodes # encerra os processos de réplicas
}

