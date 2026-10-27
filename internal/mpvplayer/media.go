package mpvplayer

import (
	"github.com/ccammack/easytv/internal/preparedmedia"
	"strconv"
)

func mediaArguments(original string, offset int64) []string {
	if preparedmedia.IsImage(original) {
		return []string{"--keep-open=yes", "--image-display-duration=86400"}
	}
	args := []string{"--keep-open=no"}
	if offset > 0 {
		args = append(args, "--start="+strconv.FormatInt(offset, 10))
	}
	return args
}
