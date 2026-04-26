package server

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Server はS3クライアントとアップロード設定を保持します。
type Server struct {
	s3Client      *s3.Client
	uploader      *manager.Uploader
	bucket        string
	endpoint      string
	publicBaseURL string
	semaphore     chan struct{}
	authEnabled   bool
	authKey       string
}

// New は環境変数からS3接続設定を読み込み Server を初期化します。
func New() (*Server, error) {
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

	maxConcurrent := 10
	if value := os.Getenv("MAX_CONCURRENT_UPLOADS"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			maxConcurrent = parsed
		}
	}

	partSizeMB := 32
	if value := os.Getenv("UPLOAD_PART_SIZE_MB"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed >= 5 {
			partSizeMB = parsed
		}
	}

	concurrency := 8
	if value := os.Getenv("UPLOAD_CONCURRENCY"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			concurrency = parsed
		}
	}

	maxConns := concurrency * maxConcurrent
	httpClient := awshttp.NewBuildableClient().WithTransportOptions(func(transport *http.Transport) {
		transport.MaxIdleConns = maxConns
		transport.MaxIdleConnsPerHost = maxConns
		transport.MaxConnsPerHost = maxConns
	})

	config := aws.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		HTTPClient:  httpClient,
	}

	client := s3.NewFromConfig(config, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		options.UsePathStyle = true
	})

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

	publicBaseURL := os.Getenv("PUBLIC_BASE_URL")

	return &Server{
		s3Client:      client,
		uploader:      uploader,
		bucket:        bucket,
		endpoint:      endpoint,
		publicBaseURL: publicBaseURL,
		semaphore:     make(chan struct{}, maxConcurrent),
		authEnabled:   authEnabled,
		authKey:       authKey,
	}, nil
}

// RegisterRoutes は API ルートを mux に登録します。
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/upload", s.authMiddleware(s.uploadHandler))
	mux.HandleFunc("GET /api/v1/files", s.authMiddleware(s.listFilesHandler))
	mux.HandleFunc("DELETE /api/v1/files/{key...}", s.authMiddleware(s.deleteFileHandler))
}

// MaxConcurrentUploads は同時アップロード上限を返します。
func (s *Server) MaxConcurrentUploads() int {
	return cap(s.semaphore)
}
