package main

import (
  "net/http"
  "sync"
  "unsafe"
)

/*
#include <stdlib.h>
 */
import "C"

/*
 * This object is passed to the request handler so that we can give it a lot
 * of context.
 */
type RequestContext interface {
  // Read-only information about the incoming HTTP request.
  Request() *http.Request

  // Object that may be used to generate a response. If a response is
  // written using this object, then the call to the target is skipped and
  // this response is sent directly back to the client.
  Response() http.ResponseWriter

  // An object that may be used to modify the request before it is
  // forwarded to the target
  ProxyRequest() *ProxyRequest

  // If this method is called, then filterFunc is called with the headers
  // returned from the target server. The caller will have the opportunity
  // to modify the headers.
  SetHeaderFilter(func (hdrs http.Header) http.Header)

  // If this method is called, then filterFunc will be called, possibly
  // multiple times, with the body returned from the target server.
  // The caller is responsible for returning the possibly-modified body.
  SetBodyFilter(func (body []byte, last bool) []byte)
}

/*
 * The final purpose of this whole module is to call this function.
 */
type RequestHandler interface {
  HandleRequest(ctx RequestContext)
}

/*
 * A global, thread-safe chunk table.
 */

type chunk struct {
  id uint32
  len uint32
  data unsafe.Pointer
}

var lastChunkID uint32
var chunks = make(map[uint32]chunk)
var chunkLock = sync.Mutex{}

/*
 * This is the actual C language interface to weaver. It is basically
 * a small C wrapper to the "manager."
 */

// Functions below are the public C-language API for this code.

/*
 * Create a new "request" object and return its unique ID. The request
 * goes in a map, so it's important that the caller always call
 * GoFreeRequest or there will be a memory leak.
 */
//export GoCreateRequest
func GoCreateRequest() uint32 {
  return CreateRequest()
}

/*
 * Clean up any storage used by the request. This method must be called for
 * every ID generated by GoCreateRequest or there will be a memory leak.
 */
//export GoFreeRequest
func GoFreeRequest(id uint32) {
  FreeRequest(id)
}

/*
 * Store a chunk of data. The pointer must already have been allocated
 * using "malloc" and the data must be valid for the length of the
 * request. A chunk ID will be returned.
 */
//export GoStoreChunk
func GoStoreChunk(data unsafe.Pointer, len uint32) uint32 {
  chunkLock.Lock()
  defer chunkLock.Unlock()

  lastChunkID++
  c := chunk{
    id: lastChunkID,
    len: len,
    data: data,
  }
  chunks[lastChunkID] = c
  return lastChunkID
}

/*
 * Free a chunk of data that was stored using GoStoreChunk. This only frees
 * the data used to track the chunk -- the caller is responsible for
 * actually calling "free".
 */
//export GoReleaseChunk
func GoReleaseChunk(id uint32) {
  releaseChunk(id)
}

/*
 * Retrieve the pointer to a chunk of data stored using "GoStoreChunk".
 */
//export GoGetChunk
func GoGetChunk(id uint32) unsafe.Pointer {
  return getChunk(id).data
}

/*
 * Retrieve the length of a specific chunk.
 */
//export GoGetChunkLength
func GoGetChunkLength(id uint32) uint32 {
  return getChunk(id).len
}

func getChunk(id uint32) chunk {
  chunkLock.Lock()
  defer chunkLock.Unlock()
  return chunks[id]
}

func releaseChunk(id uint32)  {
  chunkLock.Lock()
  defer chunkLock.Unlock()
  delete(chunks, id)
}

/*
 * Start parsing the new request. "rawHeaders" must be a string that
 * represents the HTTP request line and headers, separated by CRLF pairs,
 * exactly as described in the HTTP spec.
 * Once this function has been called, the request is already running.
 * The caller MUST periodically call "GoPollRequest" in order to get updates
 * on the status of the request, and MUST call "GoFreeRequest" after
 * the request is done.
 */
//export GoBeginRequest
func GoBeginRequest(id uint32, rawHeaders *C.char) {
  BeginRequest(id, C.GoString(rawHeaders))
}

/*
 * Poll for updates from the running request. Each update is returned as
 * a null-terminated string. The format of each command string is
 * described in the README.
 * If "block" is non-zero, then block until a command is present. Otherwise,
 * return immediately if there is no command on the queue.
 * The final response from the request will be "DONE." When this is called,
 * then no more commands will be returned. The caller must not poll
 * after "DONE" is returned.
 * The caller is responsible for calling "free" on the returned command string.
 */
//export GoPollRequest
func GoPollRequest(id uint32, block int32) *C.char {
  cmd := PollRequest(id, block != 0)
  if cmd == "" {
    return nil
  }
  return C.CString(cmd)
}

/*
 * Send a chunk of request data to the running goroutine. The second pointer,
 * if non-zero, indicates that this is the last chunk. "data" and "len"
 * must point to valid memory. A copy will be made before this function
 * call returns, so the caller is free to deallocate this memory
 * after calling this function.
 */
//export GoSendRequestBodyChunk
func GoSendRequestBodyChunk(id uint32, l int32, data unsafe.Pointer, len uint32) {
  var buf []byte
  if data != nil && len > 0 {
    buf := make([]byte, len)
    copy(buf[:], (*[1<<30]byte)(data)[:])
  }
  var last bool
  if l != 0 { last = true }
  SendRequestBodyChunk(id, last, buf)
}

/*
 * Transform a set of headers from the proxy response. The headers are passed
 * in a string in the same format as produced by the "WHDR" command.
 * The callback may return a new set of headers in the same format,
 * or NULL to indicate that the headers are unchanged.
 */
//export GoTransformHeaders
func GoTransformHeaders(id uint32, hdrs *C.char) *C.char {
  hdrString := C.GoString(hdrs)
  newHdrs := TransformHeaders(id, hdrString)
  if newHdrs == "" {
    return nil
  }
  return C.CString(newHdrs)
}

/*
 * This is a convenience function used to install a test handler that responds
 * to a particular set of API calls.
 */
//export GoInstallTestHandler
func GoInstallTestHandler() {
  SetTestRequestHandler()
}

func main() {
  panic("This is a library. No main.");
}
