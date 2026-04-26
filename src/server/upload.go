package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

var errInvalidUploadFilename = errors.New("有効なファイル名が必要です")

// uploadHandler は単一または複数のファイルをストリーミングアップロードします。
func (s *Server) uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{Success: false, Message: "POST メソッドのみ許可されています"})
		return
	}

	uploadDirectory := normalizeDirectoryKey(r.URL.Query().Get("path"))

	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Content-Type は multipart/form-data である必要があります"})
		return
	}

	boundary := params["boundary"]
	if boundary == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "multipart boundary が見つかりません"})
		return
	}

	reader := multipart.NewReader(r.Body, boundary)
	uploadedFiles := make([]UploadFile, 0)

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "マルチパートの解析に失敗しました"})
			return
		}

		if part.FileName() == "" || !isUploadField(part.FormName()) {
			_ = part.Close()
			continue
		}

		uploadedFile, err := s.uploadMultipartFile(r.Context(), part, uploadDirectory)
		closeErr := part.Close()
		if closeErr != nil {
			log.Printf("マルチパート close エラー [%s]: %v", part.FileName(), closeErr)
		}
		if err != nil {
			if errors.Is(err, errInvalidUploadFilename) {
				writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
				return
			}

			log.Printf("アップロードエラー [%s]: %v", part.FileName(), err)
			writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "アップロードに失敗しました"})
			return
		}

		uploadedFiles = append(uploadedFiles, uploadedFile)
	}

	if len(uploadedFiles) == 0 {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "file または files フィールドが必要です"})
		return
	}

	message := "アップロードが完了しました"
	if len(uploadedFiles) > 1 {
		message = fmt.Sprintf("%d 件のアップロードが完了しました", len(uploadedFiles))
	}

	writeJSON(w, http.StatusCreated, UploadResponse{
		Success: true,
		Message: message,
		Files:   uploadedFiles,
	})
}

func (s *Server) uploadMultipartFile(ctx context.Context, part *multipart.Part, uploadDirectory string) (UploadFile, error) {
	filename := normalizeUploadFileName(part.FileName())
	if filename == "" {
		return UploadFile{}, errInvalidUploadFilename
	}

	key := buildUploadKey(uploadDirectory, filename)

	contentType := part.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	s.semaphore <- struct{}{}
	defer func() { <-s.semaphore }()

	_, err := s.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        part,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return UploadFile{}, err
	}

	return UploadFile{
		Name:        filename,
		Key:         key,
		URL:         s.objectURL(key),
		ContentType: contentType,
	}, nil
}
