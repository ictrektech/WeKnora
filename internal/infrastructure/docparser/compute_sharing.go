package docparser

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/docreader/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	computePairingPrefix    = "hybrag-pair-v1."
	computePairingHeader    = "hybrag-docreader-pair"
	computeCredentialHeader = "hybrag-docreader-credential"
	computePairingTTL       = 10 * time.Minute
	maxSharedNodes          = 32
	maxSharedTextModels     = 32
)

type computeSharingConfig struct {
	Enabled            bool                  `json:"enabled"`
	DocumentProcessing bool                  `json:"document_processing,omitempty"`
	DocumentModel      *ComputeDocumentModel `json:"document_model,omitempty"`
	TextInference      bool                  `json:"text_inference,omitempty"`
	TextModels         []ComputeTextModel    `json:"text_models,omitempty"`
	Credentials        []string              `json:"credentials"`
}

type ComputeDocumentModel struct {
	TenantID uint64 `json:"tenant_id"`
	ModelID  string `json:"model_id"`
}

func (m *ComputeDocumentModel) valid() bool {
	return m != nil && m.TenantID != 0 && m.ModelID != ""
}

type ComputeTextModel struct {
	TenantID uint64 `json:"tenant_id"`
	ModelID  string `json:"model_id"`
}

func (m ComputeTextModel) valid() bool {
	return m.TenantID != 0 && strings.TrimSpace(m.ModelID) != ""
}

type ComputeTextModelInfo struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	DisplayName   string `json:"display_name,omitempty"`
	ContextWindow int    `json:"context_window,omitempty"`
}

type ComputeSharingStatus struct {
	Enabled            bool     `json:"enabled"`
	DocumentProcessing bool     `json:"document_processing"`
	DocumentModelID    string   `json:"document_model_id,omitempty"`
	TextInference      bool     `json:"text_inference"`
	TextModelIDs       []string `json:"text_model_ids,omitempty"`
	PairedNodes        int      `json:"paired_nodes"`
}

var (
	ErrComputeDocumentModelRequired = errors.New("select a vision model before enabling document processing")
	ErrComputeTextModelRequired     = errors.New("select at least one chat model before enabling text inference")
)

type ComputePairingCode struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

type computePairingBundle struct {
	Origin      string `json:"origin"`
	Fingerprint string `json:"fingerprint"`
	Ticket      string `json:"ticket"`
}

type ComputeSharing struct {
	mu           sync.Mutex
	path         string
	gateway      string
	local        *GRPCDocumentReader
	config       computeSharingConfig
	pendingHash  string
	pendingUntil time.Time
}

func NewComputeSharing(path, gateway string, local *GRPCDocumentReader) (*ComputeSharing, error) {
	s := &ComputeSharing{path: path, gateway: gateway, local: local}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &s.config); err != nil {
		return nil, fmt.Errorf("invalid saved DocReader sharing configuration")
	}
	return s, nil
}

func (s *ComputeSharing) Status() ComputeSharingStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	var modelID string
	if s.config.DocumentModel != nil {
		modelID = s.config.DocumentModel.ModelID
	}
	textIDs := make([]string, 0, len(s.config.TextModels))
	for _, m := range s.config.TextModels {
		if m.valid() {
			textIDs = append(textIDs, m.ModelID)
		}
	}
	return ComputeSharingStatus{
		Enabled:            s.config.Enabled,
		DocumentProcessing: s.config.DocumentProcessing && s.config.DocumentModel.valid(),
		DocumentModelID:    modelID,
		TextInference:      s.config.TextInference && len(textIDs) > 0,
		TextModelIDs:       textIDs,
		PairedNodes:        len(s.config.Credentials),
	}
}

func (s *ComputeSharing) Update(enabled *bool, revoke bool) error {
	return s.UpdateSharing(enabled, nil, nil, nil, nil, revoke)
}

func (s *ComputeSharing) UpdateDocumentProcessing(enabled, documentProcessing *bool, model *ComputeDocumentModel, revoke bool) error {
	return s.UpdateSharing(enabled, documentProcessing, model, nil, nil, revoke)
}

