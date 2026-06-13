package rowpipe

import (
	"bufio"
	"encoding/csv"
	"io"
)

const writeBufBytes = 1 << 20

// Writer is a streaming CSV sink. Rows are encoded and buffered as they arrive;
// Flush must be called once at end of stream to drain the buffer.
type Writer struct {
	bw *bufio.Writer
	cw *csv.Writer
}

// WriterOptions configure encoding of outgoing records.
type WriterOptions struct {
	// Delimiter is the field separator (default ',').
	Delimiter rune
	// CRLF terminates records with \r\n instead of \n.
	CRLF bool
}

// NewWriter wraps w. Callers must call Flush when done.
func NewWriter(w io.Writer, opts WriterOptions) *Writer {
	bw := bufio.NewWriterSize(w, writeBufBytes)
	cw := csv.NewWriter(bw)
	if opts.Delimiter != 0 {
		cw.Comma = opts.Delimiter
	}
	cw.UseCRLF = opts.CRLF
	return &Writer{bw: bw, cw: cw}
}

// Write encodes one record.
func (w *Writer) Write(rec []string) error {
	return w.cw.Write(rec)
}

// Flush drains the csv encoder and the underlying buffer, returning the first
// error from either.
func (w *Writer) Flush() error {
	w.cw.Flush()
	if err := w.cw.Error(); err != nil {
		return err
	}
	return w.bw.Flush()
}
