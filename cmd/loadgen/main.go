package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type clientResult struct {
	LatencySamples []time.Duration
	Requests       int
	Errors         int
}

type cdfPoint struct {
	LatencyMs   float64 `json:"latency_ms"`
	Probability float64 `json:"probability"`
}

type aggregatedMetrics struct {
	TotalRequests int                `json:"total_requests"`
	Throughput    float64            `json:"throughput_ops"`
	AvgLatencyMs  float64            `json:"avg_latency_ms"`
	DurationSec   float64            `json:"duration_sec"`
	ClientStats   []clientSummary    `json:"clients"`
	PercentilesMs map[string]float64 `json:"percentiles_ms"`
	CDF           []cdfPoint         `json:"cdf"`
	ErrorCount    int                `json:"error_count"`
	ClientCount   int                `json:"client_count"`
	PayloadBytes  int                `json:"payload_bytes"`
	DelayMs       float64            `json:"delay_ms"`
	Timestamp     time.Time          `json:"timestamp"`
}

type clientSummary struct {
	ID            int     `json:"id"`
	Requests      int     `json:"requests"`
	ThroughputOps float64 `json:"throughput_ops"`
	AvgLatencyMs  float64 `json:"avg_latency_ms"`
	Errors        int     `json:"errors"`
}

type runConfig struct {
	Targets     []string
	Duration    time.Duration
	ClientCount int
	PayloadSize int
	Delay       time.Duration
	OutputJSON  string
	OutputCSV   string
}

func main() {
	var (
		targetsFlag  = flag.String("targets", "http://127.0.0.1:9001", "lista de endpoints HTTP separados por vírgula")
		clientFlag   = flag.Int("clients", 1, "número de processos cliente")
		durationFlag = flag.Duration("duration", 30*time.Second, "tempo de execução de cada cliente")
		payloadFlag  = flag.Int("payload-bytes", 32, "tamanho do payload aleatório em bytes")
		delayFlag    = flag.Duration("delay", 0, "intervalo opcional entre requisições de um mesmo cliente")
		outJSONFlag  = flag.String("out-json", "", "arquivo para escrever métricas agregadas em JSON")
		outCSVFlag   = flag.String("out-latencies", "", "arquivo CSV para amostras de latência")
	)
	flag.Parse()
	rand.Seed(time.Now().UnixNano())
	cfg := runConfig{
		Targets:     splitAndTrim(*targetsFlag),
		Duration:    *durationFlag,
		ClientCount: *clientFlag,
		PayloadSize: *payloadFlag,
		Delay:       *delayFlag,
		OutputJSON:  *outJSONFlag,
		OutputCSV:   *outCSVFlag,
	}
	if len(cfg.Targets) == 0 {
		log.Fatalf("necessário informar pelo menos um endpoint em --targets")
	}
	metrics := executeLoad(cfg)
	printSummary(metrics)
	if cfg.OutputJSON != "" {
		if err := writeJSON(cfg.OutputJSON, metrics); err != nil {
			log.Printf("erro ao escrever json: %v", err)
		}
	}
	if cfg.OutputCSV != "" {
		if err := writeLatencies(cfg.OutputCSV, metrics); err != nil {
			log.Printf("erro ao escrever csv: %v", err)
		}
	}
}

func executeLoad(cfg runConfig) aggregatedMetrics {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Duration)
	defer cancel()
	results := make([]clientResult, cfg.ClientCount)
	wg := sync.WaitGroup{}
	wg.Add(cfg.ClientCount)
	for i := 0; i < cfg.ClientCount; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx] = runClient(ctx, idx, cfg)
		}(i)
	}
	wg.Wait()
	metrics := aggregate(results, cfg.Duration)
	metrics.ClientCount = cfg.ClientCount
	metrics.PayloadBytes = cfg.PayloadSize
	metrics.DelayMs = float64(cfg.Delay.Microseconds()) / 1000.0
	metrics.Timestamp = time.Now().UTC()
	return metrics
}