func (s *ComputeSharing) UpdateSharing(
	enabled, documentProcessing *bool,
	model *ComputeDocumentModel,
	textInference *bool,
	textModels []ComputeTextModel,
	revoke bool,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.config
	if enabled != nil {
		cfg.Enabled = *enabled
	}
	if documentProcessing != nil {
		cfg.DocumentProcessing = *documentProcessing
	}
	if model != nil {
		selected := *model
		cfg.DocumentModel = &selected
	}
	if documentProcessing != nil && *documentProcessing && !cfg.DocumentModel.valid() {
		return ErrComputeDocumentModelRequired
	}
	if textInference != nil {
		cfg.TextInference = *textInference
	}
	if textModels != nil {
		dedup := make([]ComputeTextModel, 0, len(textModels))
		seen := make(map[string]bool, len(textModels))
		for _, item := range textModels {
			item.ModelID = strings.TrimSpace(item.ModelID)
			if !item.valid() || seen[item.ModelID] {
				continue
			}
			seen[item.ModelID] = true
			dedup = append(dedup, item)
			if len(dedup) >= maxSharedTextModels {
				break
			}
		}
		cfg.TextModels = dedup
	}
	if textInference != nil && *textInference && len(cfg.TextModels) == 0 {
		return ErrComputeTextModelRequired
	}
	if len(cfg.TextModels) == 0 {
		cfg.TextInference = false
	}
	if revoke {
		cfg.Credentials = nil
	}
	if err := writeComputeConfig(s.path, cfg); err != nil {
		return err
	}
	s.config = cfg
	if !cfg.Enabled || revoke {
		s.pendingHash = ""
	}
	return nil
}

func (s *ComputeSharing) documentModel() (ComputeDocumentModel, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.config.Enabled || !s.config.DocumentProcessing || !s.config.DocumentModel.valid() {
		return ComputeDocumentModel{}, false
	}
	return *s.config.DocumentModel, true
}

func (s *ComputeSharing) textModels() ([]ComputeTextModel, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.config.Enabled || !s.config.TextInference || len(s.config.TextModels) == 0 {
		return nil, false
	}
	out := make([]ComputeTextModel, len(s.config.TextModels))
	copy(out, s.config.TextModels)
	return out, true
}

func (s *ComputeSharing) textModel(modelID string) (ComputeTextModel, bool) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ComputeTextModel{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.config.Enabled || !s.config.TextInference {
		return ComputeTextModel{}, false
	}
	for _, m := range s.config.TextModels {
		if m.ModelID == modelID && m.valid() {
			return m, true
		}
	}
	return ComputeTextModel{}, false
}

func newComputeSecret() (string, error) {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(secret[:]), nil
}

func computeSecretHash(secret string) string {
	hash := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(hash[:])
}

