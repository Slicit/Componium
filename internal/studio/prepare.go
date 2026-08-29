package studio

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Preparing a film for the browser.
//
// A film that plays perfectly well in mpv very often will not play in a
// browser at all, and for reasons that have nothing to do with each other:
// Matroska is not a container any browser demuxes, HEVC decoding is absent or
// conditional almost everywhere, and DTS and E-AC-3 are not audio codecs a
// browser has ever shipped. A typical library file fails two or three of those
// at once, which is why "it plays but has no sound" and "it does not play"
// are both common answers to the same question.
//
// So the studio makes a second copy that a browser can read, next to the
// original, and plays that instead. The original is never touched: it stays
// the thing the analysis reads and the thing the operator keeps.
//
// The important part is doing as little work as possible. Re-encoding video is
// by far the most expensive thing this program can do — measured on the
// project's own box, a 110 minute 1080p film takes about 95 minutes to
// re-encode at 720p but only about 3 minutes to remux with the video copied
// untouched, a factor of thirty. And most library files are already H.264,
// which every browser decodes. Their video does not need re-encoding at all;
// only the container and the audio are in the way. Working that out per file
// is the difference between a preview that is ready before you have finished
// reading the synopsis and one that takes the rest of the afternoon.

// mediaInfo is what ffprobe found. Only the parts that decide the plan.
type mediaInfo struct {
	Ext           string
	VideoCodec    string
	AudioCodec    string
	AudioChannels int
	HasAudio      bool
	Height        int
	Duration      float64
}

// previewPlan is what to do about it.
type previewPlan struct {
	// Needed is false when the original is already something a browser can
	// play, in which case nothing is produced and nothing is copied.
	Needed bool
	// CopyVideo is the expensive decision. True means a remux.
	CopyVideo bool
	CopyAudio bool
	// Height to scale down to, or 0 to leave alone. Only consulted when the
	// video is being re-encoded anyway; scaling is free at that point and
	// pointless otherwise.
	Height int
	Why    string
}

// previewHeight is the tallest a re-encoded preview is made.
//
// This is a preview to scrub a score against, not a viewing copy, and the cost
// is superlinear in pixel count on the small machines this tends to run on.
// 720 is high enough to recognise what is happening in a shot, which is the
// job. Films already shorter than this are left at their own height rather
// than upscaled, because upscaling spends time to add nothing.
const previewHeight = 720

// browserVideo reports whether a browser can be expected to decode this video
// codec inside an MP4.
//
// Deliberately short. H.264 is universal and AV1 is now broadly supported;
// everything else is either absent (HEVC, VC-1, MPEG-4 part 2), conditional on
// hardware (HEVC again), or awkward in MP4 even where the codec itself is fine
// (VP8, VP9). Being wrong here is expensive in the direction of a file that
// silently does not play, and cheap in the direction of an unnecessary
// re-encode, so the list stays conservative.
func browserVideo(codec string) bool {
	switch codec {
	case "h264", "av1":
		return true
	}
	return false
}

// browserAudio reports whether this audio track can be carried across
// untouched. Stereo only: a 5.1 AAC track is legal in MP4 and will play, but
// downmixing is cheap and a browser on laptop speakers does nothing useful
// with six channels.
func browserAudio(codec string, channels int) bool {
	return codec == "aac" && channels > 0 && channels <= 2
}

// planPreview decides what, if anything, to do about a film.
func planPreview(info mediaInfo) previewPlan {
	ext := strings.ToLower(info.Ext)
	video := browserVideo(info.VideoCodec)
	audio := !info.HasAudio || browserAudio(info.AudioCodec, info.AudioChannels)
	mp4 := ext == ".mp4" || ext == ".m4v"

	// A WebM of VP8/VP9/AV1 with Opus or Vorbis is already a browser format.
	// Rewrapping it into MP4 would be strictly worse.
	if ext == ".webm" {
		switch info.VideoCodec {
		case "vp8", "vp9", "av1":
			switch {
			case !info.HasAudio, info.AudioCodec == "opus", info.AudioCodec == "vorbis":
				return previewPlan{Needed: false, Why: "already a WebM a browser can play"}
			}
		}
	}

	if mp4 && video && audio {
		return previewPlan{Needed: false, Why: "already an MP4 a browser can play"}
	}

	plan := previewPlan{
		Needed:    true,
		CopyVideo: video,
		CopyAudio: info.HasAudio && browserAudio(info.AudioCodec, info.AudioChannels),
	}
	if !plan.CopyVideo && info.Height > previewHeight {
		plan.Height = previewHeight
	}

	var reasons []string
	if !mp4 && ext != "" {
		reasons = append(reasons, "no browser demuxes "+strings.TrimPrefix(ext, "."))
	}
	if !video {
		if info.VideoCodec == "" {
			reasons = append(reasons, "unknown video codec")
		} else {
			reasons = append(reasons, "no browser decodes "+info.VideoCodec)
		}
	}
	if info.HasAudio && !browserAudio(info.AudioCodec, info.AudioChannels) {
		if info.AudioCodec != "aac" {
			reasons = append(reasons, "no browser decodes "+info.AudioCodec)
		} else {
			reasons = append(reasons, fmt.Sprintf("%d channel audio", info.AudioChannels))
		}
	}
	plan.Why = strings.Join(reasons, "; ")

	if plan.CopyVideo {
		plan.Why += " (video copied, not re-encoded)"
	}
	return plan
}

