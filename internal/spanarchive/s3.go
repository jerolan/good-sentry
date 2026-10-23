package spanarchive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Objects struct {
	client *s3.Client
	bucket string
}

func NewS3Objects(ctx context.Context, region, bucket, endpoint string) (*S3Objects, error) {
	if strings.TrimSpace(region) == "" || strings.TrimSpace(bucket) == "" {
		return nil, errors.New("span archive S3 bucket and region are required")
	}
	awsConfig, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load AWS SDK configuration: %w", err)
	}
	client := s3.NewFromConfig(awsConfig, func(options *s3.Options) {
		if endpoint != "" {
			options.BaseEndpoint = aws.String(endpoint)
			options.UsePathStyle = true
		}
	})
	return &S3Objects{client: client, bucket: bucket}, nil
}

func (s *S3Objects) Put(ctx context.Context, key string, data []byte) error {
	clean, err := cleanKey(key)
	if err != nil {
		return err
	}
	if len(data) == 0 || len(data) > MaxObjectBytes {
		return errors.New("span archive object size must be between 1 byte and 64 MiB")
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(clean),
		Body:          bytes.NewReader(data),
		ContentLength: aws.Int64(int64(len(data))),
		ContentType:   aws.String("application/vnd.apache.parquet"),
		IfNoneMatch:   aws.String("*"),
	})
	if err != nil {
		// Archive keys are immutable. A retry may find the complete object from an
		// earlier upload whose manifest transaction did not commit; accept that
		// only when the stored bytes are identical.
		current, getErr := s.Get(ctx, clean)
		if getErr == nil {
			if bytes.Equal(current, data) {
				return nil
			}
			return errors.New("span archive key already exists with different contents")
		}
		return fmt.Errorf("upload span archive object: %w", err)
	}
	return nil
}

func (s *S3Objects) Get(ctx context.Context, key string) ([]byte, error) {
	clean, err := cleanKey(key)
	if err != nil {
		return nil, err
	}
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(clean)})
	if err != nil {
		return nil, fmt.Errorf("download span archive object: %w", err)
	}
	defer result.Body.Close()
	data, err := io.ReadAll(io.LimitReader(result.Body, MaxObjectBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read span archive object response: %w", err)
	}
	if len(data) == 0 || len(data) > MaxObjectBytes {
		return nil, errors.New("span archive object size must be between 1 byte and 64 MiB")
	}
	return data, nil
}

func (s *S3Objects) Delete(ctx context.Context, key string) error {
	clean, err := cleanKey(key)
	if err != nil {
		return err
	}
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(clean)}); err != nil {
		return fmt.Errorf("delete span archive object: %w", err)
	}
	return nil
}

// PrefixedKey returns the exact key persisted in the manifest. Persisting the
// prefix with each object identity keeps later reads independent of config edits.
func PrefixedKey(prefix, key string) (string, error) {
	clean, err := cleanKey(key)
	if err != nil {
		return "", err
	}
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		return clean, nil
	}
	cleanPrefix, err := cleanKey(prefix)
	if err != nil {
		return "", fmt.Errorf("invalid span archive S3 prefix: %w", err)
	}
	return cleanPrefix + "/" + clean, nil
}
