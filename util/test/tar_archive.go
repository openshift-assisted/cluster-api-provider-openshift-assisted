package test

import (
	"archive/tar"
	"bytes"
	"io"
)

// Helper function to create a tar archive with a file
func CreateTarArchive(archive TarArchive) io.ReadCloser {
	buf := new(bytes.Buffer)
	tw := tar.NewWriter(buf)

	hdr := &tar.Header{
		Name: archive.Filepath,
		Mode: 0600,
		Size: int64(len(archive.Content)),
	}
	hdr.Typeflag = tar.TypeReg
	if archive.IsSymlink {
		hdr.Typeflag = tar.TypeSymlink
		hdr.Linkname = archive.SymlinkTarget
	}

	_ = tw.WriteHeader(hdr)
	if !archive.IsSymlink {
		_, _ = tw.Write([]byte(archive.Content))
	}
	_ = tw.Close()

	return io.NopCloser(buf)
}

type TarArchive struct {
	Filepath      string
	Content       string
	IsSymlink     bool
	SymlinkTarget string
}
