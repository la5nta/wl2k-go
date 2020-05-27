package hamlib

import (
	"log"
	"os"
	"strconv"
)

// Experimental PTT STATE 3 (https://github.com/la5nta/pat/issues/184)

func init() {
	if experimentalPTT3Enabled() {
		log.Println("Experimental PTT3 enabled (https://github.com/la5nta/pat/issues/184)")
	}
}

func experimentalPTT3Enabled() bool {
	ok, _ := strconv.ParseBool(os.Getenv("EXPERIMENTAL_HAMLIB_PTT3"))
	return ok
}
