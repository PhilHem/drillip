package main

import (
	"crypto/sha256"
	"fmt"
)

func fingerprint(ev *Event) string {
	h := sha256.New()

	if ev.Exception != nil && len(ev.Exception.Values) > 0 {
		// Exception: hash type + top frame location
		exc := ev.Exception.Values[0]
		h.Write([]byte(exc.Type))
		if exc.Stacktrace != nil && len(exc.Stacktrace.Frames) > 0 {
			f := exc.Stacktrace.Frames[len(exc.Stacktrace.Frames)-1]
			h.Write([]byte(f.Filename))
			h.Write([]byte(f.Function))
			h.Write([]byte(fmt.Sprintf("%d", f.Lineno)))
		}
	} else {
		// Message: hash the message content
		h.Write([]byte("message:"))
		msg := ev.messageText()
		h.Write([]byte(msg))
	}

	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}
