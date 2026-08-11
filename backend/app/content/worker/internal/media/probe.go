package media

import (
	"context"
	"encoding/json"
	"strconv"
)

// ProbeResult ffprobe 探测结果。
type ProbeResult struct {
	DurationMs int64  // 真实媒体时长（ms），替代不可信的客户端上报
	Width      int
	Height     int
	CodecName  string
}

// ffprobeJSON ffprobe -of json 输出结构（只解析需要的字段）。
type ffprobeJSON struct {
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
	Streams []struct {
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		CodecType string `json:"codec_type"`
	} `json:"streams"`
}

// Probe 用 ffprobe 探测媒体信息（真实时长，不信任客户端上报）。
func Probe(ctx context.Context, ffprobePath string, exec Executor, input string) (*ProbeResult, error) {
	out, err := exec.Run(ctx, ffprobePath,
		"-v", "error",
		"-show_entries", "format=duration:stream=codec_name,width,height,codec_type",
		"-of", "json", input)
	if err != nil {
		return nil, err
	}
	var pj ffprobeJSON
	if err := json.Unmarshal(out, &pj); err != nil {
		return nil, err
	}
	res := &ProbeResult{}
	if d, err := strconv.ParseFloat(pj.Format.Duration, 64); err == nil {
		res.DurationMs = int64(d * 1000)
	}
	for _, s := range pj.Streams {
		if s.CodecType == "video" {
			res.Width = s.Width
			res.Height = s.Height
			res.CodecName = s.CodecName
			break
		}
	}
	return res, nil
}
