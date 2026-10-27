package mpvplayer

import (
	"context"
	"errors"
	"github.com/ccammack/easytv/internal/preparedmedia"
	"reflect"
	"testing"
)

func TestReferenceMediaArguments(t *testing.T) {
	for _, test := range []struct {
		path   string
		offset int64
		want   []string
	}{
		{"photo.JPG", 90, []string{"--keep-open=yes", "--image-display-duration=86400"}},
		{"movie.mp4", 90, []string{"--keep-open=no", "--start=90"}},
		{"sound.wav", 0, []string{"--keep-open=no"}},
	} {
		if got := mediaArguments(test.path, test.offset); !reflect.DeepEqual(got, test.want) {
			t.Fatalf("args=%v want=%v", got, test.want)
		}
	}
}
func TestPreparationFailureDoesNotLaunchPlayer(t *testing.T) {
	failure := errors.New("prepare failed")
	p := New(Config{Prepare: func(context.Context, string) (preparedmedia.Result, error) { return preparedmedia.Result{}, failure }})
	if err := p.Start("photo.png", 90, 1); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if p.cmd != nil || p.client != nil || p.dir != "" {
		t.Fatal("failed preparation retained resources")
	}
}
func TestEmptyPreparedPathRejected(t *testing.T) {
	p := New(Config{Prepare: func(context.Context, string) (preparedmedia.Result, error) { return preparedmedia.Result{}, nil }})
	if err := p.Start("photo.png", 0, 1); err == nil {
		t.Fatal("empty prepared path accepted")
	}
}
