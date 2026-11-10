// Package ai runs Aperture's own object recognition: it downloads an inference
// runtime (ONNX Runtime, with NVIDIA's CUDA libraries for a GPU) and a model on
// request, then runs them in the apdetect worker. Every download is pinned to an
// exact URL and SHA-256, so what runs is exactly what was published.
package ai

// Artifact is one file to download. Archives are unpacked, keeping only the
// files matching Keep (paths inside the archive, its top folder removed).
type Artifact struct {
	Name   string
	URL    string
	SHA256 string
	Size   int64  // bytes to download
	Kind   string // "tgz", "txz" or "file"
	Keep   []string
}

// Pack is an inference runtime for one kind of device.
type Pack struct {
	ID        string // "cpu" | "cuda"
	Name      string
	Version   string
	Artifacts []Artifact
	// Lib is libonnxruntime.so, relative to the pack folder; LibDirs are folders
	// (relative) whose libraries the runtime needs on its search path.
	Lib     string
	LibDirs []string
}

// Model is a detection model: one of the catalog's (downloaded on demand) or
// one the operator added (Custom).
type Model struct {
	ID       string
	Name     string // shown to people
	Detail   string
	Input    int // pixels per side
	Artifact Artifact
	// MsCPU and MsGPU are rough per-frame times (one 6-thread desktop CPU core
	// pair; one mid-range NVIDIA GPU), used to pick a default and show a hint.
	MsCPU, MsGPU float64

	Arch     string   // "yolox" (default) or "detr" (D-FINE, RT-DETR, RF-DETR)
	Labels   string   // label set: coco80 (default), obj365, or "custom" (Classes)
	Classes  []string // a custom model's class names
	AllInOne bool     // also finds parcels: no separate package model needed
	GPUOnly  bool     // too slow on a CPU to offer in the simple tiers
	Tier     string   // its place in the simple choice ("" = advanced only)
	License  string
	Custom   bool // added by the operator
}

// ArchOf is a model's format, "yolox" when unset.
func (m *Model) ArchOf() string {
	if m.Arch == "" {
		return "yolox"
	}
	return m.Arch
}

// ClassList is the model's class names.
func (m *Model) ClassList() []string {
	if len(m.Classes) > 0 {
		return m.Classes
	}
	return ClassNames(m.Labels)
}

// LabelSetOf is the model's label set, coco80 when unset.
func (m *Model) LabelSetOf() string {
	if m.Labels == "" {
		return LabelsCOCO
	}
	return m.Labels
}

// ONNX Runtime 1.31's CUDA 12 build carries kernels for every GPU from Pascal
// to Blackwell (RTX 50); Aperture's binding uses the C API at version 23, which
// every later release offers unchanged.
const ortVersion = "1.31.1"

const ortBase = "https://github.com/microsoft/onnxruntime/releases/download/v" + ortVersion + "/"
const nvBase = "https://developer.download.nvidia.com/compute/"

