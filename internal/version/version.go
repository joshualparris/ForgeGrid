package version

import "runtime"

var (
	Version   = "0.8.0"
	Commit    = "dev"
	BuildTime = "unknown"
	Protocol  = "1"
)

type InfoData struct {
	Version      string `json:"version"`
	Commit       string `json:"commit"`
	BuildTime    string `json:"build_time"`
	Platform     string `json:"platform"`
	Architecture string `json:"architecture"`
	Protocol     string `json:"protocol"`
}

func Info() InfoData {
	return InfoData{
		Version:      Version,
		Commit:       Commit,
		BuildTime:    BuildTime,
		Platform:     runtime.GOOS,
		Architecture: runtime.GOARCH,
		Protocol:     Protocol,
	}
}
