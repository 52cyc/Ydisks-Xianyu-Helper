package mtop

import (
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

// randomPublishImageFilename 使用加密随机字节生成平台可接受的短文件名，并按实际媒体类型保留图片后缀。
func randomPublishImageFilename(randomSource io.Reader, contentType string, data []byte) (string, error) {
	// randomBytes 保存文件名使用的随机部分，八字节可在保持名称简短的同时避免同批图片重名。
	var randomBytes [8]byte
	// readErr 表示系统随机源无法提供完整八字节名称时的底层错误。
	_, readErr := io.ReadFull(randomSource, randomBytes[:])
	if readErr != nil {
		return "", fmt.Errorf("生成图片上传文件名失败: %w", readErr)
	}
	return "item-" + hex.EncodeToString(randomBytes[:]) + publishImageExtension(contentType, data), nil
}

// publishImageExtension 根据响应媒体类型或图片字节识别安全的 ASCII 文件后缀。
func publishImageExtension(contentType string, data []byte) string {
	// mediaType 是去除 charset 等参数后的标准媒体类型。
	mediaType, _, _ := mime.ParseMediaType(strings.TrimSpace(contentType))
	if mediaType == "" || mediaType == "application/octet-stream" {
		mediaType, _, _ = mime.ParseMediaType(http.DetectContentType(data))
	}
	switch strings.ToLower(mediaType) {
	case "image/jpeg", "image/jpg", "image/pjpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".bin"
	}
}