func (s *ComputeSharing) IssuePairingCode(ctx context.Context, origin string) (ComputePairingCode, error) {
	state := s.Status()
	if !state.Enabled || state.PairedNodes >= maxSharedNodes {
		return ComputePairingCode{}, status.Error(codes.FailedPrecondition, "enable sharing and ensure there is room for another pairing")
	}
	ticket, err := newComputeSecret()
	if err != nil {
		return ComputePairingCode{}, err
	}
	node, err := normalizeComputeNode(ComputeNode{Origin: origin, Token: ticket, Weight: 1}, nil)
	if err != nil || node.Origin == ComputeNodeLocal {
		return ComputePairingCode{}, fmt.Errorf("%w: open HybRAG from its VOS HTTPS address", ErrInvalidComputeRouting)
	}
	parsed, err := url.Parse(node.Origin)
	if err != nil {
		return ComputePairingCode{}, ErrInvalidComputeRouting
	}
	// Only fetch the certificate from the local VOS TCP gateway selected by
	// the operator's install environment. The supplied origin is used for
	// TLS SNI and the recipient's address, never as a dialing target here.
	ctx, cancel := context.WithTimeout(ctx, ComputeProbeTimeout)
	defer cancel()
	dialer := tls.Dialer{NetDialer: &net.Dialer{}, Config: &tls.Config{
		MinVersion: tls.VersionTLS12, ServerName: parsed.Hostname(), InsecureSkipVerify: true,
	}}
	conn, err := dialer.DialContext(ctx, "tcp", s.gateway)
	if err != nil {
		return ComputePairingCode{}, status.Error(codes.Unavailable, "local VOS HTTPS gateway is unreachable")
	}
	certificates := conn.(*tls.Conn).ConnectionState().PeerCertificates
	_ = conn.Close()
	if len(certificates) == 0 {
		return ComputePairingCode{}, status.Error(codes.Unavailable, "local VOS gateway has no certificate")
	}
	if now := time.Now(); now.Before(certificates[0].NotBefore) || now.After(certificates[0].NotAfter) {
		return ComputePairingCode{}, status.Error(codes.Unavailable, "local VOS gateway certificate is outside its validity period")
	}
	fingerprint := sha256.Sum256(certificates[0].Raw)
	bundle := computePairingBundle{node.Origin, hex.EncodeToString(fingerprint[:]), ticket}
	raw, err := json.Marshal(bundle)
	if err != nil {
		return ComputePairingCode{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.config.Enabled || len(s.config.Credentials) >= maxSharedNodes {
		return ComputePairingCode{}, status.Error(codes.FailedPrecondition, "enable sharing and ensure there is room for another pairing")
	}
	s.pendingHash = computeSecretHash(ticket)
	s.pendingUntil = time.Now().Add(computePairingTTL)
	return ComputePairingCode{computePairingPrefix + base64.RawURLEncoding.EncodeToString(raw), s.pendingUntil}, nil
}

func (s *ComputeSharing) authorize(ctx context.Context, allowPairing bool) error {
	return s.authorizeCapability(ctx, allowPairing, false)
}

func (s *ComputeSharing) authorizeCapability(ctx context.Context, allowPairing, documentProcessing bool) error {
	return s.authorizeCapabilities(ctx, allowPairing, documentProcessing, false)
}

func (s *ComputeSharing) authorizeMethod(ctx context.Context, fullMethod string) error {
	switch fullMethod {
	case proto.DocReader_GetInfo_FullMethodName:
		return s.authorizeCapabilities(ctx, true, false, false)
	case proto.DocumentCompute_Process_FullMethodName:
		return s.authorizeCapabilities(ctx, false, true, false)
	case proto.DocumentCompute_ListTextModels_FullMethodName, proto.DocumentCompute_GenerateText_FullMethodName:
		return s.authorizeCapabilities(ctx, false, false, true)
	default:
		return s.authorizeCapabilities(ctx, false, false, false)
	}
}

func (s *ComputeSharing) authorizeCapabilities(ctx context.Context, allowPairing, documentProcessing, textInference bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.config.Enabled {
		return status.Error(codes.PermissionDenied, "DocReader sharing is disabled")
	}
	tokens := metadata.ValueFromIncomingContext(ctx, "authorization")
	if len(tokens) != 1 || !strings.HasPrefix(tokens[0], "Bearer ") || len(tokens[0]) > 520 {
		return status.Error(codes.Unauthenticated, "DocReader credential required")
	}
	hash := computeSecretHash(strings.TrimPrefix(tokens[0], "Bearer "))
	if allowPairing && isComputePairing(ctx) && time.Now().Before(s.pendingUntil) &&
		subtle.ConstantTimeCompare([]byte(hash), []byte(s.pendingHash)) == 1 {
		return nil
	}
	for _, credential := range s.config.Credentials {
		if subtle.ConstantTimeCompare([]byte(hash), []byte(credential)) == 1 && !isComputePairing(ctx) {
			if documentProcessing && (!s.config.DocumentProcessing || !s.config.DocumentModel.valid()) {
				return status.Error(codes.PermissionDenied, "document processing sharing is disabled")
			}
			if textInference && (!s.config.TextInference || len(s.config.TextModels) == 0) {
				return status.Error(codes.PermissionDenied, "text inference sharing is disabled")
			}
			return nil
		}
	}
	return status.Error(codes.Unauthenticated, "DocReader credential rejected")
}

func isComputePairing(ctx context.Context) bool {
	values := metadata.ValueFromIncomingContext(ctx, computePairingHeader)
	return len(values) == 1 && values[0] == "1"
}

func (s *ComputeSharing) redeem(ctx context.Context) (string, error) {
	secret, err := newComputeSecret()
	if err != nil {
		return "", status.Error(codes.Internal, "could not issue a credential")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	token := metadata.ValueFromIncomingContext(ctx, "authorization")
	if !s.config.Enabled || !time.Now().Before(s.pendingUntil) || len(token) != 1 ||
		subtle.ConstantTimeCompare([]byte(computeSecretHash(strings.TrimPrefix(token[0], "Bearer "))), []byte(s.pendingHash)) != 1 {
		return "", status.Error(codes.Unauthenticated, "access code expired or already used")
	}
	if len(s.config.Credentials) >= maxSharedNodes {
		return "", status.Error(codes.ResourceExhausted, "paired node limit reached")
	}
	cfg := s.config
	cfg.Credentials = append(append([]string(nil), cfg.Credentials...), computeSecretHash(secret))
	if err := writeComputeConfig(s.path, cfg); err != nil {
		return "", status.Error(codes.Internal, "could not save DocReader authorization")
	}
	s.config = cfg
	s.pendingHash = ""
	return secret, nil
}

func (r *ComputeRouter) PairNode(ctx context.Context, code string, revision uint64, enableIfEmpty bool) (ComputeNodeInfo, error) {
	cfg := r.snapshot()
	if cfg.Revision != revision {
		return ComputeNodeInfo{}, ErrComputeRoutingConflict
	}
	if len(code) > 4096 || !strings.HasPrefix(code, computePairingPrefix) {
		return ComputeNodeInfo{}, fmt.Errorf("%w: invalid DocReader access code", ErrInvalidComputeRouting)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(code, computePairingPrefix))
	var bundle computePairingBundle
	if err != nil || json.Unmarshal(raw, &bundle) != nil || bundle.Fingerprint == "" {
		return ComputeNodeInfo{}, fmt.Errorf("%w: invalid DocReader access code", ErrInvalidComputeRouting)
	}
	node, err := normalizeComputeNode(ComputeNode{
		Origin: bundle.Origin, Fingerprint: bundle.Fingerprint, Token: bundle.Ticket, Weight: 1,
	}, nil)
	if err != nil {
		return ComputeNodeInfo{}, err
	}
	index := len(cfg.Nodes)
	for i, existing := range cfg.Nodes {
		if existing.Origin == node.Origin {
			index = i
			node.Weight = existing.Weight
			node.DocumentProcessing = existing.DocumentProcessing
			break
		}
	}
	if index >= maxComputeNodes {
		return ComputeNodeInfo{}, fmt.Errorf("%w: routing group is full", ErrInvalidComputeRouting)
	}
	reader, err := newComputeReader(node)
	if err != nil {
		return ComputeNodeInfo{}, err
	}
	defer reader.Close()
	ctx, cancel := context.WithTimeout(ctx, ComputeProbeTimeout)
	defer cancel()
	pairCtx := metadata.AppendToOutgoingContext(ctx, computePairingHeader, "1")
	var headers metadata.MD
	version, err := reader.client.GetInfo(pairCtx, &proto.GetInfoRequest{}, grpc.Header(&headers))
	if err != nil {
		return ComputeNodeInfo{}, err
	}
	credentials := headers.Get(computeCredentialHeader)
	if len(credentials) != 1 || credentials[0] == "" {
		return ComputeNodeInfo{}, status.Error(codes.FailedPrecondition, "node did not return a pairing credential")
	}
	node.Token = credentials[0]
	if enableIfEmpty && len(cfg.Nodes) == 0 {
		cfg.Enabled = true
		cfg.Strategy = localComputeStrategy
	}
	if index == len(cfg.Nodes) {
		cfg.Nodes = append(cfg.Nodes, node)
	} else {
		cfg.Nodes[index] = node
	}
	if err := r.Save(cfg); err != nil {
		return ComputeNodeInfo{}, err
	}
	// The authorization is already saved. A temporary catalogue failure
	// must not hide the fact that the one-time code has been consumed.
	info := ComputeNodeInfo{
		Component: "docreader", Origin: node.Origin, Version: version.GetVersion(),
		Capabilities: computeCapabilities(headers),
	}
	if result, err := r.Probe(ctx, node); err == nil {
		info.Engines = result.Engines
		info.Capabilities = result.Capabilities
		info.TextModels = result.TextModels
	}
	return info, nil
}
