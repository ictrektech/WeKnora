package docparser

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	docclient "github.com/Tencent/WeKnora/docreader/client"
	"github.com/Tencent/WeKnora/docreader/proto"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	ComputeNodeLocal        = "local"
	localComputeStrategy    = "order"
	weightedComputeStrategy = "weight"
	maxComputeNodes         = 16
	ComputeProbeTimeout     = 4 * time.Second
)

var (
	ErrComputeRoutingConflict = errors.New("routing settings changed; reload and try again")
	ErrInvalidComputeRouting  = errors.New("invalid DocReader routing settings")
)

type ComputeNode struct {
	Origin             string                 `json:"origin"`
	Token              string                 `json:"token,omitempty"`
	Fingerprint        string                 `json:"fingerprint,omitempty"`
	Weight             int                    `json:"weight"`
	DocumentProcessing bool                   `json:"document_processing,omitempty"`
	TextModels         []ComputeTextModelInfo `json:"text_models,omitempty"`
}

type ComputeRoutingConfig struct {
	Revision uint64        `json:"revision"`
	Enabled  bool          `json:"enabled"`
	Strategy string        `json:"strategy"`
	Nodes    []ComputeNode `json:"nodes"`
}

type ComputeNodeInfo struct {
	Component    string                   `json:"component"`
	Origin       string                   `json:"origin"`
	Version      string                   `json:"version"`
	Engines      []types.ParserEngineInfo `json:"engines"`
	Capabilities []string                 `json:"capabilities,omitempty"`
	TextModels   []ComputeTextModelInfo   `json:"text_models,omitempty"`
}

// ComputeTextNodeModels exposes a paired node's authorized text inference
// models to workspace users without leaking routing credentials.
type ComputeTextNodeModels struct {
	Origin string                 `json:"origin"`
	Models []ComputeTextModelInfo `json:"models"`
}

// ComputeRouter wraps the installed DocReader client. Native and external
// parsers selected by the parser registry continue to use their own transports.
type ComputeRouter struct {
	interfaces.DocumentReader
	path           string
	mu             sync.Mutex
	config         ComputeRoutingConfig
	scores         map[string]int
	documentScores map[string]int
	Sharing        *ComputeSharing
}

