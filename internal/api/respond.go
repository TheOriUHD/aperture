package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"

	"github.com/TheOriUHD/aperture/internal/store"
)

// writeJSON encodes v as the response body. Every response passes through here, so
// this is where the API's array contract is enforced: a nil Go slice otherwise
// marshals to JSON `null`, but the OpenAPI spec promises an array — and a
// third-party client that trusts the spec crashes on `null.map(...)` (exactly how
// the extension manager went grey). emptyNilSlices coerces nil slices to [] at any
// depth, so every array field — in every existing and future endpoint — serializes
// as [], with no per-call-site orEmpty.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if v != nil {
		_ = json.NewEncoder(w).Encode(emptyNilSlices(v))
	}
}

// emptyNilSlices returns v with every nil slice (in structs, maps, slices, arrays,
// pointers, and interface values, recursively) replaced by a non-nil empty slice.
// nil→empty is semantically identical in Go, so mutating reference types shared with
// the copy is harmless. Panic-safe: any reflection surprise falls back to v as-is,
// so normalization can never break a response.
func emptyNilSlices(v any) (out any) {
	defer func() {
		if recover() != nil {
			out = v
		}
	}()
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	cp := reflect.New(rv.Type()).Elem()
	cp.Set(rv)
	coerceNilSlices(cp)
	return cp.Interface()
}

func coerceNilSlices(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			coerceNilSlices(v.Elem())
		}
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		inner := reflect.New(v.Elem().Type()).Elem()
		inner.Set(v.Elem())
		coerceNilSlices(inner)
		if v.CanSet() {
			v.Set(inner)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if f := v.Field(i); f.CanSet() {
				coerceNilSlices(f)
			}
		}
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return // []byte marshals as a base64 string, not an array — leave it
		}
		if v.IsNil() {
			if v.CanSet() {
				v.Set(reflect.MakeSlice(v.Type(), 0, 0))
			}
			return
		}
		for i := 0; i < v.Len(); i++ {
			coerceNilSlices(v.Index(i))
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			coerceNilSlices(v.Index(i))
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			ev := reflect.New(v.Type().Elem()).Elem()
			ev.Set(v.MapIndex(k))
			coerceNilSlices(ev)
			v.SetMapIndex(k, ev)
		}
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeErrorCode(w, status, codeForStatus(status), msg)
}

// writeErrorCode writes the v1 error envelope: a stable machine-readable `code`
// for programmatic handling alongside the human `error` prose (which the web app
// still reads, so adding `code` is non-breaking). Third-party clients match on
// `code`, never the English string. Handlers wanting a code more specific than
// the status default (e.g. forbidden.camera vs forbidden.scope) call this
// directly; everything else goes through writeError.
func writeErrorCode(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}

// codeForStatus is the default machine code for an HTTP status.
func codeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "validation"
	case http.StatusUnauthorized:
		return "unauthenticated"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	case http.StatusConflict:
		return "conflict"
	case http.StatusPreconditionFailed:
		return "precondition_failed"
	case http.StatusPreconditionRequired:
		return "precondition_required"
	case http.StatusRequestEntityTooLarge:
		return "payload_too_large"
	case http.StatusUnsupportedMediaType:
		return "unsupported"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusServiceUnavailable:
		return "unavailable"
	case http.StatusInternalServerError:
		return "internal"
	default:
		return "error"
	}
}

// writeStoreError maps store errors to HTTP responses.
func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeError(w, http.StatusInternalServerError, "internal error")
}

// decodeJSON reads a JSON body into v, rejecting unknown fields. The contract is
// deliberately strict for a public API: a request must carry ONLY the fields its
// operation declares. This is why adding a field to a *response* (e.g. the per-item
// etag in Item 2) can break a *write* — a client that spreads a fetched object back
// into a body now sends a field the request struct doesn't declare, and this 400s.
// Extension authors hit it whenever they round-trip a GET into a PUT; the OpenAPI
// info.description spells out the rule, and decodeErrMsg names the offending field
// so the failure is self-explanatory instead of a blank "invalid request body".
func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// decodeErrMsg turns a decodeJSON error into a client-facing message. For the
// DisallowUnknownFields case it names the offending field — turning a silent,
// hard-to-diagnose 400 into a one-line fix — and points at the usual cause (a
// response object echoed back into a request). Other malformed-body errors keep the
// generic text: their stdlib detail leaks Go types and isn't actionable. Pass the
// non-nil error from decodeJSON; the machine-readable code stays "validation".
func decodeErrMsg(err error) string {
	if err != nil && strings.HasPrefix(err.Error(), "json: unknown field ") {
		field := strings.TrimPrefix(err.Error(), "json: unknown field ")
		return "unknown field " + field + ": this endpoint accepts only its declared fields — " +
			"do not send server-only response fields (e.g. etag, id, createdAt) back in a request body"
	}
	return "invalid request body"
}

// msFromSec converts a unix-seconds column (e.g. created_at/updated_at, stored in
// seconds) to unix milliseconds for the wire. v1 normalizes every timestamp to
// unix ms and names the field with an "Ms" suffix so the unit is unambiguous
// (DD-WIRE-TIME); storage is left as-is. Fields already stored in ms (segments/
// events *_ts) are emitted directly.
func msFromSec(sec int64) int64 { return sec * 1000 }
