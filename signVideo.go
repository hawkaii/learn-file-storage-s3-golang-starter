package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
)

func generatePresignedURL(s3Client *s3.Client, bucket string, key string, expireTime time.Duration) (string, error) {
	if bucket == "" || key == "" {
		return "", fmt.Errorf("bucket or key is empty: bucket=%s, key=%s", bucket, key)
	}

	presignClient := s3.NewPresignClient(s3Client)
	ctx := context.Background()

	presignOptions, err := presignClient.PresignGetObject(ctx,
		&s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		}, func(options *s3.PresignOptions) {
			options.Expires = expireTime
		})

	if err != nil {
		return "", fmt.Errorf("failed to presign: %w", err)
	}

	if presignOptions == nil || presignOptions.URL == "" {
		return "", fmt.Errorf("presign options or URL is nil")
	}

	return presignOptions.URL, nil
}
func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	signedVideo := video

	if video.VideoURL == nil {
		return signedVideo, fmt.Errorf("video URL is nil")
	}

	urlParts := strings.Split(*video.VideoURL, ",")
	if len(urlParts) != 2 {
		return signedVideo, fmt.Errorf("invalid video URL format: %s", *video.VideoURL)
	}

	bucket := strings.TrimSpace(urlParts[0])
	key := strings.TrimSpace(urlParts[1])

	if bucket == "" || key == "" {
		return signedVideo, fmt.Errorf("empty bucket or key after split: bucket=%s, key=%s", bucket, key)
	}

	fmt.Printf("Generating signed URL for bucket: %s, key: %s\n", bucket, key)

	signedURL, err := generatePresignedURL(cfg.s3Client, bucket, key, 15*time.Minute)
	if err != nil {
		return signedVideo, fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	if signedURL == "" {
		return signedVideo, fmt.Errorf("generated presigned URL is empty")
	}

	signedVideo.VideoURL = &signedURL
	return signedVideo, nil
}
