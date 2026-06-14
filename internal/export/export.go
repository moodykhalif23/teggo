// Package export turns tabular data into downloadable CSV and XLSX. It is a
// pure encoder layer (no database): callers build a Table (column metadata +
// pre-rendered string rows) and stream it to an io.Writer. The data-export
// module supplies the Tables; the report builder keeps its own CSV path.
package export

import (
	"encoding/csv"
	"io"
)

type Column struct {
	Name    string
	Numeric bool
}

type Table struct {
	Columns []Column
	Rows    [][]string
}

// WriteCSV writes the whole table as CSV to w (RFC 4180 via encoding/csv).
func WriteCSV(w io.Writer, t Table) error {
	s, err := NewCSVStream(w, t.Columns)
	if err != nil {
		return err
	}
	for _, row := range t.Rows {
		if err := s.Write(row); err != nil {
			return err
		}
	}
	return s.Flush()
}

// CSVStream writes CSV incrementally, so a large export streams to the client
// with bounded memory rather than materializing every row first. NewCSVStream
// writes the header from the columns.
type CSVStream struct{ cw *csv.Writer }

func NewCSVStream(w io.Writer, cols []Column) (*CSVStream, error) {
	cw := csv.NewWriter(w)
	hdr := make([]string, len(cols))
	for i, c := range cols {
		hdr[i] = c.Name
	}
	if err := cw.Write(hdr); err != nil {
		return nil, err
	}
	return &CSVStream{cw: cw}, nil
}

// Write emits one row.
func (s *CSVStream) Write(row []string) error { return s.cw.Write(row) }

// Flush flushes buffered rows and returns any write error.
func (s *CSVStream) Flush() error {
	s.cw.Flush()
	return s.cw.Error()
}
