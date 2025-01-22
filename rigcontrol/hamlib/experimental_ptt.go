package hamlib

import (
	"log"
	"os"
	"strconv"
)

// Experimental PTT STATE 3 (https://github.com/la5nta/pat/issues/184)
var experimentalPTT3Enabled = func() bool {
	ok, _ := strconv.ParseBool(os.Getenv("EXPERIMENTAL_HAMLIB_PTT3"))
	if ok {
		log.Println("Experimental PTT3 enabled (https://github.com/la5nta/pat/issues/184)")
	}
	return ok
}()
