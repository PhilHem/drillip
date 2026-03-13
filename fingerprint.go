package main

import (
	"crypto/sha256"
	"fmt"
)

func fingerprint(ev *Event) string {
	h := sha256.New()
	if ev.Exception != nil && len(ev.Exception.Values) > 0 {
		exc := ev.Exception.Values[0]
		h.Write([]byte(exc.Type))
		if exc.Stacktrace != nil && len(exc.Stacktrace.Frames) > 0 {
			f := exc.Stacktrace.Frames[len(exc.Stacktrace.Frames)-1]
			h.Write([]byte(f.Filename))
			h.Write([]byte(f.Function))
			h.Write([]byte(fmt.Sprintf("%d", f.Lineno)))
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}
