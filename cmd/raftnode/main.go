package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"
	"go.etcd.io/raft/v3"
	"go.etcd.io/raft/v3/raftpb"
)

type peerInfo struct {
	ID   uint64
	Addr string
}

type proposal struct {
	ID      string `json:"id"`
	Payload []byte `json:"payload"`
}

type applyResult struct {
	ID    string
	Error error
}

type kvStore struct {
	mu      sync.RWMutex
	entries [][]byte
}

func newKVStore() *kvStore {
	return &kvStore{
		entries: make([][]byte, 0, 1024),
	}
}

func (s *kvStore) apply(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	buf := make([]byte, len(data))
	copy(buf, data)
	s.entries = append(s.entries, buf)
}

func (s *kvStore) count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

type httpTransport struct {
	id       uint64
	client   *http.Client
	peerAddr map[uint64]string
}

func newHTTPTransport(id uint64, peers map[uint64]string) *httpTransport {
	rt := &http.Transport{
		MaxIdleConnsPerHost: 32,
	}
	return &httpTransport{
		id: id,
		client: &http.Client{
			Transport: rt,
			Timeout:   5 * time.Second,
		},
		peerAddr: peers,
	}
}

func (t *httpTransport) send(msgs []raftpb.Message) {
	for _, m := range msgs {
		if m.To == t.id {
			continue
		}
		addr, ok := t.peerAddr[m.To]
		if !ok {
			log.Printf("destino %d desconhecido, descartando mensagem %s", m.To, m.Type.String())
			continue
		}
		data, err := m.Marshal()
		if err != nil {
			log.Printf("falha ao serializar mensagem para %d: %v", m.To, err)
			continue
		}
		url := fmt.Sprintf("%s/raft", strings.TrimRight(addr, "/"))
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
		if err != nil {
			log.Printf("falha ao criar request para %s: %v", url, err)
			continue
		}
		req.Header.Set("Content-Type", "application/protobuf")
		resp, err := t.client.Do(req)
		if err != nil {
			log.Printf("falha ao enviar mensagem para %s: %v", url, err)
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 300 {
			log.Printf("peer %d respondeu %d", m.To, resp.StatusCode)
		}
	}
}

type server struct {
	id         uint64
	listenAddr string
	raftNode   raft.Node
	storage    *raft.MemoryStorage
	stopc      chan struct{}
	proposeC   chan []byte
	transport  *httpTransport
	store      *kvStore
	pendingMu  sync.Mutex
	pending    map[string]chan applyResult
	leaderID   atomic.Uint64
	peerAddr   map[uint64]string
	httpServer *http.Server
}

func newServer(cfg *nodeConfig) (*server, error) {
	if cfg.id == 0 {
		return nil, errors.New("id inválido")
	}
	storage := raft.NewMemoryStorage()
	rcfg := &raft.Config{
		ID:                        cfg.id,
		ElectionTick:              cfg.electionTick,
		HeartbeatTick:             cfg.heartbeatTick,
		Storage:                   storage,
		MaxSizePerMsg:             cfg.maxSizePerMsg,
		MaxInflightMsgs:           cfg.maxInflightMsgs,
		CheckQuorum:               true,
		PreVote:                   true,
		DisableProposalForwarding: false,
	}
	node := raft.StartNode(rcfg, cfg.initialPeers)
	s := &server{
		id:         cfg.id,
		listenAddr: cfg.httpAddr,
		raftNode:   node,
		storage:    storage,
		stopc:      make(chan struct{}),
		proposeC:   make(chan []byte, 1024),
		transport:  newHTTPTransport(cfg.id, cfg.peerAddr),
		store:      newKVStore(),
		pending:    make(map[string]chan applyResult),
		peerAddr:   cfg.peerAddr,
	}
	return s, nil
}

type nodeConfig struct {
	id              uint64
	httpAddr        string
	peerAddr        map[uint64]string
	initialPeers    []raft.Peer
	electionTick    int
	heartbeatTick   int
	maxSizePerMsg   uint64
	maxInflightMsgs int
}

func (s *server) run(ctx context.Context) error {
	go s.startTicker()
	go s.readLoop(ctx)
	go s.forwardProposals(ctx)
	mux := http.NewServeMux()
	mux.HandleFunc("/raft", s.handleRaft)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok")
	})
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/op", s.handleOperation)
	mux.HandleFunc("/metrics", s.handleMetrics)
	httpSrv := &http.Server{
		Addr:    s.listenAddr,
		Handler: mux,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}
	s.httpServer = httpSrv
	log.Printf("nó %d escutando em %s", s.id, s.listenAddr)
	err := httpSrv.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *server) startTicker() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.raftNode.Tick()
		case <-s.stopc:
			return
		}
	}
}

