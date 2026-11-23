package handler

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
)

var (
	vnoteDocumentID    = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
	vnoteDocumentToken = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type VNoteDocumentHandler struct {
	identity *VNoteImportHandler
	parser   *service.VNoteDocumentParser
	root     string
}

type vnoteDocumentJob struct {
	UserID    string
	TenantID  uint64
	TokenHash string
	Name      string
	Error     string
	CreatedAt time.Time
}

func NewVNoteDocumentHandler(identity *VNoteImportHandler, parser *service.VNoteDocumentParser) *VNoteDocumentHandler {
	return &VNoteDocumentHandler{identity: identity, parser: parser, root: os.Getenv("HYBRAG_VNOTE_DOCUMENT_DIR")}
}

func (h *VNoteDocumentHandler) Submit(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.root == "" || !vosSSOEnabled() {
		c.Status(http.StatusNotFound)
		return
	}
	id := c.Param("id")
	token := c.GetHeader("X-Document-Token")
	if !vnoteDocumentID.MatchString(id) || !vnoteDocumentToken.MatchString(token) {
		c.Status(http.StatusBadRequest)
		return
	}
	auth := strings.Fields(c.GetHeader("Authorization"))
	if len(auth) != 2 || !strings.EqualFold(auth[0], "Bearer") {
		c.Status(http.StatusUnauthorized)
		return
	}
	ctx := c.Request.Context()
	identity, err := verifyVNoteIdentity(ctx, auth[0], auth[1])
	if err != nil {
		c.Status(http.StatusUnauthorized)
		return
	}
	identities, ok := h.identity.users.(platformIdentityService)
	if !ok {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	user, err := identities.GetPlatformIdentityUser(ctx, vosOIDCUserinfoURL(), identity.Subject)
	if err != nil || user == nil || !user.IsActive || user.TenantID == 0 {
		c.JSON(http.StatusConflict, gin.H{"code": "identity_binding_required"})
		return
	}
	membership, err := h.identity.members.GetMembership(ctx, user.ID, user.TenantID)
	if err != nil || membership == nil || membership.Status != types.TenantMemberStatusActive || membership.Role != types.TenantRoleOwner {
		c.Status(http.StatusForbidden)
		return
	}
	dir := filepath.Join(h.root, id)
	hash := vnoteDocumentTokenHash(token)
	if found, err := readVNoteDocumentJob(dir); err == nil {
		if found.UserID != user.ID || found.TokenHash != hash {
			c.Status(http.StatusConflict)
			return
		}
		c.Status(http.StatusAccepted)
		return
	}
	name := filepath.Base(c.GetHeader("X-Document-Filename"))
	// The caller sends a UTF-8 filename as a percent-encoded header.
	name, err = url.PathUnescape(name)
	if err != nil || len(name) > 255 || name != filepath.Base(name) || strings.ContainsAny(name, "\\\x00\r\n") {
		c.Status(http.StatusBadRequest)
		return
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".docx", ".xlsx", ".pptx", ".pdf":
	default:
		c.Status(http.StatusUnsupportedMediaType)
		return
	}
	if err := os.MkdirAll(h.root, 0700); err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	stage, err := os.MkdirTemp(h.root, ".upload-")
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	defer os.RemoveAll(stage)
	file, err := os.OpenFile(filepath.Join(stage, "source"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	maxBytes := min(utils.GetMaxFileSize(), 100<<20)
	size, copyErr := io.Copy(file, http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || size == 0 {
		var limit *http.MaxBytesError
		if errors.As(copyErr, &limit) {
			c.Status(http.StatusRequestEntityTooLarge)
		} else {
			c.Status(http.StatusBadRequest)
		}
		return
	}
	job := vnoteDocumentJob{UserID: user.ID, TenantID: user.TenantID, TokenHash: hash, Name: name, CreatedAt: time.Now().UTC()}
	if err := writeVNoteDocumentJSON(stage, "job", job); err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	if err := os.Rename(stage, dir); err != nil {
		if found, readErr := readVNoteDocumentJob(dir); readErr != nil || found.TokenHash != hash || found.UserID != user.ID {
			c.Status(http.StatusConflict)
			return
		}
	}
	c.Status(http.StatusAccepted)
}

func (h *VNoteDocumentHandler) Result(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id := c.Param("id")
	auth := strings.Fields(c.GetHeader("Authorization"))
	if h.root == "" || !vnoteDocumentID.MatchString(id) || len(auth) != 2 || auth[0] != "Document" || !vnoteDocumentToken.MatchString(auth[1]) {
		c.Status(http.StatusNotFound)
		return
	}
	dir := filepath.Join(h.root, id)
	job, err := readVNoteDocumentJob(dir)
	if err != nil || subtle.ConstantTimeCompare([]byte(vnoteDocumentTokenHash(auth[1])), []byte(job.TokenHash)) != 1 {
		c.Status(http.StatusNotFound)
		return
	}
	if c.Request.Method == http.MethodDelete {
		// Only terminal jobs may be acknowledged by a consumer.
		if _, err := os.Stat(filepath.Join(dir, "terminal")); err != nil {
			c.Status(http.StatusConflict)
			return
		}
		if err := os.RemoveAll(dir); err != nil {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		c.Status(http.StatusNoContent)
		return
	}
	if _, err := os.Stat(filepath.Join(dir, "terminal")); err != nil {
		if name := c.Param("image"); name != "" {
			c.Status(http.StatusNotFound)
			return
		}
		var progress struct {
			Done  int `json:"done"`
			Total int `json:"total"`
		}
		if raw, err := os.ReadFile(filepath.Join(dir, "progress")); err == nil && json.Unmarshal(raw, &progress) == nil {
			c.JSON(http.StatusAccepted, gin.H{"processing": true, "done": progress.Done, "total": progress.Total})
		} else {
			c.JSON(http.StatusAccepted, gin.H{"processing": false})
		}
		return
	}
	if job.Error != "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": job.Error})
		return
	}
	name := "result"
	contentType := "application/json"
	if image := c.Param("image"); image != "" {
		index, err := strconv.Atoi(image)
		if err != nil || index < 0 {
			c.Status(http.StatusNotFound)
			return
		}
		name, contentType = "image-"+strconv.Itoa(index), "application/octet-stream"
	}
	file, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.DataFromReader(http.StatusOK, info.Size(), contentType, file, nil)
}

func (h *VNoteDocumentHandler) Start(cleaner interfaces.ResourceCleaner) {
	if h.root == "" || !vosSSOEnabled() {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			h.processNext(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	cleaner.RegisterWithName("VNoteDocumentWorker", func() error {
		cancel()
		<-done
		return nil
	})
}

func (h *VNoteDocumentHandler) processNext(ctx context.Context) {
	dirs, _ := os.ReadDir(h.root)
	var pendingDir string
	var pending vnoteDocumentJob
	for _, entry := range dirs {
		if ctx.Err() != nil {
			return
		}
		dir := filepath.Join(h.root, entry.Name())
		if strings.HasPrefix(entry.Name(), ".upload-") {
			if info, err := entry.Info(); err == nil && time.Since(info.ModTime()) > 24*time.Hour {
				_ = os.RemoveAll(dir)
			}
			continue
		}
		if !entry.IsDir() || !vnoteDocumentID.MatchString(entry.Name()) {
			continue
		}
		job, err := readVNoteDocumentJob(dir)
		if err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, "terminal")); err == nil {
			if time.Since(job.CreatedAt) > 7*24*time.Hour {
				_ = os.RemoveAll(dir)
			}
			continue
		}
		if pendingDir == "" || job.CreatedAt.Before(pending.CreatedAt) {
			pendingDir, pending = dir, job
		}
	}
	if pendingDir == "" {
		return
	}
	taskCtx, cancel := context.WithTimeout(ctx, 90*time.Minute)
	defer cancel()
	progress := func(done, total int) {
		_ = writeVNoteDocumentJSON(pendingDir, "progress", gin.H{"done": done, "total": total})
	}
	progress(0, 0)
	data, err := os.ReadFile(filepath.Join(pendingDir, "source"))
	if err == nil {
		var result *service.VNoteDocumentResult
		result, err = h.parser.Parse(taskCtx, pending.TenantID, pending.UserID, pending.Name, data, progress)
		if err == nil {
			for index, image := range result.Images {
				err = os.WriteFile(filepath.Join(pendingDir, "image-"+strconv.Itoa(index)), image.Data, 0600)
				if err != nil {
					break
				}
			}
			if err == nil {
				err = writeVNoteDocumentJSON(pendingDir, "result", result)
			}
		}
	}
	if ctx.Err() != nil {
		return
	}
	pending.Error = ""
	if err != nil {
		pending.Error = service.ErrVNoteDocumentParse.Error()
		for _, code := range []error{service.ErrVNoteDocumentOCRConfig, service.ErrVNoteDocumentOCR, service.ErrVNoteDocumentEmpty} {
			if errors.Is(err, code) {
				pending.Error = code.Error()
			}
		}
	}
	if writeVNoteDocumentJSON(pendingDir, "job", pending) == nil && writeVNoteDocumentJSON(pendingDir, "terminal", true) == nil {
		_ = os.Remove(filepath.Join(pendingDir, "source"))
	}
}

func vnoteDocumentTokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func readVNoteDocumentJob(dir string) (vnoteDocumentJob, error) {
	var job vnoteDocumentJob
	raw, err := os.ReadFile(filepath.Join(dir, "job"))
	if err != nil {
		return job, err
	}
	return job, json.Unmarshal(raw, &job)
}

func writeVNoteDocumentJSON(dir, name string, value any) error {
	file, err := os.CreateTemp(dir, ".write-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	err = json.NewEncoder(file).Encode(value)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), filepath.Join(dir, name))
}
