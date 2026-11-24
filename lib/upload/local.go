package upload

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const (
	AvatarDirectory = "data/avatars"
	AvatarURLPrefix = "/api/avatar/"
	MaxAvatarBytes  = 2 << 20
	MaxAvatarPixels = 4 << 20
)

func SaveAvatar(reader io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(reader, MaxAvatarBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > MaxAvatarBytes {
		return "", errors.New("头像不能超过 2 MB")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg" && format != "gif") {
		return "", errors.New("请上传有效的 PNG、JPEG 或 GIF 图片")
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > 4096 || config.Height > 4096 {
		return "", errors.New("头像尺寸不能超过 4096 × 4096")
	}
	if int64(config.Width)*int64(config.Height) > MaxAvatarPixels {
		return "", errors.New("头像像素不能超过 4 MP")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", errors.New("无法读取头像图片")
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		return "", err
	}
	if encoded.Len() > MaxAvatarBytes {
		return "", errors.New("头像编码后不能超过 2 MB")
	}
	if err := os.MkdirAll(AvatarDirectory, 0755); err != nil {
		return "", err
	}
	name := uuid.NewString() + ".png"
	path := filepath.Join(AvatarDirectory, name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return "", err
	}
	_, encodeErr := file.Write(encoded.Bytes())
	closeErr := file.Close()
	if err := errors.Join(encodeErr, closeErr); err != nil {
		return "", errors.Join(err, os.Remove(path))
	}
	return AvatarURLPrefix + name, nil
}

func AvatarPath(url string) string {
	name, ok := strings.CutPrefix(url, AvatarURLPrefix)
	if !ok || !strings.HasSuffix(name, ".png") {
		return ""
	}
	id := strings.TrimSuffix(name, ".png")
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		return ""
	}
	return filepath.Join(AvatarDirectory, name)
}

func RemoveAvatar(url string) error {
	path := AvatarPath(url)
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
