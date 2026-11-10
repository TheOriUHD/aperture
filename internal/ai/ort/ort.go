// Package ort is a minimal binding to the ONNX Runtime C API, loaded at run time
// with purego, so Aperture's binaries stay CGO-free: the shared library is a
// download (see internal/ai), not a build dependency.
//
// Only what object detection needs is bound: one session, float32 tensors in
// and out, and the CPU or CUDA execution provider. The C API is a table of
// function pointers that only ever grows, so the indices below (taken from
// onnxruntime_c_api.h at ORT_API_VERSION 23) hold for every later release.
// Each entry is bound with purego.RegisterFunc, which keeps pointer arguments
// alive and in place for the duration of the call.
//
// It is used by the apdetect worker process, never inside nvrd: a fault in a
// native library or a GPU driver must not take recording down with it.
package ort

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// APIVersion is the C API version requested from the library.
const APIVersion = 23

// Function-table indices in OrtApi (onnxruntime_c_api.h, ORT_API_VERSION 23).
const (
	idxGetErrorMessage                  = 2
	idxCreateEnv                        = 3
	idxCreateSession                    = 7
	idxRun                              = 9
	idxCreateSessionOptions             = 10
	idxSetSessionGraphOptimizationLevel = 23
	idxSetIntraOpNumThreads             = 24
	idxSetInterOpNumThreads             = 25
	idxSessionGetInputCount             = 30
	idxSessionGetOutputCount            = 31
	idxSessionGetInputTypeInfo          = 33
	idxSessionGetInputName              = 36
	idxSessionGetOutputName             = 37
	idxCreateTensorWithDataAsOrtValue   = 49
	idxGetTensorMutableData             = 51
	idxCastTypeInfoToTensorInfo         = 55
	idxGetDimensionsCount               = 61
	idxGetDimensions                    = 62
	idxGetTensorTypeAndShape            = 65
	idxCreateCpuMemoryInfo              = 69
	idxAllocatorFree                    = 76
	idxGetAllocatorWithDefaultOptions   = 78
	idxReleaseStatus                    = 93
	idxReleaseSession                   = 95
	idxReleaseValue                     = 96
	idxReleaseTypeInfo                  = 98
	idxReleaseTensorTypeAndShapeInfo    = 99
	idxReleaseSessionOptions            = 100
	idxGetAvailableProviders            = 125
	idxReleaseAvailableProviders        = 126
	idxAppendExecutionProviderCUDAV2    = 204
	idxCreateCUDAProviderOptions        = 205
	idxUpdateCUDAProviderOptions        = 206
	idxReleaseCUDAProviderOptions       = 208
	tableLen                            = 260
)

// Execution providers.
const (
	ProviderCPU  = "cpu"
	ProviderCUDA = "cuda"
)

