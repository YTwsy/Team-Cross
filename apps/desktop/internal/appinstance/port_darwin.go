package appinstance

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework CoreFoundation -framework Foundation
#include <stdlib.h>
#include <stdint.h>
void *tcPortCreate(char *name, uintptr_t identity);
void tcPortClose(void *port);
char *tcPortSend(char *name, char *data, int length);
char *tcIdentityPath(char *directory);
*/
import "C"

import (
	"errors"
	"sync"
	"sync/atomic"
	"unsafe"
)

var callbacks sync.Map
var nextIdentity atomic.Uint64

type messagePort struct {
	pointer  unsafe.Pointer
	identity uint64
}

func identityPath(directory string) (string, error) {
	input := C.CString(directory)
	defer C.free(unsafe.Pointer(input))
	output := C.tcIdentityPath(input)
	if output == nil {
		return "", errors.New("cannot resolve desktop identity")
	}
	defer C.free(unsafe.Pointer(output))
	return C.GoString(output), nil
}

//export tcReceive
func tcReceive(identity C.uintptr_t, data *C.char, length C.int) *C.char {
	if length < 0 || length > 1<<20 {
		return nil
	}
	callback, ok := callbacks.Load(uint64(identity))
	if !ok {
		return nil
	}
	response := callback.(func([]byte) []byte)(C.GoBytes(unsafe.Pointer(data), length))
	if response == nil {
		return nil
	}
	return C.CString(string(response))
}

func listen(name string, callback func([]byte) []byte) (*messagePort, error) {
	id := nextIdentity.Add(1)
	callbacks.Store(id, callback)
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	port := C.tcPortCreate(cname, C.uintptr_t(id))
	if port == nil {
		callbacks.Delete(id)
		return nil, errors.New("cannot create desktop message port")
	}
	return &messagePort{port, id}, nil
}

func (p *messagePort) close() {
	callbacks.Delete(p.identity)
	C.tcPortClose(p.pointer)
}

func send(name string, data []byte) []byte {
	cname, body := C.CString(name), C.CString(string(data))
	defer C.free(unsafe.Pointer(cname))
	defer C.free(unsafe.Pointer(body))
	reply := C.tcPortSend(cname, body, C.int(len(data)))
	if reply == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(reply))
	return []byte(C.GoString(reply))
}