func NewComputeRouter(local interfaces.DocumentReader, path string) (*ComputeRouter, error) {
	r := &ComputeRouter{
		DocumentReader: local, path: path, scores: make(map[string]int), documentScores: make(map[string]int),
		config: ComputeRoutingConfig{Strategy: localComputeStrategy, Nodes: []ComputeNode{}},
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &r.config); err != nil {
		return nil, fmt.Errorf("invalid saved DocReader routing file")
	}
	r.config, err = normalizeComputeConfig(r.config, nil)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (r *ComputeRouter) Config() ComputeRoutingConfig {
	cfg := r.snapshot()
	for i := range cfg.Nodes {
		cfg.Nodes[i].Token = ""
	}
	return cfg
}

func (r *ComputeRouter) GetVersionInfo(ctx context.Context) (string, string, error) {
	if reader, ok := r.DocumentReader.(*GRPCDocumentReader); ok {
		return reader.GetVersionInfo(ctx)
	}
	return "", "", errNotConnected
}

func (r *ComputeRouter) snapshot() ComputeRoutingConfig {
	r.mu.Lock()
	defer r.mu.Unlock()
	cfg := r.config
	cfg.Nodes = append(make([]ComputeNode, 0, len(cfg.Nodes)), cfg.Nodes...)
	return cfg
}

func (r *ComputeRouter) Save(config ComputeRoutingConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if config.Revision != r.config.Revision {
		return ErrComputeRoutingConflict
	}
	previousTokens := make(map[string]string, len(r.config.Nodes))
	previousTextModels := make(map[string][]ComputeTextModelInfo, len(r.config.Nodes))
	for _, node := range r.config.Nodes {
		previousTokens[node.Origin] = node.Token
		if len(node.TextModels) > 0 {
			copied := make([]ComputeTextModelInfo, len(node.TextModels))
			copy(copied, node.TextModels)
			previousTextModels[node.Origin] = copied
		}
	}
	config, err := normalizeComputeConfig(config, previousTokens)
	if err != nil {
		return err
	}
	for i := range config.Nodes {
		if config.Nodes[i].Origin == ComputeNodeLocal {
			config.Nodes[i].TextModels = nil
			continue
		}
		if len(config.Nodes[i].TextModels) == 0 {
			if cached, ok := previousTextModels[config.Nodes[i].Origin]; ok {
				config.Nodes[i].TextModels = cached
			}
		}
	}
	config.Revision++
	if err := writeComputeConfig(r.path, config); err != nil {
		return err
	}
	r.config = config
	clear(r.scores)
	clear(r.documentScores)
	return nil
}

func writeComputeConfig(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".docreader-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func normalizeComputeConfig(cfg ComputeRoutingConfig, tokens map[string]string) (ComputeRoutingConfig, error) {
	if cfg.Strategy != localComputeStrategy && cfg.Strategy != weightedComputeStrategy {
		return cfg, fmt.Errorf("%w: strategy must be order or weight", ErrInvalidComputeRouting)
	}
	if len(cfg.Nodes) > maxComputeNodes || (cfg.Enabled && len(cfg.Nodes) == 0) {
		return cfg, fmt.Errorf("%w: configure 1–%d nodes when enabled", ErrInvalidComputeRouting, maxComputeNodes)
	}
	normalized := make([]ComputeNode, 0, len(cfg.Nodes))
	seen := make(map[string]bool, len(cfg.Nodes))
	for _, node := range cfg.Nodes {
		node, err := normalizeComputeNode(node, tokens)
		if err != nil {
			return cfg, err
		}
		if seen[node.Origin] {
			return cfg, fmt.Errorf("%w: duplicate DocReader origin", ErrInvalidComputeRouting)
		}
		seen[node.Origin] = true
		normalized = append(normalized, node)
	}
	cfg.Nodes = normalized
	return cfg, nil
}

func normalizeComputeNode(node ComputeNode, tokens map[string]string) (ComputeNode, error) {
	if node.Weight < 1 || node.Weight > 100 {
		return node, fmt.Errorf("%w: node weight must be 1–100", ErrInvalidComputeRouting)
	}
	if node.Origin == ComputeNodeLocal {
		if node.Token != "" || node.Fingerprint != "" {
			return node, fmt.Errorf("%w: local node cannot have credentials", ErrInvalidComputeRouting)
		}
		node.DocumentProcessing = false
		node.TextModels = nil
		return node, nil
	}
	origin, err := url.Parse(strings.TrimSpace(node.Origin))
	if err != nil || origin.Scheme != "https" || origin.User != nil || origin.RawQuery != "" ||
		origin.ForceQuery || origin.Fragment != "" || (origin.Path != "" && origin.Path != "/") {
		return node, fmt.Errorf("%w: enter an HTTPS VOS origin without a path", ErrInvalidComputeRouting)
	}
	host := strings.ToLower(strings.TrimSuffix(origin.Hostname(), "."))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.Contains(host, "%") {
		return node, fmt.Errorf("%w: enter a reachable VOS host", ErrInvalidComputeRouting)
	}
	if ip, err := netip.ParseAddr(host); err == nil && (!ip.Unmap().IsGlobalUnicast() || ip.Unmap().IsLoopback()) {
		return node, fmt.Errorf("%w: enter a reachable VOS host", ErrInvalidComputeRouting)
	}
	port := origin.Port()
	if port == "" {
		port = "443"
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return node, fmt.Errorf("%w: invalid HTTPS port", ErrInvalidComputeRouting)
	}
	node.Origin = "https://" + net.JoinHostPort(host, strconv.Itoa(number))
	if node.Token == "" {
		node.Token = tokens[node.Origin]
	}
	if len(node.Token) < 16 || len(node.Token) > 512 || strings.ContainsFunc(node.Token, func(c rune) bool {
		return c <= ' ' || c >= 127
	}) {
		return node, fmt.Errorf("%w: node token must be 16–512 printable ASCII characters without spaces", ErrInvalidComputeRouting)
	}
	node.Fingerprint = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(node.Fingerprint), ":", ""))
	if node.Fingerprint != "" {
		if pin, err := hex.DecodeString(node.Fingerprint); err != nil || len(pin) != 32 {
			return node, fmt.Errorf("%w: invalid SHA-256 certificate fingerprint", ErrInvalidComputeRouting)
		}
	}
	return node, nil
}

