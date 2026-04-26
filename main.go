package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	appserver "s3-upload/src/server"

	"github.com/joho/godotenv"
)

func main() {
	// バイナリと同じディレクトリの .env を読み込む（存在しない場合は無視）
	exe, err := os.Executable()
	if err == nil {
		envPath := filepath.Join(filepath.Dir(exe), ".env")
		_ = godotenv.Load(envPath)
	}

	srv, err := appserver.New()
	if err != nil {
		log.Fatalf("サーバーの初期化に失敗しました: %v", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	mux.HandleFunc("/docs", docsHandler)
	mux.HandleFunc("/openapi.json", openAPIHandler)

	// 大容量ファイルのアップロードに対応するため Read/Write タイムアウトは設定しない
	// ReadHeaderTimeout のみ設定してスローロリス攻撃を防ぐ
	httpSrv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
	}

	log.Printf("サーバーを起動しています: :%s (最大同時アップロード数: %d)", port, srv.MaxConcurrentUploads())
	if err := httpSrv.ListenAndServe(); err != nil {
		log.Fatalf("サーバーエラー: %v", err)
	}
}
