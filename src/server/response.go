package server

import (
	"encoding/json"
	"log"
	"net/http"
)

// APIResponse は汎用の JSON レスポンスです。
type APIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// UploadFile はアップロードされたファイル情報です。
type UploadFile struct {
	Name        string `json:"name"`
	Key         string `json:"key"`
	URL         string `json:"url"`
	ContentType string `json:"contentType,omitempty"`
}

// UploadResponse はアップロード結果の JSON 構造体です。
type UploadResponse struct {
	Success bool         `json:"success"`
	Message string       `json:"message,omitempty"`
	Files   []UploadFile `json:"files"`
}

// FileInfo は一覧レスポンスの各エントリです。
type FileInfo struct {
	Key          string `json:"key"`
	Name         string `json:"name"`
	URL          string `json:"url,omitempty"`
	Size         int64  `json:"size"`
	LastModified string `json:"lastModified,omitempty"`
	IsDirectory  bool   `json:"isDirectory"`
}

// FileListResponse はファイル一覧の JSON 構造体です。
type FileListResponse struct {
	Success bool       `json:"success"`
	Files   []FileInfo `json:"files"`
}

// DeleteResponse は削除結果の JSON 構造体です。
type DeleteResponse struct {
	Success      bool   `json:"success"`
	Message      string `json:"message,omitempty"`
	Target       string `json:"target"`
	Kind         string `json:"kind"`
	DeletedCount int    `json:"deletedCount"`
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("レスポンスエンコードエラー: %v", err)
	}
}
