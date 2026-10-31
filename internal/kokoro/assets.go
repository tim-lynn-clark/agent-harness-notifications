package kokoro

import (
	"fmt"
	"strings"
)

// Pinned versions. sherpa-onnx 2.x dropped espeak-ng and skips every word
// missing from the lexicon (most project names), so the engine stays on the
// last 1.x release, which falls back to espeak-ng for those words.
const (
	EngineVersion = "v1.12.40"
	ModelName     = "kokoro-multi-lang-v1_0"
)

// Asset is one archive Install downloads from the sherpa-onnx GitHub releases.
type Asset struct {
	Name   string // file name, the last element of URL
	URL    string
	SHA256 string
	Size   int64 // bytes, for progress output
	// top is the archive's top-level directory; extracted paths are relative to it.
	top string
	// files are extracted as-is; each must be present.
	files []string
	// dirs are extracted with everything below them.
	dirs []string
	// into is the install subdirectory the paths land in.
	into string
}

func engineAsset(platform, sha string, size int64, files ...string) Asset {
	name := "sherpa-onnx-" + EngineVersion + "-" + platform
	return Asset{
		Name:   name + ".tar.bz2",
		URL:    "https://github.com/k2-fsa/sherpa-onnx/releases/download/" + EngineVersion + "/" + name + ".tar.bz2",
		SHA256: sha,
		Size:   size,
		top:    name,
		files:  files,
	}
}

// engines are the shared-library builds of the engine, keyed by GOOS/GOARCH.
// Only the command-line synthesizer and the ONNX Runtime library it links are
// extracted. The Windows build is the MT (static C runtime) one, so it needs
// no Visual C++ redistributable.
var engines = map[string]Asset{
	"darwin/arm64": engineAsset("osx-arm64-shared",
		"d01596a71a3bfd1722a7730e0512146686aaf5ecca8b85cc343f4342d0c9ab6c", 25848598,
		"bin/sherpa-onnx-offline-tts", "lib/libonnxruntime.1.24.4.dylib"),
	"darwin/amd64": engineAsset("osx-x64-shared",
		"ecc6ba873f1a85df316f842a21a93fbe9475f25d5370a532ccf52c81a7ece26e", 28851662,
		"bin/sherpa-onnx-offline-tts", "lib/libonnxruntime.1.24.4.dylib"),
	"linux/amd64": engineAsset("linux-x64-shared",
		"f7bb558400a298c94aaebeba6f68cd2bcdd0a9484b63daaed3f45a2c246eb929", 26752110,
		"bin/sherpa-onnx-offline-tts", "lib/libonnxruntime.so"),
	"linux/arm64": engineAsset("linux-aarch64-shared-cpu",
		"d69893930cc31d8f2fe1e222670b8d3aaf90b0d83f87dd3a184991f611177961", 26576207,
		"bin/sherpa-onnx-offline-tts", "lib/libonnxruntime.so"),
	"windows/amd64": engineAsset("win-x64-shared-MT-Release",
		"7555e9fa5e30e5b47feea1700090086c0fa2a91dd09754922ac7d79a72f46f91", 23272584,
		"bin/sherpa-onnx-offline-tts.exe", "bin/onnxruntime.dll"),
}

// model is Kokoro-82M v1.0 at full precision, with the English lexicons and
// the espeak-ng data used for words the lexicons lack. The int8 build is a
// third of the size but quantisation leaves spoken names hissy and muffled
// next to the bundled clips, which are made from the same weights.
var model = Asset{
	Name:   ModelName + ".tar.bz2",
	URL:    "https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/" + ModelName + ".tar.bz2",
	SHA256: "60703707bc05ccd48d60e1eb723f38b07d6e474795a0b6a588d7220400e0f7a4",
	Size:   349886476,
	top:    ModelName,
	files:  []string{"model.onnx", "voices.bin", "tokens.txt", "lexicon-us-en.txt", "lexicon-gb-en.txt", "LICENSE"},
	dirs:   []string{"espeak-ng-data"},
	into:   "model",
}

// Assets lists the archives Install needs on goos/goarch.
func Assets(goos, goarch string) ([]Asset, error) {
	engine, ok := engines[goos+"/"+goarch]
	if !ok {
		return nil, fmt.Errorf("Kokoro is not available for %s/%s", goos, goarch)
	}
	return []Asset{engine, model}, nil
}

// wants reports whether rel, a slash-separated path below the archive's top
// directory, is extracted.
func (a Asset) wants(rel string) bool {
	for _, f := range a.files {
		if rel == f {
			return true
		}
	}
	for _, d := range a.dirs {
		if strings.HasPrefix(rel, d+"/") {
			return true
		}
	}
	return false
}
