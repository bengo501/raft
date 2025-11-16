import argparse
import csv
import glob
import json
import os

import matplotlib.pyplot as plt


def collect_points(folder):
    pattern = os.path.join(folder, "*.json")
    files = glob.glob(pattern)
    if not files:
        raise FileNotFoundError(f"nenhum arquivo json encontrado em {folder}")
    rows = []
    for path in sorted(files):
        with open(path, "r", encoding="utf-8") as fh:
            data = json.load(fh)
        rows.append(
            {
                "arquivo": os.path.basename(path),
                "clients": data.get("client_count", 0),
                "throughput": data.get("throughput_ops", 0.0),
                "latencia_ms": data.get("avg_latency_ms", 0.0),
            }
        )
    return rows


def plot(rows, output):
    plt.figure(figsize=(8, 6))
    xs = [row["throughput"] for row in rows]
    ys = [row["latencia_ms"] for row in rows]
    plt.scatter(xs, ys, c="tab:blue")
    for row in rows:
        label = f"{row['clients']} clientes"
        plt.annotate(label, (row["throughput"], row["latencia_ms"]), textcoords="offset points", xytext=(5, 5))
    plt.xlabel("Vazão (ops/s)")
    plt.ylabel("Latência média (ms)")
    plt.title("Vazão x Latência - Execuções do cluster Raft")
    plt.grid(True, linestyle="--", alpha=0.5)
    plt.tight_layout()
    plt.savefig(output, dpi=200)
    plt.close()


def write_csv(rows, path):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", newline="", encoding="utf-8") as fh:
        writer = csv.DictWriter(fh, fieldnames=["arquivo", "clients", "throughput", "latencia_ms"])
        writer.writeheader()
        writer.writerows(rows)


def main():
    parser = argparse.ArgumentParser(description="gera gráfico vazão x latência a partir dos resultados do loadgen")
    parser.add_argument("--folder", required=True, help="pasta contendo arquivos .json produzidos pelo loadgen")
    parser.add_argument("--output", required=True, help="caminho do arquivo de imagem (png)")
    parser.add_argument("--csv", default="resultados/pontos.csv", help="csv auxiliar com os pontos plotados")
    args = parser.parse_args()

    rows = collect_points(args.folder)
    output_dir = os.path.dirname(args.output)
    if output_dir:
        os.makedirs(output_dir, exist_ok=True)
    plot(rows, args.output)
    write_csv(rows, args.csv)
    print(f"gráfico gerado em {args.output}")
    print(f"pontos exportados em {args.csv}")


if __name__ == "__main__":
    main()