// api holds the bound C functions. Every function returns an OrtStatus* (0 on
// success) unless it is a Release*.
type api struct {
	getErrorMessage           func(status uintptr) uintptr
	releaseStatus             func(status uintptr)
	createEnv                 func(level int32, logID *byte, out *uintptr) uintptr
	createSessionOptions      func(out *uintptr) uintptr
	releaseSessionOptions     func(so uintptr)
	setGraphOptimization      func(so uintptr, level int32) uintptr
	setIntraOpThreads         func(so uintptr, n int32) uintptr
	setInterOpThreads         func(so uintptr, n int32) uintptr
	createSession             func(env uintptr, path *byte, so uintptr, out *uintptr) uintptr
	releaseSession            func(s uintptr)
	sessionGetInputCount      func(s uintptr, out *uintptr) uintptr
	sessionGetOutputCount     func(s uintptr, out *uintptr) uintptr
	sessionGetInputTypeInfo   func(s uintptr, i uintptr, out *uintptr) uintptr
	sessionGetInputName       func(s uintptr, i uintptr, alloc uintptr, out *uintptr) uintptr
	sessionGetOutputName      func(s uintptr, i uintptr, alloc uintptr, out *uintptr) uintptr
	releaseTypeInfo           func(ti uintptr)
	castTypeInfoToTensorInfo  func(ti uintptr, out *uintptr) uintptr
	getDimensionsCount        func(info uintptr, out *uintptr) uintptr
	getDimensions             func(info uintptr, dims *int64, n uintptr) uintptr
	getTensorTypeAndShape     func(v uintptr, out *uintptr) uintptr
	releaseTensorTypeAndShape func(info uintptr)
	createCPUMemoryInfo       func(allocType int32, memType int32, out *uintptr) uintptr
	getDefaultAllocator       func(out *uintptr) uintptr
	allocatorFree             func(alloc uintptr, p uintptr) uintptr
	createTensor              func(mem uintptr, data unsafe.Pointer, dataLen uintptr, shape *int64, shapeLen uintptr, typ int32, out *uintptr) uintptr
	getTensorMutableData      func(v uintptr, out *uintptr) uintptr
	releaseValue              func(v uintptr)
	run                       func(s uintptr, runOpts uintptr, inNames *uintptr, inputs *uintptr, nIn uintptr, outNames *uintptr, nOut uintptr, outputs *uintptr) uintptr
	getAvailableProviders     func(out *uintptr, n *int32) uintptr
	releaseAvailableProviders func(list uintptr, n int32) uintptr
	createCUDAOptions         func(out *uintptr) uintptr
	updateCUDAOptions         func(opts uintptr, keys *uintptr, values *uintptr, n uintptr) uintptr
	appendCUDA                func(so uintptr, opts uintptr) uintptr
	releaseCUDAOptions        func(opts uintptr)
}

func (a *api) bind(table []uintptr) {
	reg := func(fn any, i int) { purego.RegisterFunc(fn, table[i]) }
	reg(&a.getErrorMessage, idxGetErrorMessage)
	reg(&a.releaseStatus, idxReleaseStatus)
	reg(&a.createEnv, idxCreateEnv)
	reg(&a.createSessionOptions, idxCreateSessionOptions)
	reg(&a.releaseSessionOptions, idxReleaseSessionOptions)
	reg(&a.setGraphOptimization, idxSetSessionGraphOptimizationLevel)
	reg(&a.setIntraOpThreads, idxSetIntraOpNumThreads)
	reg(&a.setInterOpThreads, idxSetInterOpNumThreads)
	reg(&a.createSession, idxCreateSession)
	reg(&a.releaseSession, idxReleaseSession)
	reg(&a.sessionGetInputCount, idxSessionGetInputCount)
	reg(&a.sessionGetOutputCount, idxSessionGetOutputCount)
	reg(&a.sessionGetInputTypeInfo, idxSessionGetInputTypeInfo)
	reg(&a.sessionGetInputName, idxSessionGetInputName)
	reg(&a.sessionGetOutputName, idxSessionGetOutputName)
	reg(&a.releaseTypeInfo, idxReleaseTypeInfo)
	reg(&a.castTypeInfoToTensorInfo, idxCastTypeInfoToTensorInfo)
	reg(&a.getDimensionsCount, idxGetDimensionsCount)
	reg(&a.getDimensions, idxGetDimensions)
	reg(&a.getTensorTypeAndShape, idxGetTensorTypeAndShape)
	reg(&a.releaseTensorTypeAndShape, idxReleaseTensorTypeAndShapeInfo)
	reg(&a.createCPUMemoryInfo, idxCreateCpuMemoryInfo)
	reg(&a.getDefaultAllocator, idxGetAllocatorWithDefaultOptions)
	reg(&a.allocatorFree, idxAllocatorFree)
	reg(&a.createTensor, idxCreateTensorWithDataAsOrtValue)
	reg(&a.getTensorMutableData, idxGetTensorMutableData)
	reg(&a.releaseValue, idxReleaseValue)
	reg(&a.run, idxRun)
	reg(&a.getAvailableProviders, idxGetAvailableProviders)
	reg(&a.releaseAvailableProviders, idxReleaseAvailableProviders)
	reg(&a.createCUDAOptions, idxCreateCUDAProviderOptions)
	reg(&a.updateCUDAOptions, idxUpdateCUDAProviderOptions)
	reg(&a.appendCUDA, idxAppendExecutionProviderCUDAV2)
	reg(&a.releaseCUDAOptions, idxReleaseCUDAProviderOptions)
}