func (s *server) readLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopc:
			return
		case rd := <-s.raftNode.Ready():
			if err := s.storage.Append(rd.Entries); err != nil {
				log.Printf("erro ao persistir entradas: %v", err)
			}
			if !raft.IsEmptyHardState(rd.HardState) {
				if err := s.storage.SetHardState(rd.HardState); err != nil {
					log.Printf("erro ao persistir hardstate: %v", err)
				}
			}
			if !raft.IsEmptySnap(rd.Snapshot) {
				if err := s.storage.ApplySnapshot(rd.Snapshot); err != nil {
					log.Printf("erro ao aplicar snapshot: %v", err)
				}
			}
			if rd.SoftState != nil {
				s.leaderID.Store(rd.SoftState.Lead)
			}
			s.transport.send(rd.Messages)
			for _, entry := range rd.CommittedEntries {
				switch entry.Type {
				case raftpb.EntryConfChange:
					var cc raftpb.ConfChange
					if err := cc.Unmarshal(entry.Data); err != nil {
						log.Printf("falha ao decodificar confchange: %v", err)
						continue
					}
					s.raftNode.ApplyConfChange(cc)
				case raftpb.EntryNormal:
					if len(entry.Data) == 0 {
						continue
					}
					s.handleCommittedEntry(entry.Data)
				}
			}
			s.raftNode.Advance()
		}
	}
}

func (s *server) forwardProposals(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopc:
			return
		case data := <-s.proposeC:
			if err := s.raftNode.Propose(ctx, data); err != nil {
				log.Printf("erro ao propor entrada: %v", err)
			}
		}
	}
}

func (s *server) handleCommittedEntry(data []byte) {
	var p proposal
	if err := json.Unmarshal(data, &p); err != nil {
		log.Printf("entrada inválida: %v", err)
		return
	}
	s.store.apply(p.Payload)
	s.pendingMu.Lock()
	ch, ok := s.pending[p.ID]
	if ok {
		delete(s.pending, p.ID)
	}
	s.pendingMu.Unlock()
	if ok {
		select {
		case ch <- applyResult{ID: p.ID}:
		default:
		}
	}
}

