package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func getVideoAspectRatio(filePath string) (string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("can't run cmd ffprobe")
	}

	var data map[string]any
	var aspectRatio string
	err = json.Unmarshal(out.Bytes(), &data)
	if err != nil {
		return "", fmt.Errorf("unmarshal failed")
	}
	streams, ok := data["streams"].([]any)
	if !ok || len(streams) == 0 {
		return "", fmt.Errorf("no streams found")
	}
	stream, ok := streams[0].(map[string]any)
	if !ok {
		return "", fmt.Errorf("invalid stream format")
	}
	width, ok := stream["width"].(float64)
	fmt.Printf("width = %f\n", width)
	if !ok {
		return "", fmt.Errorf("width not found")
	}
	height, ok := stream["height"].(float64)
	if !ok {
		return "", fmt.Errorf("height not found")
	}
	fmt.Printf("height = %f\n", height)
	if math.Abs(width*9-height*16) < 10 {
		aspectRatio = "16:9"
	} else if math.Abs(width*16-height*9) < 10 {
		aspectRatio = "9:16"
	} else {
		aspectRatio = "other"
	}
	return aspectRatio, nil
}

func processVideoForFastStart(filePath string) (string, error) {
	targetFilePath := filePath + ".processing"
	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", targetFilePath)
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("can't process video")
	}
	return targetFilePath, nil
}

func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	preSignedClient := s3.NewPresignClient(s3Client)

	preSignedUrl, err := preSignedClient.PresignGetObject(
		context.TODO(),
		&s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		},
		s3.WithPresignExpires(expireTime),
	)
	if err != nil {
		return "", err
	}

	//Debug
	fmt.Printf("presigned url = %s\n", preSignedUrl.URL)

	return preSignedUrl.URL, nil

}
