# comandos para reproduzir o estudo de caso

este arquivo documenta os comandos executados para gerar todos os resultados do estudo de caso raft.

## 1 - execução dos experimentos

cada experimento foi executado manualmente abrindo 4 terminais:

### execução 1: carga referência (30s, 32 bytes)

**terminais 1, 2, 3 (réplicas):**
```bash
go run ./cmd/raftnode --id 1 --addr http://127.0.0.1:9001 --peers "1=http://127.0.0.1:9001,2=http://127.0.0.1:9002,3=http://127.0.0.1:9003"
go run ./cmd/raftnode --id 2 --addr http://127.0.0.1:9002 --peers "1=http://127.0.0.1:9001,2=http://127.0.0.1:9002,3=http://127.0.0.1:9003"
go run ./cmd/raftnode --id 3 --addr http://127.0.0.1:9003 --peers "1=http://127.0.0.1:9001,2=http://127.0.0.1:9002,3=http://127.0.0.1:9003"
```

**terminal 4 (gerador de carga):**
```bash
go run ./cmd/loadgen --targets "http://127.0.0.1:9001,http://127.0.0.1:9002,http://127.0.0.1:9003" --clients 3 --duration 30s --payload-bytes 32 --out-json resultados/run-01.json --out-latencies resultados/run-01-cdf.csv
```

### execução 2: carga leve (60s, 32 bytes)

**terminais 1, 2, 3:** mesmos comandos das réplicas acima

**terminal 4:**
```bash
go run ./cmd/loadgen --targets "http://127.0.0.1:9001,http://127.0.0.1:9002,http://127.0.0.1:9003" --clients 3 --duration 60s --payload-bytes 32 --out-json resultados-lev/run-01.json --out-latencies resultados-lev/run-01-cdf.csv
```

### execução 3: carga média (120s, 64 bytes)

**terminais 1, 2, 3:** mesmos comandos das réplicas acima

**terminal 4:**
```bash
go run ./cmd/loadgen --targets "http://127.0.0.1:9001,http://127.0.0.1:9002,http://127.0.0.1:9003" --clients 3 --duration 120s --payload-bytes 64 --out-json resultados-med/run-01.json --out-latencies resultados-med/run-01-cdf.csv
```

### execução 4: carga pesada (180s, 128 bytes)

**terminais 1, 2, 3:** mesmos comandos das réplicas acima

**terminal 4:**
```bash
go run ./cmd/loadgen --targets "http://127.0.0.1:9001,http://127.0.0.1:9002,http://127.0.0.1:9003" --clients 3 --duration 180s --payload-bytes 128 --out-json resultados-pes/run-01.json --out-latencies resultados-pes/run-01-cdf.csv
```

## 2 - consolidação dos resultados

após executar todos os experimentos, consolidamos os arquivos json:

```bash
# criar pasta consolidada
mkdir resultados-all

# copiar todos os json com nomes descritivos
Copy-Item "resultados\run-01.json" "resultados-all\resultados.json"
Copy-Item "resultados-lev\run-01.json" "resultados-all\resultados-lev.json"
Copy-Item "resultados-med\run-01.json" "resultados-all\resultados-med.json"
Copy-Item "resultados-pes\run-01.json" "resultados-all\resultados-pes.json"
```

## 3 - geração do gráfico e tabela

```bash
python experiments/plot.py --folder resultados-all --output relatorio/vazao_vs_latencia.png --csv resultados-all/pontos.csv
```

este comando:
- lê todos os arquivos `.json` da pasta `resultados-all/`
- gera o gráfico vazão × latência em `relatorio/vazao_vs_latencia.png`
- exporta a tabela de pontos em `resultados-all/pontos.csv`

## 4 - estrutura final de arquivos

```
raft/
├── resultados/
│   ├── run-01.json
│   └── run-01-cdf.csv
├── resultados-lev/
│   ├── run-01.json
│   └── run-01-cdf.csv
├── resultados-med/
│   ├── run-01.json
│   └── run-01-cdf.csv
├── resultados-pes/
│   ├── run-01.json
│   └── run-01-cdf.csv
├── resultados-all/
│   ├── resultados.json
│   ├── resultados-lev.json
│   ├── resultados-med.json
│   ├── resultados-pes.json
│   └── pontos.csv
└── relatorio/
    ├── relatorio.md
    ├── vazao_vs_latencia.png
    └── COMANDOS.md (este arquivo)
```

## 5 - requisitos

- go 1.21+
- python 3.8+
- matplotlib (`pip install matplotlib`)

