package media

import (
	"bytes"
	"context"
	_ "embed"
	"os/exec"
	"testing"
)

//go:embed testdata/sticker.webm
var stickerWebM []byte

// jpegMagic is the SOI marker that starts every JPEG file.
var jpegMagic = []byte{0xFF, 0xD8, 0xFF}

func TestFFmpegExtractor_CanConvert(t *testing.T) {
	e := NewFFmpegExtractor()

	if !e.CanConvert("video/webm") {
		t.Error("CanConvert(video/webm) = false, want true")
	}
	for _, mt := range []string{"image/webp", "image/jpeg", "application/x-tgsticker", ""} {
		if e.CanConvert(mt) {
			t.Errorf("CanConvert(%q) = true, want false", mt)
		}
	}
}

func TestFFmpegExtractor_ToImage(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed; skipping frame extraction test")
	}

	e := NewFFmpegExtractor()
	jpeg, err := e.ToImage(context.Background(), stickerWebM)
	if err != nil {
		t.Fatalf("ToImage: %v", err)
	}
	if !bytes.HasPrefix(jpeg, jpegMagic) {
		t.Errorf("output is not a JPEG (prefix %x)", jpeg[:min(3, len(jpeg))])
	}
}

func TestFFmpegExtractor_ToImageBadInput(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed; skipping frame extraction test")
	}

	e := NewFFmpegExtractor()
	_, err := e.ToImage(context.Background(), []byte("this is not a video"))
	if err == nil {
		t.Fatal("expected an error for non-video input, got nil")
	}
}