func runClient(ctx context.Context, id int, cfg runConfig) clientResult {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}
	targetIdx := id % len(cfg.Targets)
	target := cfg.Targets[targetIdx]
	res := clientResult{
		LatencySamples: make([]time.Duration, 0, 1024),
	}
	payload := make([]byte, cfg.PayloadSize)
	for {
		select {
		case <-ctx.Done():
			return res
		default:
		}
		_, _ = rand.Read(payload)
		begin := time.Now()
		req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/op", strings.TrimRight(target, "/")), bytes.NewReader(payload))
		if err != nil {
			res.Errors++
			continue
		}
		req = req.WithContext(ctx)
		resp, err := client.Do(req)
		if err != nil {
			res.Errors++
			targetIdx = (targetIdx + 1) % len(cfg.Targets)
			target = cfg.Targets[targetIdx]
			continue
		}
		func() {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusConflict {
				if leader := resp.Header.Get("X-Raft-Leader"); leader != "" {
					target = leader
				}
				res.Errors++
				return
			}
			if resp.StatusCode >= 300 {
				res.Errors++
				return
			}
			lat := time.Since(begin)
			res.LatencySamples = append(res.LatencySamples, lat)
			res.Requests++
		}()
		if cfg.Delay > 0 {
			select {
			case <-ctx.Done():
				return res
			case <-time.After(cfg.Delay):
			}
		}
	}
}

func aggregate(results []clientResult, runtime time.Duration) aggregatedMetrics {
	totalReq := 0
	totalErr := 0
	latencies := make([]time.Duration, 0)
	summaries := make([]clientSummary, len(results))
	for i, r := range results {
		totalReq += r.Requests
		totalErr += r.Errors
		latencies = append(latencies, r.LatencySamples...)
		avg := averageLatency(r.LatencySamples)
		summaries[i] = clientSummary{
			ID:            i,
			Requests:      r.Requests,
			ThroughputOps: throughput(r.Requests, runtime),
			AvgLatencyMs:  avg,
			Errors:        r.Errors,
		}
	}
	avgLat := averageLatency(latencies)
	percentiles := computePercentiles(latencies)
	cdf := buildCDF(latencies, 100)
	return aggregatedMetrics{
		TotalRequests: totalReq,
		ErrorCount:    totalErr,
		Throughput:    throughput(totalReq, runtime),
		AvgLatencyMs:  avgLat,
		DurationSec:   runtime.Seconds(),
		ClientStats:   summaries,
		PercentilesMs: percentiles,
		CDF:           cdf,
	}
}

func throughput(reqs int, runtime time.Duration) float64 {
	if runtime <= 0 {
		return 0
	}
	return float64(reqs) / runtime.Seconds()
}

func averageLatency(latencies []time.Duration) float64 {
	if len(latencies) == 0 {
		return 0
	}
	var total time.Duration
	for _, l := range latencies {
		total += l
	}
	return float64(total.Microseconds()) / 1000.0 / float64(len(latencies))
}

func computePercentiles(latencies []time.Duration) map[string]float64 {
	if len(latencies) == 0 {
		return map[string]float64{}
	}
	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	ps := []int{50, 75, 90, 95, 99}
	result := make(map[string]float64, len(ps))
	for _, p := range ps {
		idx := int(float64(p) / 100.0 * float64(len(sorted)-1))
		if idx < 0 {
			idx = 0
		}
		result[fmt.Sprintf("p%d", p)] = float64(sorted[idx].Microseconds()) / 1000.0
	}
	return result
}

func buildCDF(latencies []time.Duration, points int) []cdfPoint {
	if len(latencies) == 0 || points <= 0 {
		return nil
	}
	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	result := make([]cdfPoint, 0, points)
	for i := 1; i <= points; i++ {
		idx := int(float64(i) / float64(points) * float64(len(sorted)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sorted) {
			idx = len(sorted) - 1
		}
		latms := float64(sorted[idx].Microseconds()) / 1000.0
		prob := float64(idx+1) / float64(len(sorted))
		result = append(result, cdfPoint{
			LatencyMs:   latms,
			Probability: prob,
		})
	}
	return result
}

func writeJSON(path string, data aggregatedMetrics) error {
	if err := ensureDir(path); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

func writeLatencies(path string, data aggregatedMetrics) error {
	if err := ensureDir(path); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	w := csv.NewWriter(file)
	defer w.Flush()
	if err := w.Write([]string{"latency_ms", "cdf"}); err != nil {
		return err
	}
	for _, point := range data.CDF {
		if err := w.Write([]string{
			fmt.Sprintf("%.4f", point.LatencyMs),
			fmt.Sprintf("%.4f", point.Probability),
		}); err != nil {
			return err
		}
	}
	return w.Error()
}

func printSummary(metrics aggregatedMetrics) {
	log.Printf("requisições totais: %d", metrics.TotalRequests)
	log.Printf("erros: %d", metrics.ErrorCount)
	log.Printf("vazão média: %.2f ops/s", metrics.Throughput)
	log.Printf("latência média: %.2f ms", metrics.AvgLatencyMs)
	for name, value := range metrics.PercentilesMs {
		log.Printf("%s: %.2f ms", name, value)
	}
}

func splitAndTrim(s string) []string {
	items := strings.Split(s, ",")
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func ensureDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}
