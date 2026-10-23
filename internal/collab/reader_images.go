package collab

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"teamcross/internal/materialstore"
)

type readerImage struct {
	Quote       string `json:"quote"`
	StartOffset int    `json:"startOffset"`
	EndOffset   int    `json:"endOffset"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	MIME        string `json:"mime"`
	Hash        string `json:"hash"`
}

type readerImageChunk struct {
	readerImage
	Data       string `json:"data"`
	NextOffset int    `json:"nextOffset"`
}

type ReaderRequest struct {
	AttachmentID     string               `json:"attachmentId,omitempty"`
	AttachmentOffset int                  `json:"attachmentOffset,omitempty"`
	Search           *readerSearchRequest `json:"search,omitempty"`
}

func imageMetadata(item materialstore.Item, text string, start, end int) []readerImage {
	var images []readerImage
	for _, a := range item.Attachments {
		if a.StartOffset < start || a.StartOffset >= end {
			continue
		}
		images = append(images, readerImage{Quote: readerQuote(text, a.StartOffset, a.EndOffset), StartOffset: a.StartOffset, EndOffset: a.EndOffset, ID: a.ID, Name: a.Name, MIME: a.MIME, Hash: a.Body.Hash})
	}
	return images
}

func readImage(readBody materialBodyReader, manifest materialstore.Manifest, turnID, itemID string, in ReaderRequest) (*readerImageChunk, error) {
	if turnID == "" || itemID == "" || in.AttachmentOffset < 0 || in.AttachmentOffset%4 != 0 {
		return nil, fmt.Errorf("图片读取需要准确的轮次、消息和有效偏移")
	}
	for _, turn := range manifest.Turns {
		if turn.ID != turnID {
			continue
		}
		for _, item := range turn.Items {
			if item.ID != itemID {
				continue
			}
			for _, a := range item.Attachments {
				if a.ID != in.AttachmentID {
					continue
				}
				data, err := readBody(materialstore.Item{Body: a.Body})
				if err != nil {
					return nil, err
				}
				if in.AttachmentOffset >= len(data) {
					return nil, fmt.Errorf("图片读取偏移越界")
				}
				end := min(len(data), in.AttachmentOffset+128*1024)
				next := 0
				if end < len(data) {
					next = end
				}
				return &readerImageChunk{readerImage: readerImage{StartOffset: a.StartOffset, EndOffset: a.EndOffset, ID: a.ID, Name: a.Name, MIME: a.MIME, Hash: a.Body.Hash}, Data: data[in.AttachmentOffset:end], NextOffset: next}, nil
			}
		}
	}
	return nil, fmt.Errorf("当前版本或历史页没有这张图片")
}

// Only provider-typed image fields reach this function. Markdown links,
// command arguments and arbitrary paths in tool output are never traversed.
func sourceImage(value, cwd string, local bool) (MaterialImage, error) {
	var data []byte
	name := "image"
	if strings.HasPrefix(value, "data:") {
		header, encoded, ok := strings.Cut(value, ",")
		if !ok || !strings.HasSuffix(header, ";base64") || len(encoded) > materialstore.MaxBlobBytes {
			return MaterialImage{}, fmt.Errorf("图片编码无效或过大")
		}
		var err error
		data, err = base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return MaterialImage{}, err
		}
	} else if local && value != "" {
		path := value
		if !filepath.IsAbs(path) {
			if cwd == "" {
				return MaterialImage{}, fmt.Errorf("图片缺少来源目录")
			}
			path = filepath.Join(cwd, path)
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 6<<20 {
			return MaterialImage{}, fmt.Errorf("图片文件不可读或超过 6 MiB")
		}
		file, err := os.Open(path)
		if err != nil {
			return MaterialImage{}, err
		}
		defer file.Close()
		data, err = io.ReadAll(io.LimitReader(file, (6<<20)+1))
		if err != nil {
			return MaterialImage{}, err
		}
		name = filepath.Base(path)
	} else {
		return MaterialImage{}, fmt.Errorf("远程图片未收集")
	}
	mime := http.DetectContentType(data)
	if name == "image" {
		name += "." + strings.TrimPrefix(mime, "image/")
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	if err := materialstore.ValidateImage(mime, encoded); err != nil {
		return MaterialImage{}, err
	}
	if len([]rune(name)) > 160 {
		name = string([]rune(name)[:160])
	}
	return MaterialImage{Name: name, MIME: mime, Data: encoded}, nil
}

func appendSourceImage(item *MaterialItem, value, cwd string, local bool) {
	image, err := sourceImage(value, cwd, local)
	if err != nil || len(item.Attachments) >= 32 {
		const notice = "图片未包含：来源不可读、格式不支持或超过大小限制"
		if !strings.Contains(item.Notice, notice) {
			item.Notice = appendMaterialNotice(item.Notice, notice)
		}
		item.Text += "\n[图片未包含]"
		return
	}
	image.ID = fmt.Sprintf("image-%d", len(item.Attachments)+1)
	image.StartOffset = materialstore.UTF16Length(item.Text) + 1
	// A readable, immutable source marker supplies a whole-image annotation
	// anchor without inventing coordinates in the image itself.
	item.Text += fmt.Sprintf("\n[图片 %d：%s]", len(item.Attachments)+1, image.Name)
	image.EndOffset = materialstore.UTF16Length(item.Text)
	item.Attachments = append(item.Attachments, image)
}

func sourceBlockString(block map[string]json.RawMessage, key string) string {
	var s string
	_ = json.Unmarshal(block[key], &s)
	return s
}
