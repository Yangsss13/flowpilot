package knowledge

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Yangsss13/flowpilot/internal/config"
)

type fakeCommandRunner struct {
	output []byte
	err    error
	args   []string
}

func (f *fakeCommandRunner) Run(_ context.Context, _ string, arguments []string, _ int) ([]byte, error) {
	f.args = append([]string(nil), arguments...)
	return f.output, f.err
}

func TestFFprobeValidatesContainerCodecDurationAndResolution(t *testing.T) {
	runner := &fakeCommandRunner{output: []byte(`{
  "format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"72.500"},
  "streams":[
    {"codec_type":"video","codec_name":"h264","width":1920,"height":1080},
    {"codec_type":"audio","codec_name":"aac"}
  ]
}`)}
	cfg := mediaTestConfig()
	processor := NewFFmpegProcessor(cfg, runner)
	info, err := processor.Probe(context.Background(), "video.mp4", ".mp4")
	if err != nil {
		t.Fatal(err)
	}
	if info.DurationMS != 72_500 || !info.HasAudio || !info.HasVideo || info.Width != 1920 {
		t.Fatalf("info = %#v", info)
	}
}

func TestFFprobeRejectsDurationResolutionCodecAndContainer(t *testing.T) {
	tests := []struct {
		name      string
		extension string
		info      MediaInfo
	}{
		{name: "duration", extension: ".mp4", info: MediaInfo{FormatName: "mov,mp4", DurationMS: 7_300_000, HasVideo: true, VideoCodec: "h264", Width: 1920, Height: 1080}},
		{name: "resolution", extension: ".mp4", info: MediaInfo{FormatName: "mov,mp4", DurationMS: 1000, HasVideo: true, VideoCodec: "h264", Width: 8000, Height: 4000}},
		{name: "codec", extension: ".mp4", info: MediaInfo{FormatName: "mov,mp4", DurationMS: 1000, HasVideo: true, VideoCodec: "mpeg2video", Width: 640, Height: 480}},
		{name: "container", extension: ".webm", info: MediaInfo{FormatName: "mov,mp4", DurationMS: 1000, HasVideo: true, VideoCodec: "vp9", Width: 640, Height: 480}},
	}
	processor := NewFFmpegProcessor(mediaTestConfig(), &fakeCommandRunner{})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := processor.validate(test.info, test.extension); !errors.Is(err, ErrMediaInvalid) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestParseWhisperJSONWithOffsetsAndTimestamps(t *testing.T) {
	segments, err := parseWhisperJSON([]byte(`{"transcription":[
  {"timestamps":{"from":"00:00:01,250","to":"00:00:03,500"},"offsets":{"from":0,"to":0},"text":"你好"},
  {"offsets":{"from":3500,"to":6000},"text":"FlowPilot"}
]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 2 || segments[0].StartMS != 1250 || segments[0].EndMS != 3500 || segments[1].StartMS != 3500 {
		t.Fatalf("segments = %#v", segments)
	}
}

func TestParseTesseractTSVFiltersConfidenceAndDuplicates(t *testing.T) {
	input := "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n" +
		"5\t1\t1\t1\t1\t1\t0\t0\t1\t1\t91\tFlowPilot\n" +
		"5\t1\t1\t1\t1\t2\t0\t0\t1\t1\t91\tFlowPilot\n" +
		"5\t1\t1\t1\t1\t3\t0\t0\t1\t1\t35\tnoise\n" +
		"5\t1\t1\t1\t1\t4\t0\t0\t1\t1\t88\t退款政策\n"
	if result := parseTesseractTSV(input, 60); result != "FlowPilot 退款政策" {
		t.Fatalf("result = %q", result)
	}
}

func TestMergeMediaChunksCombinesSpeechOCRAndTimeline(t *testing.T) {
	chunks, err := MergeMediaChunks([]TranscriptSegment{
		{StartMS: 192_000, EndMS: 205_000, Text: "退款申请需要订单号。"},
		{StartMS: 205_000, EndMS: 220_000, Text: "审核通常需要三个工作日。"},
	}, []FrameObservation{{TimeMS: 200_000, Text: "退款流程"}}, 1200, 20_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 || chunks[0].StartMS != 192_000 || chunks[0].EndMS != 220_000 ||
		!strings.Contains(chunks[0].Text, "画面文字：退款流程") {
		t.Fatalf("chunks = %#v", chunks)
	}
}

func TestMergeMediaChunksSupportsSilentVideoOCR(t *testing.T) {
	chunks, err := MergeMediaChunks(nil, []FrameObservation{{TimeMS: 10_000, Text: "季度营收"}}, 1200, 20_000)
	if err != nil || len(chunks) != 1 || chunks[0].StartMS != 10_000 || chunks[0].EndMS != 30_000 {
		t.Fatalf("chunks=%#v error=%v", chunks, err)
	}
}

func TestMediaSignaturesRejectExtensionSpoofing(t *testing.T) {
	if mimeMatches(".mp3", "audio/mpeg", "application/octet-stream", []byte("not mp3")) {
		t.Fatal("accepted spoofed MP3")
	}
	webm := []byte{0x1a, 0x45, 0xdf, 0xa3, 0, 0, 0, 0}
	if !mimeMatches(".webm", "video/webm", "video/webm", webm) {
		t.Fatal("rejected WebM signature")
	}
}

func TestSupportedMediaSignatures(t *testing.T) {
	tests := []struct {
		extension string
		mime      string
		header    []byte
	}{
		{extension: ".mp3", mime: "audio/mpeg", header: []byte("ID3\x04\x00\x00")},
		{extension: ".wav", mime: "audio/wav", header: []byte("RIFF\x00\x00\x00\x00WAVE")},
		{extension: ".m4a", mime: "audio/mp4", header: []byte("\x00\x00\x00\x18ftypM4A ")},
		{extension: ".mp4", mime: "video/mp4", header: []byte("\x00\x00\x00\x18ftypisom")},
		{extension: ".mov", mime: "video/quicktime", header: []byte("\x00\x00\x00\x18moovabcd")},
		{extension: ".webm", mime: "video/webm", header: []byte{0x1a, 0x45, 0xdf, 0xa3}},
	}
	for _, test := range tests {
		t.Run(test.extension, func(t *testing.T) {
			if !mimeMatches(test.extension, test.mime, test.mime, test.header) {
				t.Fatalf("rejected valid %s signature", test.extension)
			}
			if _, extension, err := ValidateUploadName("media" + test.extension); err != nil || extension != test.extension {
				t.Fatalf("ValidateUploadName extension=%q error=%v", extension, err)
			}
		})
	}
}

func mediaTestConfig() config.KnowledgeConfig {
	return config.KnowledgeConfig{
		ProbeTimeout: time.Second, FFmpegTimeout: time.Second, MaxMediaDuration: 2 * time.Hour,
		MaxVideoWidth: 3840, MaxVideoHeight: 2160, FFmpegThreads: 2,
		KeyframeInterval: 20 * time.Second, MaxKeyframes: 100,
	}
}
