package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
	// "net/http"
	// "os"
	// "path/filepath"
	// "strings"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	//3.1
	const maxSize = 1 << 30
	r.Body = http.MaxBytesReader(w, r.Body, maxSize)
	//3.2
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}
	//3.3
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
	//3.4
	videoMeta, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't find video", err)
		return
	}
	if userID != videoMeta.UserID {
		respondWithError(w, http.StatusUnauthorized, "Not video owner", err)
		return
	}
	//3.5
	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()
	//3.6
	contenttype := header.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contenttype)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Can't parse media type", err)
	}
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Video must be a mp4 file", nil)
		return
	}
	//3.7
	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Can't create temp file", err)
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	_, err = io.Copy(tempFile, file)
	if err != nil {
		fmt.Println("Error happened when copying file", err)
		respondWithError(w, http.StatusBadRequest, "Unable to create file", err)
		return
	}

	//get video aspectratio
	// tempFilePath := os.TempDir() + "/tubely-upload.mp4"
	tempFilePath := tempFile.Name()
	// fmt.Printf("temp file path = %s\n", tempFilePath)
	// _, err = os.Stat(tempFilePath)
	// if errors.Is(err, os.ErrNotExist) {
	// 	fmt.Printf("temp file doesn't exist\n")
	// }

	aspectRatio, err := getVideoAspectRatio(tempFilePath)
	if err != nil {
		fmt.Println("Can't get aspect ratio", err)
		respondWithError(w, http.StatusBadRequest, "Unable to create file", err)
		return
	}
	fmt.Printf("aspect ratio = %s\n", aspectRatio)
	var objectPrefix string
	switch aspectRatio {
	case "16:9":
		objectPrefix = "landscape"
	case "9:16":
		objectPrefix = "portrait"
	default:
		objectPrefix = "other"
	}

	//3.8
	_, err = tempFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Can't rest temp file pointer", err)
		return
	}

	//process the temp file and load the processed file
	processedFilePath, err := processVideoForFastStart(tempFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Can't process video", err)
		return
	}
	processedFile, err := os.Open(processedFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Can't open processed file", err)
		return
	}
	defer os.Remove(processedFile.Name())
	defer processedFile.Close()

	//3.9
	videofiletoken := make([]byte, 32)
	_, err = rand.Read(videofiletoken)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Can't generate video file name", err)
		return
	}
	// videofilename := base64.RawURLEncoding.EncodeToString(videofiletoken) + ".mp4"
	videofilename := objectPrefix + "/" + base64.RawURLEncoding.EncodeToString(videofiletoken) + ".mp4"
	putObjectInput := &s3.PutObjectInput{
		Bucket: aws.String(cfg.s3Bucket),
		Key:    aws.String(videofilename),
		// Body:        tempFile,
		Body:        processedFile,
		ContentType: aws.String("video/mp4"),
	}
	s3client := cfg.s3Client
	_, err = s3client.PutObject(r.Context(), putObjectInput)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Can't upload video to s3", err)
		return
	}
	//3.10
	// videoUrl := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, videofilename)
	videoUrl := fmt.Sprintf("%s,%s", cfg.s3Bucket, videofilename)

	videoMeta.VideoURL = &videoUrl

	err = cfg.db.UpdateVideo(videoMeta)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update video", err)
		return
	}

	//get presigned video url
	preSignedVideoMeta, err := cfg.dbVideoToSignedVideo(videoMeta)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to get presigned video", err)
		return
	}
	// respondWithJSON(w, http.StatusOK, videoMeta)
	respondWithJSON(w, http.StatusOK, preSignedVideoMeta)
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	urlSplit := strings.Split(*video.VideoURL, ",")
	if len(urlSplit) < 2 {
		return database.Video{}, fmt.Errorf("video url not valid")
	}
	bucket := urlSplit[0]
	key := urlSplit[1]
	//Debug
	// fmt.Printf("bucket = %s; key = %s\n", bucket, key)

	preSignedUrl, err := generatePresignedURL(cfg.s3Client, bucket, key, time.Minute*10)
	if err != nil {
		return database.Video{}, fmt.Errorf("presigned video url not generated")
	}
	video.VideoURL = &preSignedUrl
	return video, nil
}