func (r *ComputeRouter) Probe(ctx context.Context, node ComputeNode) (ComputeNodeInfo, error) {
	node.Weight = 1
	tokens := make(map[string]string)
	for _, existing := range r.snapshot().Nodes {
		tokens[existing.Origin] = existing.Token
	}
	node, err := normalizeComputeNode(node, tokens)
	if err != nil {
		return ComputeNodeInfo{}, err
	}
	if node.Origin == ComputeNodeLocal {
		return ComputeNodeInfo{}, fmt.Errorf("%w: enter a remote origin", ErrInvalidComputeRouting)
	}
	reader, err := newComputeReader(node)
	if err != nil {
		return ComputeNodeInfo{}, err
	}
	defer reader.Close()
	ctx, cancel := context.WithTimeout(ctx, ComputeProbeTimeout)
	defer cancel()
	var headers metadata.MD
	info, err := reader.client.GetInfo(ctx, &proto.GetInfoRequest{}, grpc.Header(&headers))
	if err != nil {
		return ComputeNodeInfo{}, err
	}
	engines, err := reader.ListEngines(ctx, nil)
	if err != nil {
		return ComputeNodeInfo{}, err
	}
	capabilities := computeCapabilities(headers)
	var textModels []ComputeTextModelInfo
	if slices.Contains(capabilities, computeCapabilityTextInference) {
		listed, listErr := proto.NewDocumentComputeClient(reader.conn).ListTextModels(ctx, &proto.ListTextModelsRequest{})
		if listErr != nil {
			return ComputeNodeInfo{}, listErr
		}
		textModels = make([]ComputeTextModelInfo, 0, len(listed.GetModels()))
		for _, m := range listed.GetModels() {
			if m == nil || strings.TrimSpace(m.GetId()) == "" {
				continue
			}
			textModels = append(textModels, ComputeTextModelInfo{
				ID:            strings.TrimSpace(m.GetId()),
				Name:          strings.TrimSpace(m.GetName()),
				DisplayName:   strings.TrimSpace(m.GetDisplayName()),
				ContextWindow: int(m.GetContextWindow()),
			})
		}
	}
	r.updateNodeTextModels(node.Origin, textModels)
	return ComputeNodeInfo{
		Component: "docreader", Origin: node.Origin, Version: info.GetVersion(), Engines: engines,
		Capabilities: capabilities, TextModels: textModels,
	}, nil
}

func (r *ComputeRouter) updateNodeTextModels(origin string, models []ComputeTextModelInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.config.Nodes {
		if r.config.Nodes[i].Origin != origin {
			continue
		}
		copied := make([]ComputeTextModelInfo, len(models))
		copy(copied, models)
		r.config.Nodes[i].TextModels = copied
		if r.path != "" {
			_ = writeComputeConfig(r.path, r.config)
		}
		return
	}
}

// ListDiscoveredTextModels returns paired remote nodes that have authorized
// text-inference models discovered through pairing or node checks.
func (r *ComputeRouter) ListDiscoveredTextModels() []ComputeTextNodeModels {
	cfg := r.snapshot()
	out := make([]ComputeTextNodeModels, 0, len(cfg.Nodes))
	for _, node := range cfg.Nodes {
		if node.Origin == ComputeNodeLocal || len(node.TextModels) == 0 {
			continue
		}
		models := make([]ComputeTextModelInfo, len(node.TextModels))
		copy(models, node.TextModels)
		out = append(out, ComputeTextNodeModels{
			Origin: node.Origin,
			Models: models,
		})
	}
	return out
}

// LookupTextModel checks whether origin is a paired node with modelID in its
// discovered text-inference catalog.
func (r *ComputeRouter) LookupTextModel(origin, modelID string) (ComputeTextModelInfo, bool) {
	origin = NormalizeComputeOrigin(origin)
	modelID = strings.TrimSpace(modelID)
	if origin == "" || origin == ComputeNodeLocal || modelID == "" {
		return ComputeTextModelInfo{}, false
	}
	cfg := r.snapshot()
	for _, node := range cfg.Nodes {
		if node.Origin != origin {
			continue
		}
		for _, m := range node.TextModels {
			if m.ID == modelID {
				return m, true
			}
		}
	}
	return ComputeTextModelInfo{}, false
}

