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

const (
	deletePathQuery     = "path"
	deleteKindFile      = "file"
	deleteKindDirectory = "directory"
)

type deleteTarget struct {
	Target string
	Kind   string
}

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

func parseDeletePathQuery(values url.Values) (string, string) {
	rawPath := strings.TrimSpace(values.Get(deletePathQuery))
	if rawPath == "" {
		return "", ""
	}

	cleanedRawPath := strings.ReplaceAll(rawPath, "\\", "/")
	if normalizeObjectKey(cleanedRawPath) == "" {
		return "", "削除対象の path クエリが不正です"
	}

	return cleanedRawPath, ""
}

func hasLegacyDeleteTarget(values url.Values, header http.Header) bool {
	return strings.TrimSpace(header.Get("Path")) != "" ||
		strings.TrimSpace(values.Get("key")) != "" ||
		strings.TrimSpace(values.Get("prefix")) != ""
}

// deleteFileQueryHandler は path クエリで指定した削除対象を削除します。
func (s *Server) deleteFileQueryHandler(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	queryPath, message := parseDeletePathQuery(values)
	if message != "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: message})
		return
	}

	legacyTargetSpecified := hasLegacyDeleteTarget(values, r.Header)
	if queryPath == "" {
		if legacyTargetSpecified {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "削除には path クエリを使ってください"})
			return
		}

		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "path クエリが必要です"})
		return
	}

	if legacyTargetSpecified {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "削除対象は path クエリだけを指定してください"})
		return
	}

	s.deleteAutoTarget(w, r, queryPath)
}

// deleteFileHandler はファイルまたはディレクトリを削除します。
func (s *Server) deleteFileHandler(w http.ResponseWriter, r *http.Request) {
	rawKey, err := url.PathUnescape(r.PathValue("key"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "削除対象の key が不正です"})
		return
	}

	s.deleteAutoTarget(w, r, strings.ReplaceAll(rawKey, "\\", "/"))
}

func (s *Server) deleteAutoTarget(w http.ResponseWriter, r *http.Request, cleanedRawKey string) {
	key := normalizeObjectKey(cleanedRawKey)
	if key == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "削除対象の key が必要です"})
		return
	}

	if strings.HasSuffix(cleanedRawKey, "/") {
		s.deleteTarget(w, r, deleteTarget{Target: normalizeDirectoryKey(cleanedRawKey), Kind: deleteKindDirectory})
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
		s.deleteTarget(w, r, deleteTarget{Target: directoryKey, Kind: deleteKindDirectory})
		return
	}

	s.deleteTarget(w, r, deleteTarget{Target: key, Kind: deleteKindFile})
}

func (s *Server) deleteTarget(w http.ResponseWriter, r *http.Request, target deleteTarget) {
	switch target.Kind {
	case deleteKindDirectory:
		s.deleteDirectory(w, r, target.Target)
	default:
		s.deleteObject(w, r, target.Target)
	}
}

func (s *Server) deleteObject(w http.ResponseWriter, r *http.Request, key string) {
	_, err := s.s3Client.DeleteObject(r.Context(), &s3.DeleteObjectInput{
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
		Kind:         deleteKindFile,
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
		Kind:         deleteKindDirectory,
		DeletedCount: deletedCount,
	})
}
