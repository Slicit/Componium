package studio

import (
	"strings"
	"testing"
)

// The plan is the part of this worth testing hard. Getting it wrong is not a
// cosmetic failure: deciding to re-encode a film whose video was already fine
// turns three minutes of work into ninety-five, and deciding to copy one that
// was not produces a preview that silently will not play — which is the exact
// problem the whole feature exists to fix.
func TestPlanPreview(t *testing.T) {
	cases := []struct {
		name string
		in   mediaInfo
		want previewPlan
	}{{
		// The common library file: H.264 already, so the video is copied and
		// the whole job is a remux. This is the case that has to be right,
		// because it is most of a real library and it is thirty times cheaper.
		name: "matroska h264 with DTS",
		in: mediaInfo{Ext: ".mkv", VideoCodec: "h264", Height: 816,
			HasAudio: true, AudioCodec: "dts", AudioChannels: 6},
		want: previewPlan{Needed: true, CopyVideo: true, CopyAudio: false, Height: 0},
	}, {
		// HEVC has to be re-encoded, and only then is scaling worth doing.
		name: "matroska hevc with e-ac-3",
		in: mediaInfo{Ext: ".mkv", VideoCodec: "hevc", Height: 1080,
			HasAudio: true, AudioCodec: "eac3", AudioChannels: 6},
		want: previewPlan{Needed: true, CopyVideo: false, CopyAudio: false, Height: 720},
	}, {
		name: "already a browser mp4",
		in: mediaInfo{Ext: ".mp4", VideoCodec: "h264", Height: 240,
			HasAudio: true, AudioCodec: "aac", AudioChannels: 2},
		want: previewPlan{Needed: false},
	}, {
		// Right codecs, wrong box. Only the container is in the way, so this
		// is still a remux.
		name: "mkv with browser-ready streams",
		in: mediaInfo{Ext: ".mkv", VideoCodec: "h264", Height: 360,
			HasAudio: true, AudioCodec: "aac", AudioChannels: 2},
		want: previewPlan{Needed: true, CopyVideo: true, CopyAudio: true},
	}, {
		// An MP4 whose only problem is six channels of AAC. The video must
		// not be touched for that.
		name: "mp4 with 5.1 aac",
		in: mediaInfo{Ext: ".mp4", VideoCodec: "h264", Height: 1080,
			HasAudio: true, AudioCodec: "aac", AudioChannels: 6},
		want: previewPlan{Needed: true, CopyVideo: true, CopyAudio: false},
	}, {
		name: "webm is left alone",
		in: mediaInfo{Ext: ".webm", VideoCodec: "vp9", Height: 720,
			HasAudio: true, AudioCodec: "opus", AudioChannels: 2},
		want: previewPlan{Needed: false},
	}, {
		// Opus is fine in WebM and awkward in MP4, so moving the container
		// means the audio has to move too.
		name: "opus in matroska",
		in: mediaInfo{Ext: ".mkv", VideoCodec: "h264", Height: 720,
			HasAudio: true, AudioCodec: "opus", AudioChannels: 2},
		want: previewPlan{Needed: true, CopyVideo: true, CopyAudio: false},
	}, {
		name: "silent film",
		in:   mediaInfo{Ext: ".mkv", VideoCodec: "h264", Height: 480},
		want: previewPlan{Needed: true, CopyVideo: true, CopyAudio: false},
	}, {
		// Already short enough that scaling would be upscaling, which spends
		// time to add nothing.
		name: "small hevc is not upscaled",
		in: mediaInfo{Ext: ".mkv", VideoCodec: "hevc", Height: 480,
			HasAudio: true, AudioCodec: "aac", AudioChannels: 2},
		want: previewPlan{Needed: true, CopyVideo: false, CopyAudio: true, Height: 0},
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := planPreview(tc.in)
			if got.Needed != tc.want.Needed {
				t.Fatalf("Needed = %v, want %v (%s)", got.Needed, tc.want.Needed, got.Why)
			}
			if !tc.want.Needed {
				return
			}
			if got.CopyVideo != tc.want.CopyVideo {
				t.Errorf("CopyVideo = %v, want %v", got.CopyVideo, tc.want.CopyVideo)
			}
			if got.CopyAudio != tc.want.CopyAudio {
				t.Errorf("CopyAudio = %v, want %v", got.CopyAudio, tc.want.CopyAudio)
			}
			if got.Height != tc.want.Height {
				t.Errorf("Height = %d, want %d", got.Height, tc.want.Height)
			}
			if got.Why == "" {
				t.Error("no explanation given for a preview that is needed")
			}
		})
	}
}

