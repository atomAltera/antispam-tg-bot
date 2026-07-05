package services

import (
	"context"
	"errors"
	"testing"

	"nuclight.org/antispam-tg-bot/pkg/ai"
	e "nuclight.org/antispam-tg-bot/pkg/entities"
)

// fakeAI records how the moderator invoked the vision / text completion calls.
type fakeAI struct {
	imageCalled bool
	imageMime   string
	imageBytes  []byte
	textCalled  bool
}

func (f *fakeAI) GetJSONCompletion(_ context.Context, _, _ string, _ ai.ResponseFormat, _ any) (*ai.Usage, error) {
	f.textCalled = true
	return &ai.Usage{}, nil
}

func (f *fakeAI) GetJSONCompletionWithImage(_ context.Context, _, _ string, image []byte, mimeType string, _ ai.ResponseFormat, _ any) (*ai.Usage, error) {
	f.imageCalled = true
	f.imageMime = mimeType
	f.imageBytes = image
	return &ai.Usage{}, nil
}

type fakeDownloader struct{ content []byte }

func (f *fakeDownloader) DownloadFile(_ context.Context, _ string) ([]byte, error) {
	return f.content, nil
}

type fakeConverter struct {
	convertible string
	output      []byte
	err         error
	called      bool
}

func (f *fakeConverter) CanConvert(mimeType string) bool { return mimeType == f.convertible }
func (f *fakeConverter) ToImage(_ context.Context, _ []byte) ([]byte, error) {
	f.called = true
	return f.output, f.err
}

func strptr(s string) *string { return &s }
func i64ptr(v int64) *int64   { return &v }

func mediaMsg(mime string) e.Message {
	return e.Message{
		Sender:      e.User{ID: "1"},
		ID:          "m1",
		MediaType:   strptr(mime),
		MediaFileID: strptr("file123"),
		MediaSize:   i64ptr(100 * 1024), // within the convertible size limit
	}
}

func TestCheckSpam_VideoStickerExtractsFrame(t *testing.T) {
	aiClient := &fakeAI{}
	converter := &fakeConverter{convertible: "video/webm", output: []byte("jpeg-frame")}
	s := &ModeratingSrv{
		AI:              aiClient,
		MediaDownloader: &fakeDownloader{content: []byte("webm-bytes")},
		MediaConverter:  converter,
	}

	if _, err := s.checkSpam(context.Background(), mediaMsg("video/webm")); err != nil {
		t.Fatalf("checkSpam: %v", err)
	}

	if !converter.called {
		t.Error("converter was not called for video/webm")
	}
	if !aiClient.imageCalled {
		t.Fatal("vision API was not called")
	}
	if aiClient.imageMime != "image/jpeg" {
		t.Errorf("vision mime = %q, want image/jpeg", aiClient.imageMime)
	}
	if string(aiClient.imageBytes) != "jpeg-frame" {
		t.Errorf("vision bytes = %q, want converted frame", aiClient.imageBytes)
	}
}

func TestCheckSpam_DirectImageSkipsConverter(t *testing.T) {
	aiClient := &fakeAI{}
	converter := &fakeConverter{convertible: "video/webm", output: []byte("jpeg-frame")}
	s := &ModeratingSrv{
		AI:              aiClient,
		MediaDownloader: &fakeDownloader{content: []byte("real-webp")},
		MediaConverter:  converter,
	}

	if _, err := s.checkSpam(context.Background(), mediaMsg("image/webp")); err != nil {
		t.Fatalf("checkSpam: %v", err)
	}

	if converter.called {
		t.Error("converter should not be called for a directly-supported image")
	}
	if aiClient.imageMime != "image/webp" {
		t.Errorf("vision mime = %q, want image/webp", aiClient.imageMime)
	}
	if string(aiClient.imageBytes) != "real-webp" {
		t.Errorf("vision bytes = %q, want downloaded content", aiClient.imageBytes)
	}
}

