package transfer

import (
	"path/filepath"
	"strings"
)

const (
	CompressAuto   = "auto"
	CompressAlways = "always"
	CompressNever  = "never"
)

var incompressibleExtensions = map[string]struct{}{
	".7z":      {},
	".apk":     {},
	".avi":     {},
	".br":      {},
	".bz2":     {},
	".cab":     {},
	".cr2":     {},
	".cr3":     {},
	".dmg":     {},
	".flac":    {},
	".gif":     {},
	".gz":      {},
	".heic":    {},
	".heif":    {},
	".iso":     {},
	".jar":     {},
	".jpeg":    {},
	".jpg":     {},
	".lz4":     {},
	".m4a":     {},
	".m4v":     {},
	".mkv":     {},
	".mov":     {},
	".mp3":     {},
	".mp4":     {},
	".mpeg":    {},
	".mpg":     {},
	".ogg":     {},
	".opus":    {},
	".pdf":     {},
	".png":     {},
	".rar":     {},
	".tar.br":  {},
	".tar.bz2": {},
	".tar.gz":  {},
	".tar.xz":  {},
	".tar.zst": {},
	".tgz":     {},
	".webm":    {},
	".webp":    {},
	".whl":     {},
	".xz":      {},
	".zip":     {},
	".zst":     {},
}

func NormalizeCompressionMode(mode string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", CompressAuto:
		return CompressAuto, true
	case CompressAlways:
		return CompressAlways, true
	case CompressNever:
		return CompressNever, true
	default:
		return "", false
	}
}

func ShouldCompress(filename, mode string) bool {
	return shouldCompress(filename, mode)
}

func shouldCompress(filename, mode string) bool {
	mode, ok := NormalizeCompressionMode(mode)
	if !ok {
		mode = CompressAuto
	}

	switch mode {
	case CompressAlways:
		return true
	case CompressNever:
		return false
	}

	name := strings.ToLower(filepath.Base(filename))
	for ext := range incompressibleExtensions {
		if strings.HasSuffix(name, ext) {
			return false
		}
	}
	return true
}