// Packs are the runtimes Aperture can install.
var Packs = []Pack{
	{
		ID: "cpu", Name: "ONNX Runtime (CPU)", Version: ortVersion,
		Artifacts: []Artifact{{
			Name: "onnxruntime", Kind: "tgz",
			URL:    ortBase + "onnxruntime-linux-x64-" + ortVersion + ".tgz",
			SHA256: "c849e36a1ef3e22d4ff217b9d1ce4a3cf5b9467b975610913ae959ad5b16bdc9",
			Size:   11413655,
			Keep:   []string{"lib/libonnxruntime.so*", "LICENSE", "ThirdPartyNotices.txt"},
		}},
		Lib: "onnxruntime/lib/libonnxruntime.so",
	},
	{
		// ONNX Runtime's CUDA build needs CUDA 12 and cuDNN 9; NVIDIA publishes
		// both as redistributable archives. CUDA 12.9 and cuDNN 9.28 cover every
		// GPU from Pascal to Blackwell (RTX 50 series). Only the GPU driver has
		// to be installed on the machine itself.
		ID: "cuda", Name: "ONNX Runtime with NVIDIA CUDA", Version: ortVersion + " · CUDA 12.9 · cuDNN 9.28",
		Artifacts: []Artifact{
			{
				Name: "onnxruntime", Kind: "tgz",
				URL:    ortBase + "onnxruntime-linux-x64-gpu_cuda12-" + ortVersion + ".tgz",
				SHA256: "57cd51f677eca19a269cdba31b0c16c15d73a8feea41f170aa8b750447ab1f70",
				Size:   450233079,
				// The TensorRT provider would need TensorRT itself; leave it out.
				Keep: []string{"lib/libonnxruntime.so*", "lib/libonnxruntime_providers_shared.so", "lib/libonnxruntime_providers_cuda.so", "LICENSE", "ThirdPartyNotices.txt"},
			},
			{
				Name: "cudart", Kind: "txz",
				URL:    nvBase + "cuda/redist/cuda_cudart/linux-x86_64/cuda_cudart-linux-x86_64-12.9.79-archive.tar.xz",
				SHA256: "1f6ad42d4f530b24bfa35894ccf6b7209d2354f59101fd62ec4a6192a184ce99",
				Size:   1514676,
				Keep:   []string{"lib/libcudart.so*", "LICENSE"},
			},
			{
				Name: "cublas", Kind: "txz",
				URL:    nvBase + "cuda/redist/libcublas/linux-x86_64/libcublas-linux-x86_64-12.9.1.4-archive.tar.xz",
				SHA256: "546addc4a9d82b8f23aa9ba9274b6bc0429a63008a31c759884ac24880796057",
				Size:   933611504,
				Keep:   []string{"lib/libcublas.so*", "lib/libcublasLt.so*", "LICENSE"},
			},
			{
				Name: "cufft", Kind: "txz",
				URL:    nvBase + "cuda/redist/libcufft/linux-x86_64/libcufft-linux-x86_64-11.4.1.4-archive.tar.xz",
				SHA256: "b0e65af59b0c2f6c8ed9f5552a9b375890855b7926ae2c0404d15dcf2565bda4",
				Size:   470942192,
				Keep:   []string{"lib/libcufft.so*", "LICENSE"},
			},
			{
				Name: "curand", Kind: "txz",
				URL:    nvBase + "cuda/redist/libcurand/linux-x86_64/libcurand-linux-x86_64-10.3.10.19-archive.tar.xz",
				SHA256: "48281b4caadb1cf790d44ac76b23c77d06f474c0b1799814f314aafec9258ad6",
				Size:   89582256,
				Keep:   []string{"lib/libcurand.so*", "LICENSE"},
			},
			{
				Name: "nvrtc", Kind: "txz",
				URL:    nvBase + "cuda/redist/cuda_nvrtc/linux-x86_64/cuda_nvrtc-linux-x86_64-12.9.86-archive.tar.xz",
				SHA256: "82913658363892dbc0f2638b070476234476e06e084fed60db861cb7e161a6af",
				Size:   114231976,
				Keep:   []string{"lib/libnvrtc*.so*", "LICENSE"},
			},
			{
				Name: "nvjitlink", Kind: "txz",
				URL:    nvBase + "cuda/redist/libnvjitlink/linux-x86_64/libnvjitlink-linux-x86_64-12.9.86-archive.tar.xz",
				SHA256: "392cac3144b52ba14900bc7259ea6405ae6da88a8c704eab9bbbcc9ba4824b07",
				Size:   54021940,
				Keep:   []string{"lib/libnvJitLink.so*", "LICENSE"},
			},
			{
				Name: "cudnn", Kind: "txz",
				URL:    nvBase + "cudnn/redist/cudnn/linux-x86_64/cudnn-linux-x86_64-9.28.1.4_cuda12-archive.tar.xz",
				SHA256: "bcd9fdf9e80812e400954eb948b1b59f41ac8bd20fd990d9aa5ada32745e6253",
				Size:   1027537968,
				Keep:   []string{"lib/libcudnn*.so*", "LICENSE"},
			},
		},
		Lib:     "onnxruntime/lib/libonnxruntime.so",
		LibDirs: []string{"onnxruntime/lib", "cudart/lib", "cublas/lib", "cufft/lib", "curand/lib", "nvrtc/lib", "nvjitlink/lib", "cudnn/lib"},
	},
}

const yoloxBase = "https://github.com/Megvii-BaseDetection/YOLOX/releases/download/0.1.1rc0/"

