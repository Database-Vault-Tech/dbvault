package mysql

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"regexp"
	"strings"

	"github.com/dbvault/dbvault/backend/internal/engine"
)

// sandboxLine is the first line of dumps from mariadb-dump 11.4+. It makes
// the mariadb client refuse shell commands (\!) while restoring, but
// MySQL's own mysql client rejects it, so DBVault strips it from stored
// dumps (keeping them portable) and adds it back when restoring.
const sandboxLine = "/*M!999999\\- enable the sandbox mode */"

// lineReader returns a stream line by line, newline included. Lines longer
// than its buffer (multi-megabyte extended INSERTs) come back in pieces, so
// memory stays bounded.
type lineReader struct {
	r       *bufio.Reader
	atStart bool
}

func newLineReader(r io.Reader) *lineReader {
	return &lineReader{r: bufio.NewReaderSize(r, 256<<10), atStart: true}
}

func (l *lineReader) next() (piece []byte, lineStart bool, err error) {
	piece, err = l.r.ReadSlice('\n')
	lineStart = l.atStart
	l.atStart = err == nil // a full line ended; the next piece starts a line
	if errors.Is(err, bufio.ErrBufferFull) {
		err = nil
	}
	return piece, lineStart, err
}

// stripSandbox drops the sandbox line from the start of a dump stream.
func stripSandbox(r io.Reader) io.Reader {
	br := bufio.NewReaderSize(r, 4096)
	head, _ := br.Peek(len(sandboxLine) + 2)
	if bytes.HasPrefix(head, []byte(sandboxLine)) {
		_, _ = br.ReadSlice('\n')
	}
	return br
}

// definerRE matches DEFINER=`user`@`host` clauses (backticks inside names
// are doubled).
var definerRE = regexp.MustCompile("DEFINER=`(?:[^`]|``)*`@`(?:[^`]|``)*`")

// restoreFilter prepares a stored dump for the mariadb client: it enables
// sandbox mode and rewrites view/routine/trigger/event definers to
// CURRENT_USER. Definer accounts from the source server often don't exist
// (or lack rights) on the target, which makes restored views unusable or
// fails the restore outright.
type restoreFilter struct {
	src       *lineReader
	buf       bytes.Buffer
	rewritten int
	inData    bool // inside a long INSERT line split into pieces
	done      bool
}

func newRestoreFilter(r io.Reader) *restoreFilter {
	f := &restoreFilter{src: newLineReader(stripSandbox(r))}
	f.buf.WriteString(sandboxLine + "\n")
	return f
}

func (f *restoreFilter) Read(p []byte) (int, error) {
	for f.buf.Len() == 0 {
		if f.done {
			return 0, io.EOF
		}
		piece, start, err := f.src.next()
		if len(piece) > 0 {
			f.process(piece, start)
		}
		if err == io.EOF {
			f.done = true
		} else if err != nil {
			return 0, err
		}
	}
	return f.buf.Read(p)
}

func (f *restoreFilter) process(piece []byte, start bool) {
	if start {
		f.inData = bytes.HasPrefix(piece, []byte("INSERT INTO "))
	}
	// Data never needs rewriting, and a value could contain the text
	// "DEFINER=`" itself.
	if f.inData || !bytes.Contains(piece, []byte("DEFINER=`")) {
		f.buf.Write(piece)
		return
	}
	out := definerRE.ReplaceAllFunc(piece, func([]byte) []byte {
		f.rewritten++
		return []byte("DEFINER=CURRENT_USER")
	})
	f.buf.Write(out)
}

// createTableRE matches the CREATE TABLE line of mariadb-dump / mysqldump.
var createTableRE = regexp.MustCompile("^CREATE TABLE `((?:[^`]|``)+)` \\(")

// listTables returns the tables a dump creates (views are excluded: dumps
// create them with CREATE VIEW or a /*!50001 stand-in).
func listTables(ctx context.Context, r io.Reader) ([]engine.Table, error) {
	lr := newLineReader(r)
	var out []engine.Table
	for n := 0; ; n++ {
		if n%1024 == 0 && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		piece, start, err := lr.next()
		if start && len(piece) > 14 && piece[0] == 'C' {
			if m := createTableRE.FindSubmatch(piece); m != nil {
				out = append(out, engine.Table{Name: strings.ReplaceAll(string(m[1]), "``", "`")})
			}
		}
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
	}
}
