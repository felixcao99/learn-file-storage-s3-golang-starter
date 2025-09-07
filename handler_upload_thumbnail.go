package main

import (
	// "encoding/base64"
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

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	// TODO: implement the upload here
	const maxMemory = 10 << 20
	r.ParseMultipartForm(maxMemory)

	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	contenttype := header.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contenttype)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Can't parse media type", err)
	}
	if mediaType != "image/jpeg" && mediaType != "image/png" {
		respondWithError(w, http.StatusBadRequest, "Thumbnail must be a JPEG or PNG image", nil)
		return
	}
	ext := strings.Split(mediaType, "/")[1]
	wd, _ := os.Getwd()

	filetoken := make([]byte, 32)
	_, err = rand.Read(filetoken)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Can't generate thumbnail file name", err)
		return
	}
	filename := base64.RawURLEncoding.EncodeToString(filetoken)
	filepath := filepath.Join(wd, cfg.assetsRoot, fmt.Sprintf("%s.%s", filename, ext))

	fmt.Println("creating file at", filepath)

	thumbfile, err := os.Create(filepath)

	if err != nil {
		fmt.Println("Error happened when creating file", err)
		respondWithError(w, http.StatusInternalServerError, "Unable to create file", err)
		return
	}
	defer thumbfile.Close()

	fmt.Println("copying file...")
	_, err = io.Copy(thumbfile, file)
	if err != nil {
		fmt.Println("Error happened when copying file", err)
		respondWithError(w, http.StatusBadRequest, "Unable to create file", err)
		return
	}

	thumbfileurl := fmt.Sprintf("http://localhost:%s/assets/%s.%s", cfg.port, filename, ext)

	// imagedata, err := io.ReadAll(file)
	// if err != nil {
	// 	respondWithError(w, http.StatusInternalServerError, "Unable to read file", err)
	// 	return
	// }

	// imagebase64 := base64.StdEncoding.EncodeToString(imagedata)
	// imagebase64url := fmt.Sprintf("data:%s;base64,%s", mediaType, imagebase64)

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to get video", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "You don't have permission to upload a thumbnail for this video", nil)
		return
	}

	// thumbnail := thumbnail{
	// 	data:      imagedata,
	// 	mediaType: mediaType,
	// }

	// videoThumbnails[videoID] = thumbnail
	// tburl := fmt.Sprintf("http://localhost:8091/api/thumbnails/%s", videoID.String())
	// video.ThumbnailURL = &tburl
	// video.ThumbnailURL = &imagebase64url
	video.ThumbnailURL = &thumbfileurl

	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}