func (s *server) handleRaft(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "falha ao ler corpo", http.StatusBadRequest)
		return
	}
	var msg raftpb.Message
	if err := msg.Unmarshal(body); err != nil {
		http.Error(w, "mensagem inválida", http.StatusBadRequest)
		return
	}
	if err := s.raftNode.Step(r.Context(), msg); err != nil {
		http.Error(w, fmt.Sprintf("erro ao step: %v", err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := s.raftNode.Status()
	s.leaderID.Store(status.Lead)
	resp := map[string]any{
		"id":        s.id,
		"leader_id": status.Lead,
		"term":      status.Term,
		"commit":    status.Commit,
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *server) handleOperation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "método não suportado", http.StatusMethodNotAllowed)
		return
	}
	leader := s.leaderID.Load()
	if leader != 0 && leader != s.id {
		if addr := s.peerAddr[leader]; addr != "" {
			w.Header().Set("X-Raft-Leader", addr)
		}
		http.Error(w, "não sou líder", http.StatusConflict)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "falha ao ler payload", http.StatusBadRequest)
		return
	}
	id := uuid.NewString()
	payload := proposal{ID: id, Payload: body}
	data, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "erro ao serializar proposta", http.StatusInternalServerError)
		return
	}
	respCh := make(chan applyResult, 1)
	s.pendingMu.Lock()
	s.pending[id] = respCh
	s.pendingMu.Unlock()
	select {
	case s.proposeC <- data:
	default:
		go func() {
			s.proposeC <- data
		}()
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	select {
	case res := <-respCh:
		if res.Error != nil {
			http.Error(w, res.Error.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	case <-ctx.Done():
		s.pendingMu.Lock()
		delete(s.pending, id)
		s.pendingMu.Unlock()
		http.Error(w, "timeout aguardando commit", http.StatusGatewayTimeout)
	}
}

func (s *server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	s.pendingMu.Lock()
	pending := len(s.pending)
	s.pendingMu.Unlock()
	resp := map[string]any{
		"id":          s.id,
		"leader_id":   s.leaderID.Load(),
		"entries":     s.store.count(),
		"pending_ops": pending,
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *server) stop(ctx context.Context) error {
	close(s.stopc)
	s.raftNode.Stop()
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

func parsePeers(peers string) (map[uint64]string, []raft.Peer, error) {
	result := make(map[uint64]string)
	peersList := make([]raft.Peer, 0)
	if strings.TrimSpace(peers) == "" {
		return result, peersList, nil
	}
	items := strings.Split(peers, ",")
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			return nil, nil, fmt.Errorf("par inválido %q", item)
		}
		id, err := parseUint(parts[0])
		if err != nil {
			return nil, nil, err
		}
		addr := strings.TrimSpace(parts[1])
		if !strings.Contains(addr, "://") {
			addr = "http://" + addr
		}
		result[id] = addr
		peersList = append(peersList, raft.Peer{ID: id})
	}
	return result, peersList, nil
}

func parseUint(val string) (uint64, error) {
	var id uint64
	_, err := fmt.Sscan(val, &id)
	if err != nil {
		return 0, fmt.Errorf("não consegui ler id %q: %w", val, err)
	}
	return id, nil
}

func normalizeAddr(raw string) (listen string, advertise string, err error) {
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return "", "", err
		}
		return u.Host, raw, nil
	}
	return raw, "http://" + raw, nil
}

func main() {
	var (
		idFlag    = flag.Uint("id", 1, "identificador único da réplica")
		addrFlag  = flag.String("addr", "http://127.0.0.1:9001", "endereço http local (formato http://host:porta ou host:porta)")
		peersFlag = flag.String("peers", "", "lista de peers id=url separados por vírgula")
	)
	flag.Parse()
	listenAddr, advertiseAddr, err := normalizeAddr(*addrFlag)
	if err != nil {
		log.Fatalf("endereço inválido: %v", err)
	}
	peerAddr, peersList, err := parsePeers(*peersFlag)
	if err != nil {
		log.Fatalf("erro ao processar peers: %v", err)
	}
	peerAddr[uint64(*idFlag)] = advertiseAddr
	found := false
	for _, p := range peersList {
		if p.ID == uint64(*idFlag) {
			found = true
			break
		}
	}
	if !found {
		peersList = append(peersList, raft.Peer{ID: uint64(*idFlag)})
	}
	cfg := &nodeConfig{
		id:              uint64(*idFlag),
		httpAddr:        listenAddr,
		peerAddr:        peerAddr,
		initialPeers:    peersList,
		electionTick:    10,
		heartbeatTick:   1,
		maxSizePerMsg:   1 << 20,
		maxInflightMsgs: 256,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, err := newServer(cfg)
	if err != nil {
		log.Fatalf("erro ao criar servidor: %v", err)
	}
	go func() {
		if err := srv.run(ctx); err != nil {
			log.Printf("servidor terminou com erro: %v", err)
		}
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	<-signals
	log.Printf("encerrando nó %d", cfg.id)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.stop(shutdownCtx); err != nil {
		log.Printf("erro ao interromper: %v", err)
	}
}
