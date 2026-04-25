package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/joho/godotenv"
)

// Server はS3アップローダーとバケット設定を保持します
type Server struct {
	uploader    *manager.Uploader
	bucket      string
	endpoint    string
	semaphore   chan struct{} // 同時アップロード上限
	authEnabled bool
	authKey     string
}

// Response はAPIレスポンスのJSON構造体です
type Response struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	URL     string `json:"url,omitempty"`
	Key     string `json:"key,omitempty"`
}

// newServer は環境変数からS3接続設定を読み込みServerを初期化します
func newServer() (*Server, error) {
	accessKey := os.Getenv("S3_ACCESS_KEY")
	secretKey := os.Getenv("S3_SECRET_KEY")
	region := os.Getenv("S3_REGION")
	endpoint := os.Getenv("S3_ENDPOINT")
	bucket := os.Getenv("S3_BUCKET")

	if accessKey == "" || secretKey == "" {
		return nil, fmt.Errorf("S3_ACCESS_KEY と S3_SECRET_KEY は必須です")
	}
	if bucket == "" {
		return nil, fmt.Errorf("S3_BUCKET は必須です")
	}
	if region == "" {
		region = "us-east-1"
	}
	if endpoint == "" {
		return nil, fmt.Errorf("S3_ENDPOINT は必須です")
	}

	// 同時アップロード上限 (デフォルト10、MAX_CONCURRENT_UPLOADS で変更可)
	maxConcurrent := 10
	if v := os.Getenv("MAX_CONCURRENT_UPLOADS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxConcurrent = n
		}
	}

	// 1パートのサイズ MB (デフォルト32MB、UPLOAD_PART_SIZE_MB で変更可)
	// 大きいほど往復回数が減り高速になるがメモリ使用量が増加する
	// メモリ消費の目安: partSizeMB × concurrency × maxConcurrent
	partSizeMB := 32
	if v := os.Getenv("UPLOAD_PART_SIZE_MB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 5 {
			partSizeMB = n
		}
	}

	// 1ファイルあたりの並列パート数 (デフォルト8、UPLOAD_CONCURRENCY で変更可)
	concurrency := 8
	if v := os.Getenv("UPLOAD_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			concurrency = n
		}
	}

	// HTTP接続プール: パートの並列数 × 同時アップロード数に合わせて拡大
	maxConns := concurrency * maxConcurrent
	httpClient := awshttp.NewBuildableClient().WithTransportOptions(func(t *http.Transport) {
		t.MaxIdleConns = maxConns
		t.MaxIdleConnsPerHost = maxConns
		t.MaxConnsPerHost = maxConns
	})

	cfg := aws.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		HTTPClient:  httpClient,
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})

	// マルチパートアップロード設定
	uploader := manager.NewUploader(client, func(u *manager.Uploader) {
		u.PartSize = int64(partSizeMB) * 1024 * 1024
		u.Concurrency = concurrency
	})

	log.Printf("アップローダー設定: PartSize=%dMB Concurrency=%d MaxConcurrent=%d (接続プール=%d)",
		partSizeMB, concurrency, maxConcurrent, maxConns)

	authEnabled := os.Getenv("AUTH_ENABLED") == "true"
	authKey := os.Getenv("AUTH_KEY")
	if authEnabled && authKey == "" {
		return nil, fmt.Errorf("AUTH_ENABLED=true の場合、AUTH_KEY は必須です")
	}

	return &Server{
		uploader:    uploader,
		bucket:      bucket,
		endpoint:    endpoint,
		semaphore:   make(chan struct{}, maxConcurrent),
		authEnabled: authEnabled,
		authKey:     authKey,
	}, nil
}

// authMiddleware は X-Auth-Key ヘッダーを検証するミドルウェアです
// AUTH_ENABLED=true の場合のみ認証を行います
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.authEnabled {
			key := r.Header.Get("X-Auth-Key")
			if key == "" || key != s.authKey {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(Response{Success: false, Message: "認証に失敗しました"})
				return
			}
		}
		next(w, r)
	}
}

// uploadHandler は /api/v1/upload エンドポイントのハンドラです
// multipart/form-data の "file" フィールドを受け取り、
// ディスクに書かずに直接 Wasabi へマルチパートストリーミングアップロードします
func (s *Server) uploadHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(Response{Success: false, Message: "POST メソッドのみ許可されています"})
		return
	}

	// Content-Type から boundary を取得
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(Response{Success: false, Message: "Content-Type は multipart/form-data である必要があります"})
		return
	}
	boundary := params["boundary"]
	if boundary == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(Response{Success: false, Message: "multipart boundary が見つかりません"})
		return
	}

	// r.Body を直接パース（ディスク書き出しなし）
	mr := multipart.NewReader(r.Body, boundary)
	var (
		filePart        *multipart.Part
		filename        string
		partContentType string
	)
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(Response{Success: false, Message: "マルチパートの解析に失敗しました"})
			return
		}
		if part.FormName() == "file" {
			filePart = part
			filename = part.FileName()
			partContentType = part.Header.Get("Content-Type")
			break
		}
		part.Close()
	}

	if filePart == nil || filename == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(Response{Success: false, Message: "file フィールドが必要です"})
		return
	}
	defer filePart.Close()

	if partContentType == "" {
		partContentType = "application/octet-stream"
	}
	key := "uploads/" + filename

	// セマフォで同時アップロード数を制限
	s.semaphore <- struct{}{}
	defer func() { <-s.semaphore }()

	_, err = s.uploader.Upload(context.Background(), &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        filePart,
		ContentType: aws.String(partContentType),
	})
	if err != nil {
		log.Printf("アップロードエラー [%s]: %v", filename, err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(Response{Success: false, Message: "アップロードに失敗しました"})
		return
	}

	fileURL := fmt.Sprintf("%s/%s/%s", s.endpoint, s.bucket, key)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(Response{
		Success: true,
		Message: "アップロードが完了しました",
		URL:     fileURL,
		Key:     key,
	})
}

func main() {
	// バイナリと同じディレクトリの .env を読み込む（存在しない場合は無視）
	exe, err := os.Executable()
	if err == nil {
		envPath := filepath.Join(filepath.Dir(exe), ".env")
		_ = godotenv.Load(envPath)
	}

	srv, err := newServer()
	if err != nil {
		log.Fatalf("サーバーの初期化に失敗しました: %v", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/upload", srv.authMiddleware(srv.uploadHandler))
	mux.HandleFunc("/docs", docsHandler)

	// 大容量ファイルのアップロードに対応するため Read/Write タイムアウトは設定しない
	// ReadHeaderTimeout のみ設定してスローロリス攻撃を防ぐ
	httpSrv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
	}

	log.Printf("サーバーを起動しています: :%s (最大同時アップロード数: %d)", port, cap(srv.semaphore))
	if err := httpSrv.ListenAndServe(); err != nil {
		log.Fatalf("サーバーエラー: %v", err)
	}
}
