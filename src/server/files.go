package server

import (
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// listFilesHandler はバケット内のファイル一覧を JSON で返します。
func (s *Server) listFilesHandler(w http.ResponseWriter, r *http.Request) {
	paginator := s3.NewListObjectsV2Paginator(s.s3Client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
	})

	files := make([]FileInfo, 0)
	directories := make(map[string]FileInfo)

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(r.Context())
		if err != nil {
			log.Printf("一覧取得エラー: %v", err)
			writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "ファイル一覧の取得に失敗しました"})
			return
		}

		for _, object := range page.Contents {
			if object.Key == nil || *object.Key == "" {
				continue
			}

			key := *object.Key
			if strings.HasSuffix(key, "/") {
				directories[key] = FileInfo{
					Key:         key,
					Name:        objectName(key),
					IsDirectory: true,
				}
				continue
			}

			file := FileInfo{
				Key:         key,
				Name:        objectName(key),
				URL:         s.objectURL(key),
				IsDirectory: false,
			}
			if object.Size != nil {
				file.Size = *object.Size
			}
			if object.LastModified != nil {
				file.LastModified = object.LastModified.Format(time.RFC3339)
			}
			files = append(files, file)

			for _, dirKey := range parentDirectoryKeys(key) {
				if _, exists := directories[dirKey]; exists {
					continue
				}

				directories[dirKey] = FileInfo{
					Key:         dirKey,
					Name:        objectName(dirKey),
					IsDirectory: true,
				}
			}
		}
	}

	entries := make([]FileInfo, 0, len(directories)+len(files))
	for _, dir := range directories {
		entries = append(entries, dir)
	}
	entries = append(entries, files...)

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDirectory != entries[j].IsDirectory {
			return entries[i].IsDirectory && !entries[j].IsDirectory
		}
		return entries[i].Key < entries[j].Key
	})

	writeJSON(w, http.StatusOK, FileListResponse{
		Success: true,
		Files:   entries,
	})
}

// deleteFileHandler はファイルまたはディレクトリを削除します。
func (s *Server) deleteFileHandler(w http.ResponseWriter, r *http.Request) {
	rawKey, err := url.PathUnescape(r.PathValue("key"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "削除対象の key が不正です"})
		return
	}

	cleanedRawKey := strings.ReplaceAll(rawKey, "\\", "/")
	key := normalizeObjectKey(cleanedRawKey)
	if key == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "削除対象の key が必要です"})
		return
	}

	if strings.HasSuffix(cleanedRawKey, "/") {
		s.deleteDirectory(w, r, normalizeDirectoryKey(cleanedRawKey))
		return
	}

	directoryKey := normalizeDirectoryKey(cleanedRawKey)
	hasChildren, err := s.prefixHasObjects(r.Context(), directoryKey)
	if err != nil {
		log.Printf("ディレクトリ確認エラー [%s]: %v", directoryKey, err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "削除対象の確認に失敗しました"})
		return
	}
	if hasChildren {
		s.deleteDirectory(w, r, directoryKey)
		return
	}

	_, err = s.s3Client.DeleteObject(r.Context(), &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		log.Printf("削除エラー [%s]: %v", key, err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "ファイルの削除に失敗しました"})
		return
	}

	writeJSON(w, http.StatusOK, DeleteResponse{
		Success:      true,
		Message:      "ファイルを削除しました",
		Target:       key,
		Kind:         "file",
		DeletedCount: 1,
	})
}

func (s *Server) deleteDirectory(w http.ResponseWriter, r *http.Request, directoryKey string) {
	if directoryKey == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "削除対象のディレクトリが不正です"})
		return
	}

	deletedCount, err := s.deletePrefix(r.Context(), directoryKey)
	if err != nil {
		log.Printf("ディレクトリ削除エラー [%s]: %v", directoryKey, err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "ディレクトリの削除に失敗しました"})
		return
	}

	writeJSON(w, http.StatusOK, DeleteResponse{
		Success:      true,
		Message:      "ディレクトリを削除しました",
		Target:       directoryKey,
		Kind:         "directory",
		DeletedCount: deletedCount,
	})
}
