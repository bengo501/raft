import csv
import matplotlib.pyplot as plt

# ler os dados
throughput = []
latencia = []
labels = []

with open('../resultados-variacao-clientes/pontos.csv', 'r') as f:
    reader = csv.DictReader(f)
    for row in reader:
        throughput.append(float(row['throughput']))
        latencia.append(float(row['latencia_ms']))
        
        # label mostrando número de clientes
        num_clientes = row['clientes']
        labels.append(f"{num_clientes} cliente{'s' if int(num_clientes) > 1 else ''}")

# criar o gráfico
plt.figure(figsize=(12, 7))
plt.scatter(throughput, latencia, s=150, alpha=0.6, color='blue')

# adicionar rótulos para cada ponto
for i in range(len(throughput)):
    plt.annotate(labels[i], 
                xy=(throughput[i], latencia[i]),
                xytext=(8, 8),
                textcoords='offset points',
                fontsize=11,
                fontweight='bold',
                bbox=dict(boxstyle='round,pad=0.4', facecolor='yellow', alpha=0.7))

plt.xlabel('vazão (ops/s)', fontsize=13)
plt.ylabel('latência média (ms)', fontsize=13)
plt.title('vazão vs latência - variando número de clientes\n(duração: 60s, payload: 32B)', 
          fontsize=14, fontweight='bold')
plt.grid(True, alpha=0.3)

# adicionar margens para melhor visualização
plt.margins(0.15)

# salvar o gráfico
plt.tight_layout()
plt.savefig('../resultados-variacao-clientes/vazao_vs_latencia_clientes.png', dpi=300, bbox_inches='tight')
print('gráfico salvo em: resultados-variacao-clientes/vazao_vs_latencia_clientes.png')

plt.show()


