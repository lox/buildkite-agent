package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/experiments"
	"github.com/buildkite/agent/v3/logger"
	"github.com/stretchr/testify/assert"
)

func findArtifact(artifacts []*api.Artifact, search string) *api.Artifact {
	for _, a := range artifacts {
		if filepath.Base(a.Path) == search {
			return a
		}
	}

	return nil
}

func TestCollect(t *testing.T) {
	// t.Parallel() cannot be used with experiments.Enable

	wd, _ := os.Getwd()
	root := filepath.Join(wd, "..")
	os.Chdir(root)
	defer os.Chdir(wd)

	volumeName := filepath.VolumeName(root)
	rootWithoutVolume := strings.TrimPrefix(root, volumeName)

	var testCases = []struct {
		Name         string
		Path         []string
		AbsolutePath string
		GlobPath     string
		FileSize     int
		Sha1Sum      string
		Sha256Sum    string
	}{
		{
			Name:         "Mr Freeze.jpg",
			Path:         []string{"test", "fixtures", "artifacts", "Mr Freeze.jpg"},
			AbsolutePath: filepath.Join(root, "test", "fixtures", "artifacts", "Mr Freeze.jpg"),
			GlobPath:     filepath.Join("test", "fixtures", "artifacts", "**", "*.jpg"),
			FileSize:     362371,
			Sha1Sum:      "f5bc7bc9f5f9c3e543dde0eb44876c6f9acbfb6b",
			Sha256Sum:    "0c657a363d92093e68224e0716ed8b8b5d4bbc3dfe9b026e32b241fc9b369d47",
		},
		{
			Name:         "Commando.jpg",
			Path:         []string{"test", "fixtures", "artifacts", "folder", "Commando.jpg"},
			AbsolutePath: filepath.Join(root, "test", "fixtures", "artifacts", "folder", "Commando.jpg"),
			GlobPath:     filepath.Join("test", "fixtures", "artifacts", "**", "*.jpg"),
			FileSize:     113000,
			Sha1Sum:      "811d7cb0317582e22ebfeb929d601cdabea4b3c0",
			Sha256Sum:    "fcfbe62fd7b6638165a61e8de901ac9df93fc1389906f2772bdefed5de115426",
		},
		{
			Name:         "The Terminator.jpg",
			Path:         []string{"test", "fixtures", "artifacts", "this is a folder with a space", "The Terminator.jpg"},
			AbsolutePath: filepath.Join(root, "test", "fixtures", "artifacts", "this is a folder with a space", "The Terminator.jpg"),
			GlobPath:     filepath.Join("test", "fixtures", "artifacts", "**", "*.jpg"),
			FileSize:     47301,
			Sha1Sum:      "ed76566ede9cb6edc975fcadca429665aad8785a",
			Sha256Sum:    "5b4228a4bbef3d9f676e0a2e8cf6ea06759124ef0fbdb27a6c35df8759fcd39d",
		},
		{
			Name:         "Smile.gif",
			Path:         []string{rootWithoutVolume[1:], "test", "fixtures", "artifacts", "gifs", "Smile.gif"},
			AbsolutePath: filepath.Join(root, "test", "fixtures", "artifacts", "gifs", "Smile.gif"),
			GlobPath:     filepath.Join(root, "test", "fixtures", "artifacts", "**", "*.gif"),
			FileSize:     2038453,
			Sha1Sum:      "bd4caf2e01e59777744ac1d52deafa01c2cb9bfd",
			Sha256Sum:    "fc5e8608c7772e4ae834fbc47eec3d902099eb3599f5191e40d9e3d9b3764b0e",
		},
	}

	uploader := NewArtifactUploader(logger.Discard, nil, ArtifactUploaderConfig{
		Paths: fmt.Sprintf("%s;%s",
			filepath.Join("test", "fixtures", "artifacts", "**/*.jpg"),
			filepath.Join(root, "test", "fixtures", "artifacts", "**/*.gif"),
		),
	})

	// For the normalised-upload-paths experiment, uploaded artifact paths are
	// normalised with Unix/URI style path separators, even on Windows.
	// Without the experiment on, we use the file path given by the file system
	//
	// To simulate that in this test, we collect artifacts from the file system
	// twice, once with the experiment explicitly disabled, and one with it
	// enabled. We then check the test cases against both sets of artifacts,
	// comparing to paths processed with filepath.Join (which uses native OS
	// path separators), and then with the experiment enabled and with the
	// path.Join function instead (which uses Unix/URI-style path separators,
	// regardless of platform)

	experimentKey := "normalised-upload-paths"
	experimentPrev := experiments.IsEnabled(experimentKey)
	defer func() {
		if experimentPrev {
			experiments.Enable(experimentKey)
		} else {
			experiments.Disable(experimentKey)
		}
	}()
	experiments.Disable("normalised-upload-paths")
	artifactsWithoutExperimentEnabled, err := uploader.Collect()
	if err != nil {
		t.Fatalf("[normalised-upload-paths disabled] uploader.Collect() error = %v", err)
	}
	assert.Equal(t, 5, len(artifactsWithoutExperimentEnabled))

	experiments.Enable("normalised-upload-paths")
	artifactsWithExperimentEnabled, err := uploader.Collect()
	if err != nil {
		t.Fatalf("[normalised-upload-paths enabled] uploader.Collect() error = %v", err)
	}
	assert.Equal(t, 5, len(artifactsWithExperimentEnabled))

	// These test cases use filepath.Join, which uses per-OS path separators;
	// this is the behaviour without normalised-upload-paths.
	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			a := findArtifact(artifactsWithoutExperimentEnabled, tc.Name)
			if a == nil {
				t.Fatalf("findArtifact(%q) == nil", tc.Name)
			}

			assert.Equal(t, filepath.Join(tc.Path...), a.Path)
			assert.Equal(t, tc.AbsolutePath, a.AbsolutePath)
			assert.Equal(t, tc.GlobPath, a.GlobPath)
			assert.Equal(t, tc.FileSize, int(a.FileSize))
			assert.Equal(t, tc.Sha1Sum, a.Sha1Sum)
			assert.Equal(t, tc.Sha256Sum, a.Sha256Sum)
		})
	}

	// These test cases uses filepath.ToSlash(), which always emits forward-slashes.
	// this is the behaviour with normalised-upload-paths.
	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			a := findArtifact(artifactsWithExperimentEnabled, tc.Name)
			if a == nil {
				t.Fatalf("findArtifact(%q) == nil", tc.Name)
			}

			// Note that the rootWithoutVolume component of some tc.Path values
			// may already have backslashes in them on Windows:
			// []string{"path\to\codebase", "test", "fixtures", "hello"}
			// So forward-slash joining them with path.Join(tc.Path...} isn't enough.
			forwardSlashed := filepath.ToSlash(filepath.Join(tc.Path...))

			assert.Equal(t, forwardSlashed, a.Path)
			assert.Equal(t, tc.AbsolutePath, a.AbsolutePath)
			assert.Equal(t, tc.GlobPath, a.GlobPath)
			assert.Equal(t, tc.FileSize, int(a.FileSize))
			assert.Equal(t, tc.Sha1Sum, a.Sha1Sum)
			assert.Equal(t, tc.Sha256Sum, a.Sha256Sum)
		})
	}
}

