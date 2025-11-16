import csv
import matplotlib.pyplot as plt

# ler os dados
throughput = []
latencia = []
labels = []

with open('../resultados-all/pontos.csv', 'r') as f:
    reader = csv.DictReader(f)
    for row in reader:
        throughput.append(float(row['throughput']))
        latencia.append(float(row['latencia_ms']))
        
        # extrair nome simplificado (remover resultados- e .json)
        label = row['arquivo'].replace('resultados-', '').replace('.json', '')
        if label == 'resultados':
            label = 'base'
        labels.append(label)

# criar o gráfico
plt.figure(figsize=(10, 6))
plt.scatter(throughput, latencia, s=100, alpha=0.6, color='blue')

# adicionar rótulos para cada ponto
for i in range(len(throughput)):
    plt.annotate(labels[i], 
                xy=(throughput[i], latencia[i]),
                xytext=(5, 5),
                textcoords='offset points',
                fontsize=10,
                fontweight='bold',
                bbox=dict(boxstyle='round,pad=0.3', facecolor='yellow', alpha=0.7))

plt.xlabel('vazão (ops/s)', fontsize=12)
plt.ylabel('latência média (ms)', fontsize=12)
plt.title('vazão vs latência - experimentos raft', fontsize=14, fontweight='bold')
plt.grid(True, alpha=0.3)

# adicionar margens para melhor visualização
plt.margins(0.15)

# salvar o gráfico
plt.tight_layout()
plt.savefig('../resultados-all/vazao_vs_latencia_identificado.png', dpi=300, bbox_inches='tight')
print('gráfico salvo em: resultados-all/vazao_vs_latencia_identificado.png')

plt.show()