func TestCheckSpam_ConversionFailureFallsBackToText(t *testing.T) {
	// A failed conversion (corrupt webm or broken ffmpeg) must not abort
	// moderation - the accompanying text still needs to be spam-checked,
	// otherwise spam text could bypass moderation via an unconvertible file.
	aiClient := &fakeAI{}
	converter := &fakeConverter{convertible: "video/webm", err: errors.New("ffmpeg blew up")}
	s := &ModeratingSrv{
		AI:              aiClient,
		MediaDownloader: &fakeDownloader{content: []byte("webm-bytes")},
		MediaConverter:  converter,
	}

	msg := mediaMsg("video/webm")
	msg.Text = "spammy text"

	if _, err := s.checkSpam(context.Background(), msg); err != nil {
		t.Fatalf("checkSpam should not error on conversion failure, got: %v", err)
	}
	if !converter.called {
		t.Error("converter should have been attempted")
	}
	if aiClient.imageCalled {
		t.Error("vision API should not be called after conversion failure")
	}
	if !aiClient.textCalled {
		t.Error("text spam check must still run after conversion failure")
	}
}

func TestCheckSpam_MediaOnlyConversionFailureErrors(t *testing.T) {
	// A media-only message whose conversion fails has nothing real to
	// analyze - the check must error out (leading to a noop upstream)
	// instead of moderating on the synthetic "(no text)" placeholder.
	aiClient := &fakeAI{}
	converter := &fakeConverter{convertible: "video/webm", err: errors.New("ffmpeg blew up")}
	s := &ModeratingSrv{
		AI:              aiClient,
		MediaDownloader: &fakeDownloader{content: []byte("webm-bytes")},
		MediaConverter:  converter,
	}

	msg := mediaMsg("video/webm") // no text

	if _, err := s.checkSpam(context.Background(), msg); err == nil {
		t.Fatal("expected error for media-only message with failed conversion, got nil")
	}
	if aiClient.imageCalled || aiClient.textCalled {
		t.Error("no AI call should be made when there is no analyzable content")
	}
}

func TestCheckSpam_LargeWebMNotConverted(t *testing.T) {
	aiClient := &fakeAI{}
	converter := &fakeConverter{convertible: "video/webm", output: []byte("jpeg-frame")}
	s := &ModeratingSrv{
		AI:              aiClient,
		MediaDownloader: &fakeDownloader{content: []byte("webm-bytes")},
		MediaConverter:  converter,
	}

	for name, size := range map[string]*int64{
		"oversized":    i64ptr(2 * 1024 * 1024), // over the limit
		"unknown size": i64ptr(0),               // Telegram omitted file_size
		"nil size":     nil,
	} {
		t.Run(name, func(t *testing.T) {
			aiClient.imageCalled, aiClient.textCalled, converter.called = false, false, false

			msg := mediaMsg("video/webm")
			msg.MediaSize = size
			msg.Text = "hi"

			if _, err := s.checkSpam(context.Background(), msg); err != nil {
				t.Fatalf("checkSpam: %v", err)
			}

			if converter.called {
				t.Error("converter should not run without a known small size (would download+ffmpeg a large upload)")
			}
			if aiClient.imageCalled {
				t.Error("vision API should not be called without a known small size")
			}
			if !aiClient.textCalled {
				t.Error("should fall back to text analysis")
			}
		})
	}
}

func TestCheckSpam_VideoStickerWithoutConverterFallsBackToText(t *testing.T) {
	aiClient := &fakeAI{}
	s := &ModeratingSrv{
		AI:              aiClient,
		MediaDownloader: &fakeDownloader{content: []byte("webm-bytes")},
		// MediaConverter intentionally nil
	}

	msg := mediaMsg("video/webm")
	msg.Text = "hello"
	if _, err := s.checkSpam(context.Background(), msg); err != nil {
		t.Fatalf("checkSpam: %v", err)
	}

	if aiClient.imageCalled {
		t.Error("vision API should not be called when media is unconvertible and no converter is set")
	}
	if !aiClient.textCalled {
		t.Error("text completion should be used as fallback")
	}
}