func TestCollectThatDoesntMatchAnyFiles(t *testing.T) {
	wd, _ := os.Getwd()
	root := filepath.Join(wd, "..")
	os.Chdir(root)
	defer os.Chdir(wd)

	uploader := NewArtifactUploader(logger.Discard, nil, ArtifactUploaderConfig{
		Paths: strings.Join([]string{
			filepath.Join("log", "*"),
			filepath.Join("tmp", "capybara", "**", "*"),
			filepath.Join("mkmf.log"),
			filepath.Join("log", "mkmf.log"),
		}, ";"),
	})

	artifacts, err := uploader.Collect()
	if err != nil {
		t.Fatalf("uploader.Collect() error = %v", err)
	}

	assert.Equal(t, len(artifacts), 0)
}

func TestCollectWithSomeGlobsThatDontMatchAnything(t *testing.T) {
	wd, _ := os.Getwd()
	root := filepath.Join(wd, "..")
	os.Chdir(root)
	defer os.Chdir(wd)

	uploader := NewArtifactUploader(logger.Discard, nil, ArtifactUploaderConfig{
		Paths: strings.Join([]string{
			filepath.Join("dontmatchanything", "*"),
			filepath.Join("dontmatchanything.zip"),
			filepath.Join("test", "fixtures", "artifacts", "**", "*.jpg"),
		}, ";"),
	})

	artifacts, err := uploader.Collect()
	if err != nil {
		t.Fatalf("uploader.Collect() error = %v", err)
	}

	if len(artifacts) != 4 {
		t.Errorf("len(artifacts) = %d, want 4", len(artifacts))
	}
}