// NormalizeComputeOrigin canonicalizes a remote HTTPS origin to the same form
// persisted in ComputeRoutingConfig, or returns "local" for the local node.
func NormalizeComputeOrigin(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.EqualFold(trimmed, ComputeNodeLocal) {
		return ComputeNodeLocal
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" ||
		parsed.ForceQuery || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "" {
		return ""
	}
	port := parsed.Port()
	if port == "" {
		port = "443"
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return ""
	}
	return "https://" + net.JoinHostPort(host, strconv.Itoa(number))
}

// GenerateText executes one text-inference request on a paired compute node.
func (r *ComputeRouter) GenerateText(
	ctx context.Context,
	origin string,
	req *proto.TextInferenceRequest,
) (*proto.TextInferenceResponse, error) {
	normalized := NormalizeComputeOrigin(origin)
	if normalized == "" || normalized == ComputeNodeLocal {
		return nil, fmt.Errorf("%w: invalid remote compute node origin", ErrInvalidComputeRouting)
	}
	cfg := r.snapshot()
	var target *ComputeNode
	for i := range cfg.Nodes {
		if cfg.Nodes[i].Origin == normalized {
			n := cfg.Nodes[i]
			target = &n
			break
		}
	}
	if target == nil {
		return nil, status.Errorf(codes.NotFound, "compute node %s is not paired", normalized)
	}
	reader, err := newComputeReader(*target)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	var trailers metadata.MD
	resp, err := proto.NewDocumentComputeClient(reader.conn).GenerateText(ctx, req, grpc.Trailer(&trailers))
	if err != nil {
		if busy, ok := busyErrorFromTrailers(err, trailers); ok {
			return nil, busy
		}
		return nil, err
	}
	return resp, nil
}

func computeCapabilities(headers metadata.MD) []string {
	var capabilities []string
	for _, capability := range headers.Get(computeCapabilityHeader) {
		switch capability {
		case computeCapabilityDocumentProcessing, computeCapabilityTextInference:
			if !slices.Contains(capabilities, capability) {
				capabilities = append(capabilities, capability)
			}
		}
	}
	return capabilities
}

func newComputeReader(node ComputeNode) (*GRPCDocumentReader, error) {
	pin, _ := hex.DecodeString(node.Fingerprint) // validated before persistence
	reader := &GRPCDocumentReader{}
	err := reader.connectWithAuth(strings.TrimPrefix(node.Origin, "https://"), &docclient.AuthConfig{
		TLSEnabled: true, AuthToken: node.Token, CertificatePin: pin,
	}, grpc.WithDisableRetry(), grpc.WithNoProxy())
	if err != nil {
		return nil, err
	}
	return reader, nil
}

type computeCandidate struct {
	node   ComputeNode
	reader interfaces.DocumentReader
	remote *GRPCDocumentReader
}

func (r *ComputeRouter) Read(ctx context.Context, req *types.ReadRequest) (*types.ReadResult, error) {
	cfg := r.snapshot()
	if !cfg.Enabled || req.URL != "" {
		return r.DocumentReader.Read(ctx, req)
	}
	useDocumentCompute := req.DocumentCompute != nil && slices.ContainsFunc(cfg.Nodes, func(node ComputeNode) bool {
		return node.DocumentProcessing
	})
	candidates := make([]computeCandidate, 0, len(cfg.Nodes))
	for _, node := range cfg.Nodes {
		if useDocumentCompute && node.Origin != ComputeNodeLocal && !node.DocumentProcessing {
			continue
		}
		candidate := computeCandidate{node: node, reader: r.DocumentReader}
		if node.Origin != ComputeNodeLocal {
			reader, err := newComputeReader(node)
			if err != nil {
				logger.Warnf(ctx, "DocReader node %s was skipped: %v", node.Origin, err)
				continue
			}
			defer reader.Close()
			candidate.reader, candidate.remote = reader, reader
		}
		candidates = append(candidates, candidate)
	}

	// Probe candidates before sending document bytes. A failed readiness RPC
	// can safely disqualify a node; the Read RPC itself is never replayed here.
	probeCtx, cancelProbes := context.WithTimeout(ctx, ComputeProbeTimeout)
	engineName := req.ParserEngine
	fileType := strings.TrimPrefix(strings.ToLower(req.FileType), ".")
	readiness := make([]chan bool, len(candidates))
	for index, candidate := range candidates {
		ready := make(chan bool, 1)
		readiness[index] = ready
		go func() {
			engines, err := candidate.reader.ListEngines(probeCtx, nil)
			supported := err == nil && engineIsSupported(engines, engineName, fileType)
			if supported && useDocumentCompute && candidate.remote != nil {
				var headers metadata.MD
				_, err = candidate.remote.client.GetInfo(probeCtx, &proto.GetInfoRequest{}, grpc.Header(&headers))
				supported = err == nil && slices.Contains(computeCapabilities(headers), computeCapabilityDocumentProcessing) &&
					engineIsSupported(engines, BuiltinEngineName, fileType)
			}
			if !supported && probeCtx.Err() != context.Canceled {
				logger.Warnf(ctx, "DocReader node %s was skipped: supported=%t probe_error=%v", candidate.node.Origin, supported, err)
			}
			ready <- supported
		}()
	}
	eligible := make([]computeCandidate, 0, len(candidates))
	for i, candidate := range candidates {
		if <-readiness[i] {
			eligible = append(eligible, candidate)
			if cfg.Strategy == localComputeStrategy {
				break
			}
		}
	}
	cancelProbes()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(eligible) == 0 {
		if useDocumentCompute {
			return nil, status.Error(codes.Unavailable, "no configured node is ready for PDF document processing")
		}
		return nil, status.Error(codes.Unavailable, "no configured DocReader node is available for this parser and file type")
	}
	chosen := r.chooseForCapability(cfg, eligible, useDocumentCompute)
	logger.Infof(ctx, "DocReader node %s selected (strategy=%s request=%s)", chosen.node.Origin, cfg.Strategy, req.RequestID)
	var result *types.ReadResult
	var err error
	if useDocumentCompute && chosen.remote != nil {
		result, err = chosen.remote.computeDocument(ctx, req)
	} else {
		result, err = chosen.reader.Read(ctx, req)
	}
	if err != nil {
		return nil, fmt.Errorf("DocReader node %s: %w", chosen.node.Origin, err)
	}
	if result != nil {
		result.DocumentComputed = useDocumentCompute
		if result.Metadata == nil {
			result.Metadata = make(map[string]string)
		}
		result.Metadata["docreader_node"] = chosen.node.Origin
		result.Metadata["docreader_strategy"] = cfg.Strategy
		if chosen.remote != nil {
			result.ImageDirPath = ""
		}
	}
	return result, nil
}

func engineIsSupported(engines []types.ParserEngineInfo, engineName, fileType string) bool {
	for _, engine := range engines {
		if !engine.Available || (engineName != "" && engineName != "auto" && engineName != engine.Name) {
			continue
		}
		for _, supported := range engine.FileTypes {
			if strings.EqualFold(strings.TrimPrefix(supported, "."), fileType) {
				return true
			}
		}
	}
	return false
}

func (r *ComputeRouter) choose(cfg ComputeRoutingConfig, eligible []computeCandidate) computeCandidate {
	return r.chooseForCapability(cfg, eligible, false)
}

func (r *ComputeRouter) chooseForCapability(cfg ComputeRoutingConfig, eligible []computeCandidate, documentProcessing bool) computeCandidate {
	if cfg.Strategy != weightedComputeStrategy {
		return eligible[0]
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	scores := r.scores
	if documentProcessing {
		scores = r.documentScores
	}
	if cfg.Revision != r.config.Revision {
		// An in-flight request retains its routing snapshot. Do not mutate
		// the new configuration's weighted round-robin state.
		scores = make(map[string]int)
	}
	active := make(map[string]bool, len(eligible))
	total := 0
	best := eligible[0]
	for _, candidate := range eligible {
		node := candidate.node
		active[node.Origin] = true
		scores[node.Origin] += node.Weight
		total += node.Weight
		if scores[node.Origin] > scores[best.node.Origin] {
			best = candidate
		}
	}
	for node := range scores {
		if !active[node] {
			delete(scores, node)
		}
	}
	scores[best.node.Origin] -= total
	return best
}
