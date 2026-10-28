package crawler

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"sync"
)

// Workers keep the first fatal output error and stop queuing work. The caller
// receives it after all active workers have finished, preserving saved results.
var outputFailure struct {
	sync.Mutex
	err error
}

func resetOutputError() {
	outputFailure.Lock()
	defer outputFailure.Unlock()
	outputFailure.err = nil
}

func outputError() error {
	outputFailure.Lock()
	defer outputFailure.Unlock()
	return outputFailure.err
}

func failOutput(err error) {
	if err == nil {
		return
	}
	outputFailure.Lock()
	defer outputFailure.Unlock()
	if outputFailure.err == nil {
		outputFailure.err = err
		stopReason.CompareAndSwap("", "output failed")
		runCancel()
	}
}

// writeCSVRow checks opening, buffered writes, flushing, and closing. Callers
// serialize appends so records from concurrent workers cannot interleave.
func writeCSVRow(path string, create bool, row []string) (err error) {
	flags := os.O_WRONLY | os.O_APPEND
	if create {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return fmt.Errorf("open CSV %q: %w", path, err)
	}
	defer func() {
		err = errors.Join(err, f.Close())
		if err != nil {
			err = fmt.Errorf("write CSV %q: %w", path, err)
		}
	}()
	w := csv.NewWriter(f)
	err = w.Write(row)
	w.Flush()
	return errors.Join(err, w.Error())
}

func appendResult(row []string) bool {
	csvMu.Lock()
	defer csvMu.Unlock()
	if outputError() != nil {
		return false
	}
	if err := writeCSVRow(resultFile, false, row); err != nil {
		failOutput(err)
		return false
	}
	return true
}
