package container

import (
	"io"
	"sync"
	"time"

	k8s "k8s.io/kubernetes/pkg/kubelet/apis/cri/runtime/v1alpha2"
)

const (
	// timeFormat is the time format used in the log.
	timeFormat = time.RFC3339Nano
)

var (
	// delimiter is the delimiter for timestamp and stream type in log line.
	delimiter = []byte{' '}
)

// rfc3339NanoWriter wraps output so that it can be later parsed by k8s.
// CRI Log format example:
//   2016-10-06T00:17:09.669794202Z stdout P log content 1
//   2016-10-06T00:17:09.669794203Z stderr F log content 2
type rfc3339NanoWriter struct {
	stream k8s.LogStreamType

	sync.Mutex
	io.Writer
}

func (w *rfc3339NanoWriter) Write(p []byte) (int, error) {
	prefix := append([]byte(time.Now().Format(timeFormat)), delimiter...)
	prefix = append(prefix, w.stream...)
	prefix = append(prefix, delimiter...)
	prefix = append(prefix, 'F')
	prefix = append(prefix, delimiter...)
	line := append(prefix, p...)
	w.Lock()
	n, err := w.Writer.Write(line)
	w.Unlock()

	return n - len(prefix), err
}
