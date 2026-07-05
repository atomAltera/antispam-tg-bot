// Package media converts media the OpenAI vision API cannot decode directly
// (e.g. Telegram video stickers, which are VP9/WEBM) into a still image the
// pipeline can analyze.
package media

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// webmMimeType is the mime type telegram media assumes for video stickers.
const webmMimeType = "video/webm"

// FFmpegExtractor extracts the first frame of a short video as a JPEG image
// by shelling out to the ffmpeg binary. There is no mature pure-Go VP9
// decoder, so ffmpeg (present in the runtime image) does the work.
type FFmpegExtractor struct {
	// Binary is the ffmpeg executable to run. Defaults to "ffmpeg".
	Binary string
}

// NewFFmpegExtractor returns an extractor that uses the "ffmpeg" binary on PATH.
func NewFFmpegExtractor() *FFmpegExtractor {
	return &FFmpegExtractor{Binary: "ffmpeg"}
}

// CanConvert reports whether this extractor can turn the given mime type into
// a still image.
func (e *FFmpegExtractor) CanConvert(mimeType string) bool {
	return mimeType == webmMimeType
}

// ToImage extracts the first video frame from content and returns it as a
// JPEG image. The video is fed to ffmpeg over stdin and the frame is read back
// over stdout, so no temporary files are touched.
func (e *FFmpegExtractor) ToImage(ctx context.Context, content []byte) ([]byte, error) {
	bin := e.Binary
	if bin == "" {
		bin = "ffmpeg"
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin,
		"-nostdin",
		"-loglevel", "error",
		"-i", "pipe:0", // read the video from stdin
		"-frames:v", "1", // just the first frame
		"-f", "image2pipe",
		"-vcodec", "mjpeg",
		"pipe:1", // write the JPEG to stdout
	)
	cmd.Stdin = bytes.NewReader(content)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("running ffmpeg: %w: %s", err, stderr.String())
	}
	if stdout.Len() == 0 {
		return nil, fmt.Errorf("ffmpeg produced no frame: %s", stderr.String())
	}
	return stdout.Bytes(), nil
}
