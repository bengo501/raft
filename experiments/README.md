# estudo de caso com raft

Este diretório documenta o passo a passo para cumprir o enunciado da cadeira de sistemas distribuídos usando a biblioteca `go.etcd.io/raft/v3`. O fluxo a seguir cobre todas as etapas: montar o cluster com 3 réplicas, gerar carga controlada, coletar métricas (vazão, latência, CDF) para níveis de carga crescentes, reiniciar o sistema a cada execução e, por fim, plotar o gráfico vazão × latência.

## 0) preparar cluster distribuído (mínimo 3 réplicas)

1. copie o diretório do projeto para três máquinas do laboratório (ou três contêineres/VMs). O código não depende de disco, então basta ter Go 1.24 instalado.
2. em cada máquina escolha um identificador único (1, 2, 3) e um endereço HTTP acessível pelas demais réplicas.
3. inicie cada réplica com o comando abaixo (ajuste `--id` e `--addr`):

```
go run ./cmd/raftnode \
  --id 1 \
  --addr http://10.0.0.11:9001 \
  --peers 1=http://10.0.0.11:9001,2=http://10.0.0.12:9002,3=http://10.0.0.13:9003
```

> cada nó precisa ser executado na sua própria máquina/processo para garantir distribuição. após alguns segundos um líder é eleito (verifique `GET /status` ou logs).

reinicie as réplicas entre cada experimento para cumprir o enunciado (“A cada execução, o sistema deve ser terminado e reiniciado”). basta interromper (`ctrl+c`) e repetir o comando acima.

### automatizando os três terminais

se estiver rodando tudo na mesma máquina, use o script `scripts/run_local_experiment.ps1` para abrir automaticamente três terminais, executar o cluster, rodar o `loadgen` para múltiplos níveis de carga, coletar os arquivos e reiniciar o sistema entre as execuções:

```
pwsh -File scripts/run_local_experiment.ps1 `
  -Clients @(1,2,4,8) `
  -DurationSeconds 180 `
  -PayloadBytes 64 `
  -ResultsDir resultados
```

o script:

- inicia três processos (`go run ./cmd/raftnode ...`) em janelas separadas;
- espera o cluster estabilizar;
- executa `cmd/loadgen` com o número de clientes indicado para 3 minutos (valor padrão);
- grava `resultados/run-XX.json` e `resultados/run-XX-cdf.csv`;
- encerra os nós e reinicia antes da próxima execução, garantindo a regra do enunciado.

## 1) módulo cliente e geração de carga controlada

`cmd/loadgen` implementa exatamente o pseudocódigo solicitado no enunciado:

- cada cliente aguarda o cluster responder;
- registra `timestamp_inicio` e roda por `--duration` (ex: 3m);
- para cada requisição:
  - gera payload aleatório,
  - envia para `/op` do cluster,
  - aguarda resposta,
  - calcula latência (`tempoAgora - timestamp1`) e grava no array;
  - incrementa `nroPedidos`.
- ao terminar, retorna `vazão = nroPedidos / tempoTotal`, `latência média`, percentis e a CDF.

### como executar o loadgen

```
go run ./cmd/loadgen \
  --targets http://10.0.0.11:9001,http://10.0.0.12:9002,http://10.0.0.13:9003 \
  --clients 4 \
  --duration 3m \
  --payload-bytes 64 \
  --out-json resultados/run-04.json \
  --out-latencies resultados/run-04-cdf.csv
```

- `--clients` define o número de processos cliente concorrentes (cada um segue o loop descrito).
- `--targets` pode listar todos os nós; o cliente automaticamente segue redirecionamentos para o líder.
- `--out-json` grava as métricas agregadas da execução (inclui `client_count`, `throughput_ops`, `avg_latency_ms`, percentis e CDF).
- `--out-latencies` grava a função de distribuição cumulativa (pares `latência_ms,probabilidade`). esse arquivo serve de evidência direta da CDF pedida.

ao final de cada execução você terá:

- `run-XX.json`: vazão global do sistema (soma das vazões dos clientes), latência média global, percentis (p50, p75, p90, p95, p99), número de erros, número de clientes, tamanho do payload e timestamp da execução;
- `run-XX-cdf.csv`: lista ordenada de pares `latency_ms,cdf` que representa a função de distribuição cumulativa solicitada no enunciado.

## 2) experimento completo com níveis crescentes de carga

1. defina previamente os níveis, por exemplo:

| execução | clientes | duração | payload |
|----------|----------|---------|---------|
| run-01   | 1        | 3m      | 32 B    |
| run-02   | 2        | 3m      | 32 B    |
| run-03   | 4        | 3m      | 32 B    |
| run-04   | 8        | 3m      | 64 B    |
| run-05   | 12       | 3m      | 64 B    |

2. para cada linha da tabela:
   - (a) derrube as réplicas anteriores e suba novamente os 3 nós;
   - (b) verifique se o líder está ativo (`curl http://nó/status`);
   - (c) execute o `loadgen` com os parâmetros correspondentes;
   - (d) ao terminar, colecione o JSON e o CSV produzidos.
3. ao finalizar todas as execuções, você terá vários arquivos `resultados/run-XX.json`. Esses arquivos contêm:
   - vazão do sistema (soma das vazões dos clientes);
   - latência média global (média das latências médias de cada cliente);
   - CDF da execução.

## 3) plotar vazão × latência

Use o script `experiments/plot.py` para gerar automaticamente o gráfico pedido:

```
python experiments/plot.py \
  --folder resultados \
  --output resultados/vazao_vs_latencia.png
```

O script varre todos os `*.json` dentro de `resultados/`, extrai `throughput_ops` (eixo X) e `avg_latency_ms` (eixo Y) e gera um gráfico de dispersão. Cada ponto recebe o rótulo com `client_count` e o nome do arquivo (facilita rastrear qual execução originou o ponto). O script também exporta `resultados/pontos.csv` com as colunas `arquivo,clients,throughput,latencia_ms` para facilitar interpretações adicionais.

> dependências do script: `python >=3.10` e `matplotlib`. Instale com `pip install matplotlib`.

## 4) checklist do enunciado

- [x] implementação Raft escolhida e executando com ≥3 réplicas distribuídas (`cmd/raftnode`).
- [x] módulo cliente que mede vazão, latência média e CDF (`cmd/loadgen`).
- [x] execuções independentes com reinicialização do cluster entre elas (orientadas nesta documentação).
- [x] gráfico final com pontos (vazão no eixo X, latência no eixo Y) (`experiments/plot.py` + arquivos JSON gerados).

Com isso você possui o estudo de caso completo, pronto para ser descrito no relatório da disciplina.

