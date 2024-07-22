package cjsmodulelexer

import (
	"context"
	_ "embed"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"unicode/utf16"
	"unsafe"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

type Exports struct {
	Exports   []string
	Reexports []string
}

var wasm api.Module

var isLE = binary.NativeEndian.String() == "LittleEndian"

func reinterpretSlice[T, U uint8 | uint16 | uint32 | uint64 | int8 | int16 | int32 | int64 | complex64 | complex128 | float32 | float64](x []T) []U {
	if x == nil {
		return nil
	}
	if len(x) == 0 {
		return []U{}
	}
	return unsafe.Slice((*U)(unsafe.Pointer(&x[0])), int(unsafe.Sizeof(x[0]))*len(x))
}

func Parse(source string, name *string) (Exports, error) {
	sourceUTF16 := utf16.Encode([]rune(source))
	var name2 string
	if name == nil {
		name2 = "@"
	} else {
		name2 = *name
	}

	if wasm == nil {
		panic(fmt.Errorf("not initialized"))
	}

	len2 := len(sourceUTF16) + 1

	extraMem := float64(wasm.ExportedGlobal("__heap_base").Get()) + float64(len2*4) - float64(wasm.ExportedMemory("memory").Size())
	if extraMem > 0 {
		wasm.ExportedMemory("memory").Grow(uint32(math.Ceil(extraMem / 65536)))
	}

	addr, err := wasm.ExportedFunction("sa").Call(context.TODO(), uint64(len2))
	if err != nil {
		panic(fmt.Errorf("wasm.sa() %d: %w", len2, err))
	}
	buffer := make([]byte, len2)
	view := reinterpretSlice[byte, uint16](buffer)
	if isLE {
		copyLE(sourceUTF16, view)
	} else {
		copyBE(sourceUTF16, view)
	}
	wasm.ExportedMemory("memory").Write(uint32(addr[0]), buffer)

	errCode, err := wasm.ExportedFunction("parseCJS").Call(context.TODO(), addr[0], uint64(len(sourceUTF16)), 0, 0, 0, 0)
	if err != nil {
		panic(fmt.Errorf("wasm.parseCJS() %d, %d: %w", addr, len(sourceUTF16), err))
	}

	if errCode[0] != 0 {
		e1, err := wasm.ExportedFunction("e").Call(context.TODO())
		if err != nil {
			panic(err)
		}
		parseErr := parseError{message: fmt.Sprintf("parse error %s%d:%s:%s", name2, e1[0], "<TODO>", "<TODO>")}
		parseErr.idx = e1[0]
		if errCode[0] == 5 || errCode[0] == 6 || errCode[0] == 7 {
			code := "ERR_LEXER_ESM_SYNTAX"
			parseErr.code = &code
		}
		return Exports{}, parseErr
	}

	exports := map[string]struct{}{}
	reexports := map[string]struct{}{}
	unsafeGetters := map[string]struct{}{}

	for {
		rre, err := wasm.ExportedFunction("rre").Call(context.TODO())
		if err != nil {
			panic(err)
		}
		if rre[0] == 0 {
			break
		}

		res, err := wasm.ExportedFunction("res").Call(context.TODO())
		if err != nil {
			panic(err)
		}
		ree, err := wasm.ExportedFunction("ree").Call(context.TODO())
		if err != nil {
			panic(err)
		}
		reexptStr := decode(sourceUTF16[res[0]:ree[0]])
		if reexptStr != nil && len(*reexptStr) > 0 {
			reexports[*reexptStr] = struct{}{}
		}
	}

	for {
		ru, err := wasm.ExportedFunction("ru").Call(context.TODO())
		if err != nil {
			panic(err)
		}
		if ru[0] == 0 {
			break
		}

		us, err := wasm.ExportedFunction("us").Call(context.TODO())
		if err != nil {
			panic(err)
		}
		ue, err := wasm.ExportedFunction("ue").Call(context.TODO())
		if err != nil {
			panic(err)
		}
		unsafeGetters[*decode(sourceUTF16[us[0]:ue[0]])] = struct{}{}
	}

	for {
		re, err := wasm.ExportedFunction("re").Call(context.TODO())
		if err != nil {
			panic(err)
		}
		if re[0] == 0 {
			break
		}

		es, err := wasm.ExportedFunction("es").Call(context.TODO())
		if err != nil {
			panic(err)
		}
		ee, err := wasm.ExportedFunction("ee").Call(context.TODO())
		if err != nil {
			panic(err)
		}
		exptStr := decode(sourceUTF16[es[0]:ee[0]])
		if exptStr != nil && len(*exptStr) > 0 {
			exports[*exptStr] = struct{}{}
		}
	}

	exportsSlice := make([]string, len(exports))
	i := 0
	for k := range exports {
		exportsSlice[i] = k
		i++
	}

	reexportsSlice := make([]string, len(reexports))
	i = 0
	for k := range reexports {
		reexportsSlice[i] = k
		i++
	}

	return Exports{Exports: exportsSlice, Reexports: reexportsSlice}, nil
}

type parseError struct {
	message string
	idx     uint64
	code    *string
}

func (e parseError) Error() string {
	return e.message
}

func decode(str []uint16) *string {
	if str[0] == '"' || str[0] == '\'' {
		decoded, err := strconv.Unquote(string(utf16.Decode(str)))
		if err != nil {
			panic(err)
		}
		return &decoded
	} else {
		str := string(utf16.Decode(str))
		return &str
	}
}

func copyBE(src []uint16, outBuf16 []uint16) {
	len := len(src)
	i := 0
	for i < len {
		ch := src[i]
		outBuf16[i] = (ch&0xFF)<<8 | ch>>8
		i++
	}
}

func copyLE(src []uint16, outBuf16 []uint16) {
	len := len(src)
	i := 0
	for i < len {
		outBuf16[i] = src[i]
		i++
	}
}

//go:generate go run github.com/jcbhmr/go-curl/v8/cmd/curl -LO https://github.com/nodejs/cjs-module-lexer/raw/1.3.1/lib/lexer.wasm
//go:embed lexer.wasm
var lexerWASM []byte

var runtime wazero.Runtime

func init() {
	runtime = wazero.NewRuntime(context.TODO())

	mod, err := runtime.Instantiate(context.TODO(), lexerWASM)
	if err != nil {
		panic(fmt.Errorf("r.Instantiate() lexer.wasm: %w", err))
	}
	wasm = mod
}
