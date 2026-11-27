package main

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
)

func (a app) canAccessReport(w http.ResponseWriter, r *http.Request) bool {
	var owner string
	if a.db.QueryRow(r.Context(), `SELECT reporter_id FROM damage_reports WHERE id=$1`, chi.URLParam(r, "id")).Scan(&owner) != nil {
		fail(w, 404, "Laporan tidak ditemukan")
		return false
	}
	if who(r).Role == "reporter" && owner != who(r).ID {
		fail(w, 403, "Akses ditolak")
		return false
	}
	return true
}
func (a app) uploadReportPhoto(w http.ResponseWriter, r *http.Request) {
	if !a.canAccessReport(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	file, header, err := r.FormFile("file")
	if err != nil {
		fail(w, 422, "Foto JPEG/PNG maksimal 8 MB diperlukan")
		return
	}
	defer file.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	mime := http.DetectContentType(head[:n])
	if mime != "image/jpeg" && mime != "image/png" {
		fail(w, 422, "Foto harus JPEG atau PNG")
		return
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		fail(w, 500, "Foto gagal dibaca")
		return
	}
	root := os.Getenv("FILE_STORAGE_DIR")
	if root == "" {
		root = "./private-files"
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		fail(w, 500, "Storage tidak tersedia")
		return
	}
	random := make([]byte, 24)
	if _, err = rand.Read(random); err != nil {
		fail(w, 500, "Gagal membuat kunci foto")
		return
	}
	key := hex.EncodeToString(random)
	target := filepath.Join(root, key)
	dest, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		fail(w, 500, "Gagal menyimpan foto")
		return
	}
	size, copyErr := io.Copy(dest, file)
	closeErr := dest.Close()
	if copyErr != nil || closeErr != nil || size == 0 {
		os.Remove(target)
		fail(w, 422, "Foto tidak valid")
		return
	}
	var id string
	err = a.db.QueryRow(r.Context(), `INSERT INTO report_photos(report_id,storage_key,filename,mime_type,size_bytes,uploaded_by) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, chi.URLParam(r, "id"), key, filepath.Base(header.Filename), mime, size, who(r).ID).Scan(&id)
	if err != nil {
		os.Remove(target)
		fail(w, 500, "Foto gagal dicatat")
		return
	}
	respond(w, 201, map[string]string{"id": id})
}
func (a app) listReportPhotos(w http.ResponseWriter, r *http.Request) {
	if !a.canAccessReport(w, r) {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT id,filename,mime_type FROM report_photos WHERE report_id=$1 ORDER BY created_at`, chi.URLParam(r, "id"))
	if err != nil {
		fail(w, 500, "Foto gagal dimuat")
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var id, name, mime string
		if rows.Scan(&id, &name, &mime) == nil {
			items = append(items, map[string]string{"id": id, "filename": name, "mime_type": mime})
		}
	}
	respond(w, 200, items)
}
func (a app) downloadReportPhoto(w http.ResponseWriter, r *http.Request) {
	if !a.canAccessReport(w, r) {
		return
	}
	var key, mime string
	if a.db.QueryRow(r.Context(), `SELECT storage_key,mime_type FROM report_photos WHERE id=$1 AND report_id=$2`, chi.URLParam(r, "photoId"), chi.URLParam(r, "id")).Scan(&key, &mime) != nil {
		fail(w, 404, "Foto tidak ditemukan")
		return
	}
	root := os.Getenv("FILE_STORAGE_DIR")
	if root == "" {
		root = "./private-files"
	}
	file, err := os.Open(filepath.Join(root, key))
	if err != nil {
		fail(w, 404, "Foto tidak tersedia")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, file)
}