// Models are the detection models on offer, fastest first. The YOLOX ones and
// D-FINE-X COCO are trained on COCO's 80 classes. The all-in-one ones are
// D-FINE trained on Objects365 (365 classes: parcels and bags too); the D-FINE
// authors note such checkpoints "may be subject to the Objects365 dataset
// terms", so they say so where they are offered.
var Models = []Model{
	{
		ID: "yolox-nano", Name: "Fast", Detail: "YOLOX-Nano · for small CPUs", Input: 416, MsCPU: 9, MsGPU: 2,
		Tier: "fast", License: "Apache-2.0",
		Artifact: Artifact{Name: "yolox-nano", Kind: "file", URL: yoloxBase + "yolox_nano.onnx",
			SHA256: "c789161ed43c8269fcd4e67c67eeeb4e80c622da2eb296a20bc6007bd18a0b7d", Size: 3659407},
	},
	{
		ID: "yolox-tiny", Name: "Balanced", Detail: "YOLOX-Tiny · the default on a CPU", Input: 416, MsCPU: 30, MsGPU: 3,
		Tier: "balanced", License: "Apache-2.0",
		Artifact: Artifact{Name: "yolox-tiny", Kind: "file", URL: yoloxBase + "yolox_tiny.onnx",
			SHA256: "427cc366d34e27ff7a03e2899b5e3671425c262ea2291f88bb942bc1cc70b0f7", Size: 20219662},
	},
	{
		ID: "yolox-s", Name: "Accurate", Detail: "YOLOX-S · sharper, for strong CPUs", Input: 640, MsCPU: 110, MsGPU: 4,
		Tier: "accurate", License: "Apache-2.0",
		Artifact: Artifact{Name: "yolox-s", Kind: "file", URL: yoloxBase + "yolox_s.onnx",
			SHA256: "c5c2d13e59ae883e6af3b45daea64af4833a4951c92d116ec270d9ddbe998063", Size: 35858002},
	},
	{
		ID: "yolox-m", Name: "Best", Detail: "YOLOX-M · people, vehicles and animals", Input: 640, MsCPU: 300, MsGPU: 12,
		Tier: "best", License: "Apache-2.0",
		Artifact: Artifact{Name: "yolox-m", Kind: "file", URL: yoloxBase + "yolox_m.onnx",
			SHA256: "21ff6cfdeb53b013bac2249599e55f00bff3cfdfdab37ed7a4620818c1d15b3f", Size: 101259744},
	},
	{
		ID: "dfine-x-obj365", Name: "All-in-one", Detail: "D-FINE-X (Objects365) · people, vehicles, animals and parcels in one model; the default on a GPU",
		Input: 640, MsCPU: 900, MsGPU: 24, Arch: "detr", Labels: LabelsObjects365, AllInOne: true, GPUOnly: true, Tier: "all-in-one",
		License: "Apache-2.0 code; trained on Objects365, whose terms may limit commercial use",
		Artifact: Artifact{Name: "dfine-x-obj365", Kind: "file",
			URL:    hfBase + "dfine_x_obj365-ONNX/resolve/8b0ccdfc94d2a405acaaeceade13b6d2b4fccd16/onnx/model.onnx",
			SHA256: "8499ca27e2ee289f0b664a2def6c3d574ae2765186460f18454969cf0d14e137", Size: 251726476},
	},
	{
		ID: "dfine-l-obj365", Name: "All-in-one (lighter)", Detail: "D-FINE-L (Objects365) · the all-in-one model at half the size",
		Input: 640, MsCPU: 500, MsGPU: 16, Arch: "detr", Labels: LabelsObjects365, AllInOne: true, GPUOnly: true,
		License: "Apache-2.0 code; trained on Objects365, whose terms may limit commercial use",
		Artifact: Artifact{Name: "dfine-l-obj365", Kind: "file",
			URL:    hfBase + "dfine_l_obj365-ONNX/resolve/32323e9a0c74b22803a2ad5b315a8fed61801e6a/onnx/model.onnx",
			SHA256: "cd0dfa92a2e0e2ab3d4a7c2e6252ebc094aa77b67f2f9dc1a010dd350a9a2f3e", Size: 125936360},
	},
	{
		ID: "dfine-x-coco", Name: "Sharpest (COCO)", Detail: "D-FINE-X (COCO) · the most accurate for people, vehicles and animals; no parcels",
		Input: 640, MsCPU: 900, MsGPU: 24, Arch: "detr", Labels: LabelsCOCO, GPUOnly: true, License: "Apache-2.0",
		Artifact: Artifact{Name: "dfine-x-coco", Kind: "file",
			URL:    hfBase + "dfine_x_coco-ONNX/resolve/b67b16d98f1f95e2af0b6d0b8f74ad956557fe8c/onnx/model.onnx",
			SHA256: "644fb5124c9c035a6082f23419da693c19ac857bd984ab7af5e353779368a03b", Size: 251138448},
	},
}

const hfBase = "https://huggingface.co/onnx-community/"

// PackageModels find parcels (see dfine.go): D-FINE trained on Objects365,
// Apache-2.0 (Peterande/D-FINE, weights by ustc-community), as converted to
// ONNX by onnx-community. URLs are pinned to a commit.
//
// The small one on every device: on a real flat box at night it scored a
// steady 0.35-0.39 where D-FINE-M jumped between 0.19 and 0.41, and it's twice
// as fast. (D-FINE-M: dfine_m_obj365-ONNX at f78e6984…, 79 MB.)
var PackageModels = []Model{
	{
		ID: "dfine-s-obj365", Name: "Packages", Detail: "D-FINE-S (Objects365)", Input: 640, MsCPU: 150, MsGPU: 8,
		Arch: "detr", Labels: LabelsObjects365, License: "Apache-2.0 code; trained on Objects365, whose terms may limit commercial use",
		Artifact: Artifact{Name: "dfine-s-obj365", Kind: "file",
			URL:    hfBase + "dfine_s_obj365-ONNX/resolve/a61e4cdfe4f9d3188a305d91e37dbf38688ffbb8/onnx/model.onnx",
			SHA256: "372feaa33ac6ba67d7df8589628f6abc395b3c6981c4edc70dfcfe2949751120", Size: 42123225},
	},
}

// PackageModelFor is the package model for a device.
func PackageModelFor(d Device) *Model { return &PackageModels[0] }

// PackByID finds a pack.
func PackByID(id string) *Pack {
	for i := range Packs {
		if Packs[i].ID == id {
			return &Packs[i]
		}
	}
	return nil
}

// ModelByID finds a model.
func ModelByID(id string) *Model {
	for i := range Models {
		if Models[i].ID == id {
			return &Models[i]
		}
	}
	return nil
}