func TestCollectWithSomeGlobsThatDontMatchAnythingFollowingSymlinks(t *testing.T) {
	wd, _ := os.Getwd()
	root := filepath.Join(wd, "..")
	os.Chdir(root)
	defer os.Chdir(wd)

	uploader := NewArtifactUploader(logger.Discard, nil, ArtifactUploaderConfig{
		Paths: strings.Join([]string{
			filepath.Join("dontmatchanything", "*"),
			filepath.Join("dontmatchanything.zip"),
			filepath.Join("test", "fixtures", "artifacts", "links", "folder-link", "dontmatchanything", "**", "*.jpg"),
			filepath.Join("test", "fixtures", "artifacts", "**", "*.jpg"),
		}, ";"),
		FollowSymlinks: true,
	})

	artifacts, err := uploader.Collect()
	if err != nil {
		t.Fatalf("uploader.Collect() error = %v", err)
	}

	if len(artifacts) != 5 {
		t.Errorf("len(artifacts) = %d, want 5", len(artifacts))
	}
}

func TestCollectWithDuplicateMatches(t *testing.T) {
	wd, _ := os.Getwd()
	root := filepath.Join(wd, "..")
	os.Chdir(root)
	defer os.Chdir(wd)

	uploader := NewArtifactUploader(logger.Discard, nil, ArtifactUploaderConfig{
		Paths: strings.Join([]string{
			filepath.Join("test", "fixtures", "artifacts", "**", "*.jpg"),
			filepath.Join("test", "fixtures", "artifacts", "folder", "Commando.jpg"), // dupe
		}, ";"),
	})

	artifacts, err := uploader.Collect()
	if err != nil {
		t.Fatalf("uploader.Collect() error = %v", err)
	}

	paths := []string{}
	for _, a := range artifacts {
		paths = append(paths, a.Path)
	}
	assert.ElementsMatch(
		t,
		[]string{
			filepath.Join("test", "fixtures", "artifacts", "Mr Freeze.jpg"),
			filepath.Join("test", "fixtures", "artifacts", "folder", "Commando.jpg"),
			filepath.Join("test", "fixtures", "artifacts", "this is a folder with a space", "The Terminator.jpg"),
			filepath.Join("test", "fixtures", "artifacts", "links", "terminator", "terminator2.jpg"),
		},
		paths,
	)
}

func TestCollectWithDuplicateMatchesFollowingSymlinks(t *testing.T) {
	wd, _ := os.Getwd()
	root := filepath.Join(wd, "..")
	os.Chdir(root)
	defer os.Chdir(wd)

	uploader := NewArtifactUploader(logger.Discard, nil, ArtifactUploaderConfig{
		Paths: strings.Join([]string{
			filepath.Join("test", "fixtures", "artifacts", "**", "*.jpg"),
			filepath.Join("test", "fixtures", "artifacts", "folder", "Commando.jpg"), // dupe
		}, ";"),
		FollowSymlinks: true,
	})

	artifacts, err := uploader.Collect()
	if err != nil {
		t.Fatalf("uploader.Collect() error = %v", err)
	}

	paths := []string{}
	for _, a := range artifacts {
		paths = append(paths, a.Path)
	}
	assert.ElementsMatch(
		t,
		[]string{
			filepath.Join("test", "fixtures", "artifacts", "Mr Freeze.jpg"),
			filepath.Join("test", "fixtures", "artifacts", "folder", "Commando.jpg"),
			filepath.Join("test", "fixtures", "artifacts", "this is a folder with a space", "The Terminator.jpg"),
			filepath.Join("test", "fixtures", "artifacts", "links", "terminator", "terminator2.jpg"),
			filepath.Join("test", "fixtures", "artifacts", "links", "folder-link", "terminator2.jpg"),
		},
		paths,
	)
}
