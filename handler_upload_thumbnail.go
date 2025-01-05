package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
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

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	const maxMemory = 10 << 20 // 10 MB
	r.ParseMultipartForm(maxMemory)

	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	mediaType := header.Header.Get("Content-Type")
	if mediaType == "" {
		respondWithError(w, http.StatusBadRequest, "Missing Content-Type for thumbnail", nil)
		return
	}

	// data, err := io.ReadAll(file)
	// if err != nil {
	// 	respondWithError(w, http.StatusInternalServerError, "Error reading file", err)
	// 	return
	// }

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't find video", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Not authorized to update this video", nil)
		return
	}

	// base64Encoded := base64.StdEncoding.EncodeToString(data)
	// base64DataURL := fmt.Sprintf("data:%s;base64,%s\n", mediaType, base64Encoded)

	parseMediaType, _, err := mime.ParseMediaType(mediaType)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't parse media type", err)
		return
	}

	if parseMediaType != "image/jpeg" && parseMediaType != "image/png" {
		respondWithError(w, http.StatusBadRequest, "Invalid media type", nil)
		return
	}

	extenstion := strings.Split(mediaType, "/")[1]
	byteSlice := make([]byte, 32)
	_, err = rand.Read(byteSlice)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't generate random bytes", err)
		return
	}

	basename := base64.RawURLEncoding.EncodeToString(byteSlice)

	assetsPath := fmt.Sprintf("/%s.%s", basename, extenstion)
	assetsPath = filepath.Join(cfg.assetsRoot, assetsPath)
	fmt.Printf("Writing thumbnail to %s\n", assetsPath)
	assetsFile, err := os.Create(assetsPath)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create file", err)
		return
	}

	defer assetsFile.Close()
	data, err := io.Copy(assetsFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't write file", err)
		return
	}

	fmt.Printf("Wrote %d bytes to %s\n", data, assetsPath)

	thumbnailURL := fmt.Sprintf("http://localhost:%s/%s", cfg.port, assetsPath)
	video.ThumbnailURL = &thumbnailURL
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}
