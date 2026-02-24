package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type Client struct {
	client   *s3.Client
	bucket   string
	endpoint string
}

func New(cfg *Config) (*Client, error) {
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, errors.New("S3 credentials not configured")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.TODO(),
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID,
			cfg.SecretAccessKey,
			"",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.Endpoint)
		o.UsePathStyle = true
	})

	return &Client{
		client:   client,
		bucket:   cfg.BucketName,
		endpoint: cfg.Endpoint,
	}, nil
}

func (c *Client) UploadFile(ctx context.Context, objectID string, key string, body io.Reader, contentType string) (string, error) {
	objectKey := fmt.Sprintf("%s/%s", objectID, key)
	_, err := c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(objectKey),
		Body:        body,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload file to S3: %w", err)
	}

	presignedURL, err := c.GetPresignedURL(ctx, objectKey, 15*time.Minute)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}
	return presignedURL, nil
}

func (c *Client) DeleteFile(ctx context.Context, key string) error {
	_, err := c.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to delete file from S3: %w", err)
	}
	return nil
}

func (c *Client) GetPresignedURL(ctx context.Context, key string, expiration time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(c.client)
	request, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = expiration
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}
	return request.URL, nil
}

func (c *Client) FileExists(ctx context.Context, key string) (bool, error) {
	_, err := c.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return false, nil
	}
	return true, nil
}

func (c *Client) FindKeyByPresignedURL(ctx context.Context, presignedURL string, prefix string) (string, error) {
	objects, err := c.client.ListObjects(ctx, &s3.ListObjectsInput{
		Bucket: aws.String(c.bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list objects: %w", err)
	}

	normalizedTarget := normalizeURL(presignedURL)
	for _, obj := range objects.Contents {
		objPresignedURL, err := c.GetPresignedURL(ctx, *obj.Key, 15*time.Minute)
		if err != nil {
			continue
		}
		if normalizeURL(objPresignedURL) == normalizedTarget {
			return *obj.Key, nil
		}
	}

	return "", fmt.Errorf("object not found for the given presigned URL")
}

func normalizeURL(url string) string {
	for i := 0; i < len(url); i++ {
		if url[i] == '?' || url[i] == '#' {
			return url[:i]
		}
	}
	return url
}

type presignedURLResult struct {
	index int
	url   string
	err   error
}

func (c *Client) GetObjects(ctx context.Context, prefix string) ([]string, error) {
	objects, err := c.client.ListObjects(ctx, &s3.ListObjectsInput{
		Bucket: aws.String(c.bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list objects: %w", err)
	}

	if len(objects.Contents) == 0 {
		return []string{}, nil
	}

	resultsChan := make(chan presignedURLResult, len(objects.Contents))
	var wg sync.WaitGroup

	for i, object := range objects.Contents {
		wg.Add(1)
		go func(idx int, obj types.Object) {
			defer wg.Done()
			presignedURL, err := c.GetPresignedURL(ctx, *obj.Key, 15*time.Minute)
			resultsChan <- presignedURLResult{
				index: idx,
				url:   presignedURL,
				err:   err,
			}
		}(i, object)
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	presignedURLs := make([]string, len(objects.Contents))
	errorCount := 0
	var firstError error

	for result := range resultsChan {
		if result.err != nil {
			errorCount++
			if firstError == nil {
				firstError = result.err
			}
			log.Printf("[go-s3 GetObjects] ERROR: Failed to get presigned URL for index %d: %v", result.index, result.err)
			continue
		}
		presignedURLs[result.index] = result.url
	}

	if errorCount == len(objects.Contents) {
		return nil, fmt.Errorf("failed to get presigned URLs: %w", firstError)
	}

	if errorCount > 0 {
		validURLs := make([]string, 0, len(presignedURLs))
		for _, url := range presignedURLs {
			if url != "" {
				validURLs = append(validURLs, url)
			}
		}
		log.Printf("[go-s3 GetObjects] WARNING: %d out of %d presigned URLs failed to generate", errorCount, len(objects.Contents))
		return validURLs, nil
	}

	return presignedURLs, nil
}

func (c *Client) Bucket() string   { return c.bucket }
func (c *Client) Endpoint() string { return c.endpoint }
