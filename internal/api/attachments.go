package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/store"
)

// attachmentExts maps the accepted upload MIME types to on-disk extensions.
// Anything else is rejected with 415 — attachments exist for pasted
// screenshots, not general file storage.
var attachmentExts = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
}

// maxAttachmentBytes bounds POST /v1/attachments bodies (10 MiB).
const maxAttachmentBytes = 10 << 20

func registerAttachmentRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("POST /v1/attachments", func(w http.ResponseWriter, r *http.Request) {
		handlePostAttachment(w, r, d)
	})
	mux.HandleFunc("GET /v1/attachments/{id}", func(w http.ResponseWriter, r *http.Request) {
		handleGetAttachment(w, r, d)
	})
}

// attachmentFilePath returns the absolute on-disk path for a: the row id
// plus the extension derived from its MIME type, under cfg.AttachmentsDir.
func attachmentFilePath(cfg *config.Config, a store.Attachment) string {
	return filepath.Join(cfg.AttachmentsDir, fmt.Sprintf("%d%s", a.ID, attachmentExts[a.MIME]))
}

// handlePostAttachment serves POST /v1/attachments: the request body IS the
// file (no multipart), typed by the Content-Type header. On success the
// bytes land in cfg.AttachmentsDir and the response is 201 {id, url}.
func handlePostAttachment(w http.ResponseWriter, r *http.Request, d Deps) {
	mime, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";")
	mime = strings.TrimSpace(mime)
	if _, ok := attachmentExts[mime]; !ok {
		writeErr(w, http.StatusUnsupportedMediaType, "unsupported_media_type",
			"only image/png, image/jpeg and image/webp are accepted")
		return
	}

	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAttachmentBytes))
	if err != nil {
		// The only expected failure here is the MaxBytesReader limit; treat
		// read errors uniformly as too-large rather than leaking internals.
		writeErr(w, http.StatusRequestEntityTooLarge, "too_large", "attachment exceeds 10 MB")
		return
	}
	if len(data) == 0 {
		writeErr(w, http.StatusBadRequest, "empty_body", "attachment body must not be empty")
		return
	}

	id, err := d.Store.AddAttachment(store.Attachment{MIME: mime, Size: int64(len(data))})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if err := os.MkdirAll(d.Cfg.AttachmentsDir, 0700); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	path := attachmentFilePath(d.Cfg, store.Attachment{ID: id, MIME: mime})
	if err := os.WriteFile(path, data, 0600); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":  id,
		"url": fmt.Sprintf("/v1/attachments/%d", id),
	})
}

// handleGetAttachment serves GET /v1/attachments/{id}. Content is immutable
// (an id is never rewritten), hence the aggressive cache header.
func handleGetAttachment(w http.ResponseWriter, r *http.Request, d Deps) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusNotFound, "attachment_not_found", "attachment not found")
		return
	}
	a, err := d.Store.GetAttachment(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "attachment_not_found", "attachment not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	w.Header().Set("Content-Type", a.MIME)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, attachmentFilePath(d.Cfg, a))
}

// attachmentLinkRe matches markdown image links pointing at the attachments
// API, as inserted by the dashboard's paste handler.
var attachmentLinkRe = regexp.MustCompile(`!\[[^\]]*\]\(/v1/attachments/(\d+)\)`)

// rewriteAttachmentLinks replaces dashboard attachment links in body with
// bracketed absolute file paths so the receiving agent can open the image
// from disk (agents get text injected into a TUI — a URL is useless there,
// a path is Read-able). Called at message-enqueue time only; question
// threads keep the original markdown for the web to render. Unknown ids
// pass through untouched.
func rewriteAttachmentLinks(d Deps, body string) string {
	return attachmentLinkRe.ReplaceAllStringFunc(body, func(link string) string {
		idStr := attachmentLinkRe.FindStringSubmatch(link)[1]
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			return link
		}
		a, err := d.Store.GetAttachment(id)
		if err != nil {
			return link
		}
		return "[screenshot: " + attachmentFilePath(d.Cfg, a) + "]"
	})
}
