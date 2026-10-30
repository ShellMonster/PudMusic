package perf

import (
	"io/fs"
	"os"
)

// ArtifactSize is one measured build artifact.
type ArtifactSize struct {
	Path  string
	Bytes int64
}

// MeasureArtifacts reports the size of each path, following the same file and
// directory accounting as the platform's du: a directory counts the bytes of
// the files below it. A missing path is an error rather than a zero, because a
// zero would read as "this build produced nothing".
func MeasureArtifacts(paths ...string) ([]ArtifactSize, error) {
	out := make([]ArtifactSize, 0, len(paths))
	for _, p := range paths {
		n, err := pathBytes(p)
		if err != nil {
			return nil, err
		}
		out = append(out, ArtifactSize{Path: p, Bytes: n})
	}
	return out, nil
}

// TotalBytes is the sum of measured artifacts.
func TotalBytes(sizes []ArtifactSize) int64 {
	var n int64
	for _, s := range sizes {
		n += s.Bytes
	}
	return n
}

func pathBytes(p string) (int64, error) {
	info, err := os.Stat(p)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return info.Size(), nil
	}
	var total int64
	err = fs.WalkDir(os.DirFS(p), ".", func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		total += fi.Size()
		return nil
	})
	return total, err
}
