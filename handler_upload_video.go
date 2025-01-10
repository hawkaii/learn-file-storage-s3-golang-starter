package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func getVideoAspectRatio(filePath string) (string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)

	// Create a buffer for the output
	var outb bytes.Buffer
	cmd.Stdout = &outb

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("error running ffprobe: %v", err)
	}

	var jsonData map[string]interface{}
	if err := json.Unmarshal(outb.Bytes(), &jsonData); err != nil {
		return "", fmt.Errorf("error parsing JSON: %v", err)
	}

	streams, ok := jsonData["streams"].([]interface{})
	if !ok || len(streams) == 0 {
		return "", fmt.Errorf("no streams found in video")
	}

	stream, ok := streams[0].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid stream data")
	}

	width, ok1 := stream["width"].(float64)
	height, ok2 := stream["height"].(float64)
	if !ok1 || !ok2 {
		return "", fmt.Errorf("could not get dimensions")
	}

	ratio := width / height
	// Use a small tolerance for floating-point comparison
	const tolerance = 0.1
	if math.Abs(ratio-16.0/9.0) < tolerance {
		return "16:9", nil
	}
	if math.Abs(ratio-9.0/16.0) < tolerance {
		return "9:16", nil
	}
	return "other", nil
}

func processVideoForFastStart(filePath string) (string, error) {
	outputPath := filePath + ".tmp.mp4"
	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", outputPath)

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("error running ffmpeg: %v", err)
	}

	return outputPath, nil
}

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {

	http.MaxBytesReader(w, r.Body, 1<<30)

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}
	userId, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	dbVideo, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't get video", err)
		return
	}

	if dbVideo.UserID != userId {
		respondWithError(w, http.StatusUnauthorized, "Not authorized", nil)
		return
	}

	video, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't get video", err)
		return
	}

	defer video.Close()

	// Check the file type
	contentType := header.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "video/mp4" {
		respondWithError(w, http.StatusUnsupportedMediaType, "Unsupported media type", err)
		return
	}

	// Upload the video to S3
	tmpl, err := os.CreateTemp("", "tubely-mms.mp4")
	fmt.Println("Created temp file:", tmpl.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create temp file", err)
		return
	}

	defer os.Remove(tmpl.Name())
	defer tmpl.Close()

	_, err = io.Copy(tmpl, video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't get video", err)
		return
	}

	// Process the video for fast start
	processedPath, err := processVideoForFastStart(tmpl.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't process video", err)
		return
	}

	defer os.Remove(processedPath)

	tmpl, err = os.Open(processedPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't open processed video", err)
		return
	}

	// Get the aspect ratio of the video
	aspectRatio, err := getVideoAspectRatio(processedPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't get video aspect ratio", err)
		return
	}

	fmt.Println("Aspect ratio:", aspectRatio)

	tmpl.Seek(0, io.SeekStart)

	prefix := ""
	switch aspectRatio {
	case "16:9":
		prefix = "landscape/"
	case "9:16":
		prefix = "portrait/"
	default:
		prefix = "other/"
	}

	s3key := prefix + fmt.Sprintf("%s.mp4", uuid.New().String())
	input := &s3.PutObjectInput{
		Bucket:      aws.String(cfg.s3Bucket),
		Key:         aws.String(s3key),
		Body:        tmpl,
		ContentType: aws.String("video/mp4"),
	}
	ctx := context.Background()
	_, err = cfg.s3Client.PutObject(ctx, input)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't upload video", err)
		return
	}
	// First, create the bucket,key format URL
	videoURL := cfg.s3CfDistribution + fmt.Sprintf("/%s", s3key)
	fmt.Println("Video URL:", videoURL)

	// Store the bucket,key format in dbVideo
	dbVideo.VideoURL = &videoURL

	// Save the bucket,key format to the database
	err = cfg.db.UpdateVideo(dbVideo)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video", err)
		return
	}

	fmt.Println("Video uploaded to S3:", videoURL)

	respondWithJSON(w, http.StatusOK, dbVideo)

}