// Runtime is a loaded ONNX Runtime library.
type Runtime struct {
	a       api
	version string
	env     uintptr
	mem     uintptr
	alloc   uintptr
}

// Load opens the library at path (libonnxruntime.so) and creates its
// environment. Libraries a GPU provider depends on are found by the dynamic
// loader, so the caller sets LD_LIBRARY_PATH for the process beforehand.
func Load(path string) (*Runtime, error) {
	lib, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", path, err)
	}
	sym, err := purego.Dlsym(lib, "OrtGetApiBase")
	if err != nil {
		return nil, fmt.Errorf("not an ONNX Runtime library: %w", err)
	}
	var getAPIBase func() uintptr
	purego.RegisterFunc(&getAPIBase, sym)
	base := getAPIBase()
	if base == 0 {
		return nil, errors.New("OrtGetApiBase returned nothing")
	}
	// struct OrtApiBase { const OrtApi* (*GetApi)(uint32_t); const char* (*GetVersionString)(void); }
	slots := unsafe.Slice((*uintptr)(cptr(base)), 2)
	var getAPI func(version uint32) uintptr
	var getVersion func() uintptr
	purego.RegisterFunc(&getAPI, slots[0])
	purego.RegisterFunc(&getVersion, slots[1])
	version := goString(getVersion())
	tbl := getAPI(APIVersion)
	if tbl == 0 {
		return nil, fmt.Errorf("ONNX Runtime %s does not offer C API version %d", version, APIVersion)
	}
	r := &Runtime{version: version}
	r.a.bind(unsafe.Slice((*uintptr)(cptr(tbl)), tableLen))

	logID := cString("aperture")
	if err := r.check(r.a.createEnv(3 /* errors only */, &logID[0], &r.env)); err != nil {
		return nil, fmt.Errorf("create environment: %w", err)
	}
	// OrtArenaAllocator = 1, OrtMemTypeDefault = 0.
	if err := r.check(r.a.createCPUMemoryInfo(1, 0, &r.mem)); err != nil {
		return nil, fmt.Errorf("memory info: %w", err)
	}
	if err := r.check(r.a.getDefaultAllocator(&r.alloc)); err != nil {
		return nil, fmt.Errorf("allocator: %w", err)
	}
	return r, nil
}

// Version is the library's version string, e.g. "1.23.2".
func (r *Runtime) Version() string { return r.version }

// Providers lists the execution providers this build of the library contains.
func (r *Runtime) Providers() []string {
	var list uintptr
	var n int32
	if r.check(r.a.getAvailableProviders(&list, &n)) != nil || list == 0 {
		return nil
	}
	ptrs := unsafe.Slice((*uintptr)(cptr(list)), n)
	out := make([]string, 0, n)
	for _, p := range ptrs {
		out = append(out, goString(p))
	}
	_ = r.check(r.a.releaseAvailableProviders(list, n))
	return out
}

// SessionOptions configures a session.
type SessionOptions struct {
	Provider string // ProviderCPU or ProviderCUDA
	Device   int    // GPU index for CUDA
	Threads  int    // intra-op threads for the CPU provider (0 = library default)
}

// Session is one loaded model. Run calls are serialised.
type Session struct {
	r        *Runtime
	h        uintptr
	mu       sync.Mutex
	inName   []byte
	outNames [][]byte
	// InputShape is the model's input shape, e.g. [1 3 416 416]; dynamic
	// dimensions are -1.
	InputShape []int64
	// Outputs names the model's outputs, in order.
	Outputs []string
}

// Tensor is one float output of a run.
type Tensor struct {
	Data  []float32
	Shape []int64
}

