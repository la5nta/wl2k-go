// Copyright 2021 Jeremy Bush (N8JJA). All rights reserved.
// Copyright 2026 Rhizomatica. All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package mercury

import (
	"fmt"
	"log"
	"os"
	"strconv"
)

// Set MERCURY_DEBUG=1 to enable verbose logging of the TNC control protocol.
var debugLogger = func() *log.Logger {
	if t, _ := strconv.ParseBool(os.Getenv("MERCURY_DEBUG")); !t {
		return nil
	}
	return log.New(os.Stderr, "[MERCURY] ", log.LstdFlags|log.Lmicroseconds|log.Lshortfile)
}()

func debugPrint(format string, args ...interface{}) {
	if debugLogger == nil {
		return
	}
	debugLogger.Output(2, fmt.Sprintf(format, args...))
}

// debugPrint3 is like debugPrint but with increased call depth for use
// with writeCmd so we're able to trace the origin of the write call.
func debugPrint3(format string, args ...interface{}) {
	if debugLogger == nil {
		return
	}
	debugLogger.Output(3, fmt.Sprintf(format, args...))
}
