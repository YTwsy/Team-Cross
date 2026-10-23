package materialstore

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

func SupportedImageMIME(mime string) bool {
	return mime == "image/png" || mime == "image/jpeg" || mime == "image/gif" || mime == "image/webp"
}

// Image blobs contain canonical base64, allowing the existing bounded text
// upload and MCP transports to carry exactly the same immutable bytes.
func ValidateImage(mime, data string) error {
	if !SupportedImageMIME(mime) || len(data) > MaxBlobBytes {
		return fmt.Errorf("图片类型不支持或超过 6 MiB")
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil || base64.StdEncoding.EncodeToString(raw) != data {
		return fmt.Errorf("图片编码无效")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || "image/"+format != mime || config.Width <= 0 || config.Height <= 0 || config.Width > 16384 || config.Height > 16384 || int64(config.Width)*int64(config.Height) > 40000000 {
		return fmt.Errorf("图片格式无效或像素尺寸过大")
	}
	return nil
}
