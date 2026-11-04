package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

// maxCreativeArtifactBytes accommodates current 620 MB video masters without
// treating the shared daemon's disk as unlimited storage. Transfers stream.
const maxCreativeArtifactBytes int64 = 2 << 30

var errCreativeArtifactTooLarge = errors.New("creative artifact exceeds 2 GiB")

type creativeArtifactVersionJSON struct {
	store.CreativeArtifactVersion
	ContentURL  string `json:"content_url"`
	DownloadURL string `json:"download_url"`
}

func creativeVersionJSON(v store.CreativeArtifactVersion) creativeArtifactVersionJSON {
	u := fmt.Sprintf("/v1/creative/artifact-versions/%d/content", v.ID)
	return creativeArtifactVersionJSON{CreativeArtifactVersion: v, ContentURL: u, DownloadURL: u + "?download=1"}
}

func registerCreativeArtifactRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/creative/runs/{run}/artifacts", func(w http.ResponseWriter, r *http.Request) {
		ctx, err := creativeArtifactRequestContext(r, d)
		if creativeArtifactError(w, err) {
			return
		}
		list, err := d.Store.ListCreativeArtifacts(ctx.RunID, ctx.StageID, ctx.ItemID)
		if creativeArtifactError(w, err) {
			return
		}
		writeJSON(w, 200, list)
	})
	mux.HandleFunc("POST /v1/creative/runs/{run}/artifacts", func(w http.ResponseWriter, r *http.Request) {
		ctx, err := creativeArtifactRequestContext(r, d)
		if creativeArtifactError(w, err) {
			return
		}
		p := store.CreativeArtifactPublication{RunID: ctx.RunID, StageID: ctx.StageID, ItemID: ctx.ItemID, Name: strings.TrimSpace(r.URL.Query().Get("name")), Kind: r.URL.Query().Get("kind")}
		if p.Name == "" || len(p.Name) > 512 || (p.Kind != "source" && p.Kind != "result") {
			writeErr(w, 400, "invalid_artifact", "name and kind=source|result are required")
			return
		}
		handleCreativeArtifactUpload(w, r, d, ctx, p)
	})
	mux.HandleFunc("GET /v1/creative/artifacts/{artifact}", func(w http.ResponseWriter, r *http.Request) {
		a, err := creativeArtifactFromRequest(r, d)
		if creativeArtifactError(w, err) {
			return
		}
		versions, err := d.Store.ListCreativeArtifactVersions(a.ID)
		if creativeArtifactError(w, err) {
			return
		}
		out := make([]creativeArtifactVersionJSON, 0, len(versions))
		for _, v := range versions {
			out = append(out, creativeVersionJSON(v))
		}
		writeJSON(w, 200, map[string]any{"artifact": a, "versions": out})
	})
	mux.HandleFunc("POST /v1/creative/artifacts/{artifact}/versions", func(w http.ResponseWriter, r *http.Request) {
		a, err := creativeArtifactFromRequest(r, d)
		if creativeArtifactError(w, err) {
			return
		}
		ctx, err := d.Store.ResolveCreativeContext(a.RunID, a.StageID, a.ItemID)
		if creativeArtifactError(w, err) {
			return
		}
		handleCreativeArtifactUpload(w, r, d, ctx, store.CreativeArtifactPublication{ArtifactID: a.ID, RunID: a.RunID, StageID: a.StageID, ItemID: a.ItemID})
	})
	mux.HandleFunc("PUT /v1/creative/artifacts/{artifact}/current", func(w http.ResponseWriter, r *http.Request) {
		a, err := creativeArtifactFromRequest(r, d)
		if creativeArtifactError(w, err) {
			return
		}
		ctx, err := d.Store.ResolveCreativeContext(a.RunID, a.StageID, a.ItemID)
		if creativeArtifactError(w, err) {
			return
		}
		if _, ok := creativeArtifactWriter(w, r, d, ctx); !ok {
			return
		}
		var in struct {
			VersionID int64 `json:"version_id"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil || in.VersionID <= 0 {
			writeErr(w, 400, "invalid_version", "positive version_id required")
			return
		}
		if err := dec.Decode(&struct{}{}); err != io.EOF {
			writeErr(w, 400, "invalid_version", "expected one JSON object")
			return
		}
		if creativeArtifactError(w, d.Store.SetCreativeArtifactCurrent(a.ID, in.VersionID)) {
			return
		}
		a, err = d.Store.GetCreativeArtifact(a.ID)
		if creativeArtifactError(w, err) {
			return
		}
		writeJSON(w, 200, a)
	})
	mux.HandleFunc("GET /v1/creative/artifact-versions/{version}", func(w http.ResponseWriter, r *http.Request) {
		v, err := creativeArtifactVersionFromRequest(r, d)
		if creativeArtifactError(w, err) {
			return
		}
		writeJSON(w, 200, creativeVersionJSON(v))
	})
	// A GET pattern also handles HEAD; ServeContent implements range/conditional access.
	mux.HandleFunc("GET /v1/creative/artifact-versions/{version}/content", func(w http.ResponseWriter, r *http.Request) { handleCreativeArtifactContent(w, r, d) })
}

func creativeArtifactPositiveID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%w: positive integer ID required", store.ErrCreativeInvalid)
	}
	return id, nil
}
func creativeArtifactRequestContext(r *http.Request, d Deps) (store.CreativeContext, error) {
	run, err := creativeArtifactPositiveID(r.PathValue("run"))
	if err != nil {
		return store.CreativeContext{}, err
	}
	var ids [2]int64
	for i, key := range []string{"stage_id", "item_id"} {
		if raw := r.URL.Query().Get(key); raw != "" {
			ids[i], err = creativeArtifactPositiveID(raw)
			if err != nil {
				return store.CreativeContext{}, err
			}
		}
	}
	return d.Store.ResolveCreativeContext(run, ids[0], ids[1])
}
func creativeArtifactFromRequest(r *http.Request, d Deps) (store.CreativeArtifact, error) {
	id, err := creativeArtifactPositiveID(r.PathValue("artifact"))
	if err != nil {
		return store.CreativeArtifact{}, err
	}
	return d.Store.GetCreativeArtifact(id)
}
func creativeArtifactVersionFromRequest(r *http.Request, d Deps) (store.CreativeArtifactVersion, error) {
	id, err := creativeArtifactPositiveID(r.PathValue("version"))
	if err != nil {
		return store.CreativeArtifactVersion{}, err
	}
	return d.Store.GetCreativeArtifactVersion(id)
}
func creativeArtifactError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, 404, "creative_artifact_not_found", "creative context, artifact or version not found")
	case errors.Is(err, store.ErrCreativeInvalid):
		writeErr(w, 400, "invalid_creative_artifact", err.Error())
	default:
		writeErr(w, 500, "creative_artifact_failed", "creative artifact storage/publication failed: "+err.Error())
	}
	return true
}
func creativeArtifactWriter(w http.ResponseWriter, r *http.Request, d Deps, ctx store.CreativeContext) (string, bool) {
	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return "", false
	}
	if caller == nil {
		return "", true
	}
	if caller.ProjectID != ctx.ProjectID {
		writeErr(w, 403, "creative_artifact_forbidden", "session belongs to another project")
		return "", false
	}
	return caller.ID, true
}

func handleCreativeArtifactUpload(w http.ResponseWriter, r *http.Request, d Deps, ctx store.CreativeContext, p store.CreativeArtifactPublication) {
	var ok bool
	p.AuthorSessionID, ok = creativeArtifactWriter(w, r, d, ctx)
	if !ok {
		return
	}
	filename := strings.ReplaceAll(r.URL.Query().Get("filename"), "\\", "/")
	filename = filepath.Base(filename)
	if filename == "" || filename == "." || filename == "/" || len(filename) > 255 || strings.ContainsAny(filename, "\r\n\x00") {
		writeErr(w, 400, "invalid_filename", "filename is required and must be at most 255 bytes")
		return
	}
	p.Filename = filename
	p.MIME = "application/octet-stream"
	if raw := r.Header.Get("Content-Type"); raw != "" {
		var err error
		p.MIME, _, err = mime.ParseMediaType(raw)
		if err != nil || !strings.Contains(p.MIME, "/") || strings.Contains(p.MIME, "*") {
			writeErr(w, 400, "invalid_media_type", "valid Content-Type required")
			return
		}
	}
	if r.ContentLength > maxCreativeArtifactBytes {
		writeErr(w, 413, "too_large", errCreativeArtifactTooLarge.Error())
		return
	}
	dir := filepath.Join(d.Cfg.Home, "creative-artifacts")
	key, size, hash, err := writeCreativeArtifactBlob(dir, r.Body, maxCreativeArtifactBytes)
	if err != nil {
		switch {
		case errors.Is(err, errCreativeArtifactTooLarge):
			writeErr(w, 413, "too_large", err.Error())
		case errors.Is(err, errCreativeArtifactTransfer):
			writeErr(w, 400, "transfer_failed", err.Error())
		default:
			creativeArtifactError(w, err)
		}
		return
	}
	// An advertised body length that cannot be read fully is a failed transfer,
	// including direct handler callers whose reader does not enforce HTTP framing.
	if r.ContentLength >= 0 && r.ContentLength != size {
		_ = os.Remove(filepath.Join(dir, key))
		writeErr(w, 400, "transfer_failed", "artifact body length does not match Content-Length")
		return
	}
	p.StorageKey = key
	p.Size = size
	p.SHA256 = hash
	a, v, err := d.Store.PublishCreativeArtifact(p)
	if err != nil {
		_ = os.Remove(filepath.Join(dir, key))
		creativeArtifactError(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"artifact": a, "version": creativeVersionJSON(v)})
}

var errCreativeArtifactTransfer = errors.New("artifact transfer failed")

// writeCreativeArtifactBlob publishes no metadata. Its caller owns removal on a
// failed metadata transaction. The limit is an argument so this primitive can be
// verified with a small stream rather than allocating multi-gigabyte fixtures.
func writeCreativeArtifactBlob(dir string, body io.Reader, limit int64) (key string, size int64, hash string, err error) {
	if err = os.MkdirAll(dir, 0700); err != nil {
		return
	}
	f, e := os.CreateTemp(dir, ".upload-")
	if e != nil {
		err = e
		return
	}
	path := f.Name()
	committed := false
	defer func() {
		_ = f.Close()
		if !committed {
			_ = os.Remove(path)
		}
	}()
	h := sha256.New()
	size, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(body, limit+1))
	if err != nil {
		// PathError identifies local disk failure; other errors originate in the transfer.
		var pe *os.PathError
		if !errors.As(err, &pe) {
			err = fmt.Errorf("%w: %v", errCreativeArtifactTransfer, err)
		}
		return
	}
	if size > limit {
		err = errCreativeArtifactTooLarge
		return
	}
	if size == 0 {
		err = fmt.Errorf("%w: artifact body must not be empty", errCreativeArtifactTransfer)
		return
	}
	if err = f.Sync(); err != nil {
		return
	}
	if err = f.Close(); err != nil {
		return
	}
	key = "blob-" + strings.TrimPrefix(filepath.Base(path), ".upload-")
	final := filepath.Join(dir, key)
	if err = os.Rename(path, final); err != nil {
		return
	}
	path = final
	// Make the rename durable before any database row can refer to these bytes.
	directory, e := os.Open(dir)
	if e != nil {
		err = e
		return
	}
	err = directory.Sync()
	closeErr := directory.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return
	}
	hash = hex.EncodeToString(h.Sum(nil))
	committed = true
	return
}

func handleCreativeArtifactContent(w http.ResponseWriter, r *http.Request, d Deps) {
	v, err := creativeArtifactVersionFromRequest(r, d)
	if creativeArtifactError(w, err) {
		return
	}
	f, err := os.Open(filepath.Join(d.Cfg.Home, "creative-artifacts", v.StorageKey))
	if err != nil {
		creativeArtifactError(w, fmt.Errorf("published content unavailable: %w", err))
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		creativeArtifactError(w, err)
		return
	}
	if !info.Mode().IsRegular() || info.Size() != v.Size {
		creativeArtifactError(w, errors.New("published content size mismatch"))
		return
	}
	disposition := "attachment"
	if creativeArtifactInlineMIME(v.MIME) && r.URL.Query().Get("download") != "1" {
		disposition = "inline"
	}
	contentType := v.MIME
	if contentType == "text/markdown" {
		contentType = "text/plain; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": v.Filename}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("ETag", `"`+v.SHA256+`"`)
	http.ServeContent(w, r, v.Filename, time.Unix(v.CreatedAt, 0), f)
}
func creativeArtifactInlineMIME(typ string) bool {
	switch typ {
	case "text/plain", "text/markdown", "image/png", "image/jpeg", "image/webp", "image/gif", "image/avif", "image/bmp":
		return true
	}
	return strings.HasPrefix(typ, "audio/") || strings.HasPrefix(typ, "video/")
}
