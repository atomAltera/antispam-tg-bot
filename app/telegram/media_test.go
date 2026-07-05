package telegram

import (
	"testing"

	"nuclight.org/antispam-tg-bot/pkg/tg"
)

func TestGetMediaInfo_Sticker(t *testing.T) {
	tests := []struct {
		name     string
		sticker  *tg.Sticker
		wantMime string
	}{
		{
			name:     "static sticker is a real webp image",
			sticker:  &tg.Sticker{FileID: "f1"},
			wantMime: "image/webp",
		},
		{
			name:     "animated (Lottie) sticker is not an image",
			sticker:  &tg.Sticker{FileID: "f2", IsAnimated: true},
			wantMime: "application/x-tgsticker",
		},
		{
			name:     "video sticker is webm, not webp",
			sticker:  &tg.Sticker{FileID: "f3", IsVideo: true},
			wantMime: "video/webm",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mi := getMediaInfo(&tg.Message{Sticker: tc.sticker})
			if mi == nil {
				t.Fatal("getMediaInfo returned nil")
			}
			if mi.mimeType != tc.wantMime {
				t.Errorf("mimeType = %q, want %q", mi.mimeType, tc.wantMime)
			}
			if mi.fileID != tc.sticker.FileID {
				t.Errorf("fileID = %q, want %q", mi.fileID, tc.sticker.FileID)
			}
		})
	}
}