// NewSession loads a model file.
func (r *Runtime) NewSession(modelPath string, o SessionOptions) (*Session, error) {
	var so uintptr
	if err := r.check(r.a.createSessionOptions(&so)); err != nil {
		return nil, err
	}
	defer r.a.releaseSessionOptions(so)
	if err := r.check(r.a.setGraphOptimization(so, 99 /* ORT_ENABLE_ALL */)); err != nil {
		return nil, err
	}
	if o.Threads > 0 {
		if err := r.check(r.a.setIntraOpThreads(so, int32(o.Threads))); err != nil {
			return nil, err
		}
		_ = r.check(r.a.setInterOpThreads(so, 1))
	}
	if o.Provider == ProviderCUDA {
		if err := r.appendCUDA(so, o.Device); err != nil {
			return nil, fmt.Errorf("CUDA: %w", err)
		}
	}
	path := cString(modelPath)
	var h uintptr
	if err := r.check(r.a.createSession(r.env, &path[0], so, &h)); err != nil {
		return nil, fmt.Errorf("load model: %w", err)
	}
	s := &Session{r: r, h: h}
	var nIn, nOut uintptr
	if err := r.check(r.a.sessionGetInputCount(h, &nIn)); err != nil || nIn != 1 {
		s.Close()
		return nil, fmt.Errorf("model must have exactly one input (has %d)", nIn)
	}
	if err := r.check(r.a.sessionGetOutputCount(h, &nOut)); err != nil || nOut < 1 {
		s.Close()
		return nil, errors.New("model has no outputs")
	}
	var err error
	if s.inName, err = r.ioName(r.a.sessionGetInputName, h, 0); err != nil {
		s.Close()
		return nil, err
	}
	for i := uintptr(0); i < nOut; i++ {
		name, err := r.ioName(r.a.sessionGetOutputName, h, i)
		if err != nil {
			s.Close()
			return nil, err
		}
		s.outNames = append(s.outNames, name)
		s.Outputs = append(s.Outputs, string(name[:len(name)-1]))
	}
	if s.InputShape, err = r.inputShape(h); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (r *Runtime) appendCUDA(so uintptr, device int) error {
	var opts uintptr
	if err := r.check(r.a.createCUDAOptions(&opts)); err != nil {
		return err
	}
	defer r.a.releaseCUDAOptions(opts)
	keys, kstore := cStrings([]string{"device_id", "cudnn_conv_algo_search", "do_copy_in_default_stream"})
	vals, vstore := cStrings([]string{fmt.Sprint(device), "HEURISTIC", "1"})
	err := r.check(r.a.updateCUDAOptions(opts, &keys[0], &vals[0], uintptr(len(keys))))
	runtime.KeepAlive(kstore)
	runtime.KeepAlive(vstore)
	if err != nil {
		return err
	}
	return r.check(r.a.appendCUDA(so, opts))
}

func (r *Runtime) ioName(fn func(s, i, alloc uintptr, out *uintptr) uintptr, h, i uintptr) ([]byte, error) {
	var p uintptr
	if err := r.check(fn(h, i, r.alloc, &p)); err != nil {
		return nil, err
	}
	s := cString(goString(p))
	_ = r.check(r.a.allocatorFree(r.alloc, p))
	return s, nil
}

func (r *Runtime) inputShape(h uintptr) ([]int64, error) {
	var ti uintptr
	if err := r.check(r.a.sessionGetInputTypeInfo(h, 0, &ti)); err != nil {
		return nil, err
	}
	defer r.a.releaseTypeInfo(ti)
	var tensor uintptr // owned by ti
	if err := r.check(r.a.castTypeInfoToTensorInfo(ti, &tensor)); err != nil || tensor == 0 {
		return nil, errors.New("model input is not a tensor")
	}
	return r.dims(tensor)
}

func (r *Runtime) dims(info uintptr) ([]int64, error) {
	var n uintptr
	if err := r.check(r.a.getDimensionsCount(info, &n)); err != nil {
		return nil, err
	}
	if n == 0 || n > 8 {
		return nil, fmt.Errorf("unexpected tensor rank %d", n)
	}
	d := make([]int64, n)
	if err := r.check(r.a.getDimensions(info, &d[0], n)); err != nil {
		return nil, err
	}
	return d, nil
}

// Run feeds one float32 tensor of the given shape and returns the first
// output, copied into Go memory, with its shape.
func (s *Session) Run(input []float32, shape []int64) ([]float32, []int64, error) {
	out, err := s.run(input, shape, 1)
	if err != nil {
		return nil, nil, err
	}
	return out[0].Data, out[0].Shape, nil
}

// RunAll runs the model on one float input and returns every output (all
// must be float tensors).
func (s *Session) RunAll(input []float32, shape []int64) ([]Tensor, error) {
	return s.run(input, shape, len(s.outNames))
}

func (s *Session) run(input []float32, shape []int64, nOut int) ([]Tensor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.r
	if len(input) == 0 || len(shape) == 0 {
		return nil, errors.New("empty input")
	}
	var in uintptr
	// ONNX_TENSOR_ELEMENT_DATA_TYPE_FLOAT = 1. The tensor borrows input's memory.
	if err := r.check(r.a.createTensor(r.mem, unsafe.Pointer(&input[0]), uintptr(len(input)*4), &shape[0], uintptr(len(shape)), 1, &in)); err != nil {
		return nil, err
	}
	defer r.a.releaseValue(in)
	inNames := []uintptr{uintptr(unsafe.Pointer(&s.inName[0]))}
	outNames := make([]uintptr, nOut)
	for i := range outNames {
		outNames[i] = uintptr(unsafe.Pointer(&s.outNames[i][0]))
	}
	inputs := []uintptr{in}
	outputs := make([]uintptr, nOut)
	err := r.check(r.a.run(s.h, 0, &inNames[0], &inputs[0], 1, &outNames[0], uintptr(nOut), &outputs[0]))
	runtime.KeepAlive(input)
	runtime.KeepAlive(s.inName)
	runtime.KeepAlive(s.outNames)
	for _, o := range outputs {
		if o != 0 {
			defer r.a.releaseValue(o)
		}
	}
	if err != nil {
		return nil, err
	}
	res := make([]Tensor, nOut)
	for i, out := range outputs {
		var info uintptr
		if err := r.check(r.a.getTensorTypeAndShape(out, &info)); err != nil {
			return nil, err
		}
		dims, err := r.dims(info)
		r.a.releaseTensorTypeAndShape(info)
		if err != nil {
			return nil, err
		}
		n := 1
		for _, d := range dims {
			if d < 0 {
				return nil, fmt.Errorf("output has a dynamic shape %v", dims)
			}
			n *= int(d)
		}
		data := make([]float32, n)
		if n > 0 {
			var p uintptr
			if err := r.check(r.a.getTensorMutableData(out, &p)); err != nil {
				return nil, err
			}
			copy(data, unsafe.Slice((*float32)(cptr(p)), n))
		}
		res[i] = Tensor{Data: data, Shape: dims}
	}
	return res, nil
}

// Close releases the session.
func (s *Session) Close() {
	if s.h != 0 {
		s.r.a.releaseSession(s.h)
		s.h = 0
	}
}

// check turns an OrtStatus* into an error (0 = success).
func (r *Runtime) check(status uintptr) error {
	if status == 0 {
		return nil
	}
	msg := goString(r.a.getErrorMessage(status))
	r.a.releaseStatus(status)
	return errors.New(msg)
}

func cString(s string) []byte { return append([]byte(s), 0) }

// cStrings returns a C array of C strings and the storage to keep alive.
func cStrings(ss []string) ([]uintptr, [][]byte) {
	store := make([][]byte, len(ss))
	ptrs := make([]uintptr, len(ss))
	for i, s := range ss {
		store[i] = cString(s)
		ptrs[i] = uintptr(unsafe.Pointer(&store[i][0]))
	}
	return ptrs, store
}

func goString(p uintptr) string {
	if p == 0 {
		return ""
	}
	n := 0
	for *(*byte)(cptr(p + uintptr(n))) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(cptr(p)), n))
}

// cptr turns an address returned by C into a pointer. Such memory belongs to
// the library, not the Go heap, so the usual uintptr rules do not apply.
func cptr(u uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&u)) }