// ffmpegArgs builds the command for a plan.
func (p previewPlan) ffmpegArgs(in, out string) []string {
	args := []string{
		"-hide_banner", "-nostdin", "-y",
		"-i", in,
		// One video and one audio track. A library file often carries several
		// audio languages and a handful of subtitle tracks; MP4 will not take
		// SubRip at all, and the extra audio is weight for no gain in a
		// preview. Taking the first of each is what a player would default to.
		"-map", "0:v:0",
		// "?" makes the audio mapping optional, so a film with no audio track
		// produces a silent preview rather than an error.
		"-map", "0:a:0?",
	}

	if p.CopyVideo {
		args = append(args, "-c:v", "copy")
	} else {
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-crf", "24",
			"-pix_fmt", "yuv420p")
		if p.Height > 0 {
			// -2 keeps the width even, which H.264 requires.
			args = append(args, "-vf", fmt.Sprintf("scale=-2:%d", p.Height))
		}
	}

	if p.CopyAudio {
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args, "-c:a", "aac", "-ac", "2", "-b:a", "160k")
	}

	args = append(args,
		// Without this the index lands at the end of the file and the browser
		// must download the whole thing before it can play or seek a single
		// frame. For a multi-gigabyte film that is the difference between
		// working and appearing to hang.
		"-movflags", "+faststart",
		// Progress on stdout, so the job can report a real fraction instead of
		// a spinner. Parsed in runPrepare.
		"-progress", "pipe:1", "-nostats",
		// Say the container out loud rather than letting ffmpeg infer it from
		// the filename. It cannot: the output is written to "….preview.mp4.part"
		// so that an interrupted run never leaves something that looks like a
		// finished preview, and ".part" is not an extension ffmpeg recognises.
		// Without this it refuses to start, having got as far as opening the
		// input and read the whole file's headers to do it.
		"-f", "mp4",
		out,
	)
	return args
}

// previewName is where a film's browser-playable copy lives.
//
// Beside the film rather than in a cache directory, so that deleting a film
// and its preview is one obvious operation, and so a person looking at the
// media directory can see what is taking up the space.
func previewName(film string) string {
	return strings.TrimSuffix(film, filepath.Ext(film)) + ".preview.mp4"
}

// isPreview reports whether a name is a generated preview rather than a film.
// The media listing hides these: a preview is not a separate film, and showing
// it as one would offer to analyse it and produce a second score.
func isPreview(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".preview.mp4")
}

// probe asks ffprobe what is in a file.
func probe(path string) (mediaInfo, error) {
	info := mediaInfo{Ext: filepath.Ext(path)}

	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "stream=index,codec_type,codec_name,channels,height",
		"-show_entries", "format=duration",
		"-of", "json", path,
	)
	out, err := cmd.Output()
	if err != nil {
		return info, fmt.Errorf("ffprobe: %w", err)
	}

	var parsed struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			Channels  int    `json:"channels"`
			Height    int    `json:"height"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return info, fmt.Errorf("ffprobe output: %w", err)
	}

	for _, st := range parsed.Streams {
		switch st.CodecType {
		case "video":
			// First video stream only. Cover art in a container shows up as a
			// second video stream and would otherwise overwrite the real one.
			if info.VideoCodec == "" {
				info.VideoCodec = st.CodecName
				info.Height = st.Height
			}
		case "audio":
			if !info.HasAudio {
				info.HasAudio = true
				info.AudioCodec = st.CodecName
				info.AudioChannels = st.Channels
			}
		}
	}
	info.Duration, _ = strconv.ParseFloat(parsed.Format.Duration, 64)
	return info, nil
}

// ffmpegAvailable reports whether previews can be made at all.
func ffmpegAvailable() bool {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return false
	}
	_, err := exec.LookPath("ffprobe")
	return err == nil
}

// parseFFmpegProgress reads one line of ffmpeg's -progress output and returns
// the position in seconds. ffmpeg writes a block of key=value lines per
// update; out_time_us is the one worth reading.
func parseFFmpegProgress(line string) (float64, bool) {
	const key = "out_time_us="
	if !strings.HasPrefix(line, key) {
		return 0, false
	}
	us, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, key)), 10, 64)
	if err != nil || us < 0 {
		return 0, false
	}
	return float64(us) / 1e6, true
}
