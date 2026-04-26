package server

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const deleteBatchSize = 1000

func (s *Server) objectURL(key string) string {
	if s.publicBaseURL != "" {
		return fmt.Sprintf("%s/%s", normalizePublicBaseURL(s.publicBaseURL), strings.TrimLeft(key, "/"))
	}
	return fmt.Sprintf("%s/%s/%s", strings.TrimRight(s.endpoint, "/"), s.bucket, strings.TrimLeft(key, "/"))
}

func normalizePublicBaseURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	return strings.TrimRight(trimmed, "/")
}

func normalizeObjectKey(raw string) string {
	normalized := strings.ReplaceAll(raw, "\\", "/")
	for strings.HasPrefix(normalized, "./") {
		normalized = strings.TrimPrefix(normalized, "./")
	}
	normalized = strings.TrimLeft(normalized, "/")

	cleaned := strings.TrimPrefix(path.Clean("/"+normalized), "/")
	if cleaned == "." || cleaned == "" {
		return ""
	}

	return cleaned
}

func normalizeDirectoryKey(raw string) string {
	key := normalizeObjectKey(raw)
	if key == "" {
		return ""
	}
	if !strings.HasSuffix(key, "/") {
		key += "/"
	}
	return key
}

func buildUploadKey(directory string, filename string) string {
	if directory == "" {
		return filename
	}
	return directory + filename
}

func normalizeUploadFileName(filename string) string {
	name := path.Base(strings.ReplaceAll(filename, "\\", "/"))
	if name == "" || name == "." || name == "/" {
		return ""
	}
	return name
}

func objectName(key string) string {
	trimmed := strings.TrimSuffix(key, "/")
	if trimmed == "" {
		return ""
	}
	return path.Base(trimmed)
}

func parentDirectoryKeys(key string) []string {
	trimmed := strings.TrimSuffix(key, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) <= 1 {
		return nil
	}

	directories := make([]string, 0, len(parts)-1)
	for index := 1; index < len(parts); index++ {
		directories = append(directories, strings.Join(parts[:index], "/")+"/")
	}

	return directories
}

func isUploadField(name string) bool {
	switch name {
	case "file", "files", "files[]":
		return true
	default:
		return false
	}
}

func (s *Server) prefixHasObjects(ctx context.Context, prefix string) (bool, error) {
	output, err := s.s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:  aws.String(s.bucket),
		Prefix:  aws.String(prefix),
		MaxKeys: aws.Int32(1),
	})
	if err != nil {
		return false, err
	}
	return len(output.Contents) > 0, nil
}

func (s *Server) deletePrefix(ctx context.Context, prefix string) (int, error) {
	paginator := s3.NewListObjectsV2Paginator(s.s3Client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})

	deletedCount := 0
	batch := make([]types.ObjectIdentifier, 0, deleteBatchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}

		_, err := s.s3Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(s.bucket),
			Delete: &types.Delete{
				Objects: append([]types.ObjectIdentifier(nil), batch...),
				Quiet:   aws.Bool(true),
			},
		})
		if err != nil {
			return err
		}

		deletedCount += len(batch)
		batch = batch[:0]
		return nil
	}

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return deletedCount, err
		}

		for _, object := range page.Contents {
			if object.Key == nil || *object.Key == "" {
				continue
			}

			batch = append(batch, types.ObjectIdentifier{Key: object.Key})
			if len(batch) == deleteBatchSize {
				if err := flush(); err != nil {
					return deletedCount, err
				}
			}
		}
	}

	if err := flush(); err != nil {
		return deletedCount, err
	}

	return deletedCount, nil
}