// The arguments matter as much as the decision: a plan that says "copy" and
// then emits an encode is the expensive bug wearing the cheap plan's clothes.
func TestFFmpegArgs(t *testing.T) {
	remux := planPreview(mediaInfo{Ext: ".mkv", VideoCodec: "h264", Height: 816,
		HasAudio: true, AudioCodec: "dts", AudioChannels: 6})
	args := strings.Join(remux.ffmpegArgs("in.mkv", "out.mp4"), " ")

	if !strings.Contains(args, "-c:v copy") {
		t.Errorf("remux plan does not copy the video: %s", args)
	}
	if strings.Contains(args, "libx264") {
		t.Errorf("remux plan re-encodes anyway: %s", args)
	}
	if !strings.Contains(args, "-c:a aac") || !strings.Contains(args, "-ac 2") {
		t.Errorf("DTS was not downmixed to stereo AAC: %s", args)
	}
	// Without faststart the index lands at the end and the browser has to
	// fetch the whole file before it can show a single frame.
	if !strings.Contains(args, "-movflags +faststart") {
		t.Errorf("no faststart: %s", args)
	}
	// An optional audio mapping, so a silent film does not fail the job.
	if !strings.Contains(args, "0:a:0?") {
		t.Errorf("audio mapping is not optional: %s", args)
	}
	// The container has to be stated, because the real output path ends in
	// ".part" and ffmpeg infers the format from the extension. Without this it
	// refuses to start, which is how this was found.
	if !strings.Contains(args, "-f mp4") {
		t.Errorf("output format is not stated: %s", args)
	}

	encode := planPreview(mediaInfo{Ext: ".mkv", VideoCodec: "hevc", Height: 1080,
		HasAudio: true, AudioCodec: "eac3", AudioChannels: 6})
	eargs := strings.Join(encode.ffmpegArgs("in.mkv", "out.mp4"), " ")
	if !strings.Contains(eargs, "libx264") {
		t.Errorf("hevc plan does not re-encode: %s", eargs)
	}
	if !strings.Contains(eargs, "scale=-2:720") {
		t.Errorf("1080p was not scaled down: %s", eargs)
	}
}

func TestPreviewNaming(t *testing.T) {
	if got := previewName("A Film.mkv"); got != "A Film.preview.mp4" {
		t.Errorf("previewName = %q", got)
	}
	if !isPreview("A Film.preview.mp4") {
		t.Error("a preview was not recognised as one")
	}
	if isPreview("A Film.mp4") {
		t.Error("a film was mistaken for a preview")
	}
	// Round trip: a preview's own name must not survive another pass, or a
	// second prepare would build a preview of a preview.
	if !isPreview(previewName("x.mkv")) {
		t.Error("previewName does not produce a recognisable preview")
	}
}

func TestParseFFmpegProgress(t *testing.T) {
	if v, ok := parseFFmpegProgress("out_time_us=1500000"); !ok || v != 1.5 {
		t.Errorf("got %v %v, want 1.5 true", v, ok)
	}
	for _, line := range []string{"frame=12", "out_time_us=N/A", "out_time_us=-1", ""} {
		if _, ok := parseFFmpegProgress(line); ok {
			t.Errorf("%q was parsed as progress", line)
		}
	}
}
