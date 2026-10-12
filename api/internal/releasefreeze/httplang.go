package releasefreeze

import (
	"bufio"
	"errors"
	"io"
	"net"
	"time"
)

// The response language H of design §3.2: the status line, the field lines and
// the framing. Every rule here is a rule on bytes. No library integer,
// whitespace or case function is on this path: strconv.Atoi, strings.TrimSpace
// and strings.EqualFold accept more than H does, and they accept differently
// from Python's int() and str.strip(). A response outside H is not clear.

const (
	maxLine      = 1024 // rule: a line with its CRLF is at most 1024 bytes
	maxFields    = 64   // rule: at most 64 field lines in a head or a trailer
	maxInterim   = 8    // rule: at most 8 interim responses
	maxLenDigits = 4    // rule: Content-Length is 1 to 4 digits
	maxHexDigits = 4    // rule: a chunk size is 1 to 4 hex digits
)

// errNotH is every departure from H. transportToken reports it as "connection failed".
var errNotH = errors.New("connection failed")

type respHead struct {
	status int
	lines  []headerLine
}

// headerLine holds a field name folded to lower case (A-Z only) and its value
// without the SP and HTAB at either end.
type headerLine struct {
	name  string
	value string
}

func (h respHead) values(name string) []string {
	var out []string
	for _, ln := range h.lines {
		if ln.name == name {
			out = append(out, ln.value)
		}
	}
	return out
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isTchar(c byte) bool {
	if isDigit(c) || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
		return true
	}
	switch c {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~': // rule: the fifteen other token bytes
		return true
	}
	return false
}

func isFtext(c byte) bool {
	return c == 0x09 || (c >= 0x20 && c <= 0x7E) // rule: field text is HTAB or 0x20-0x7E
}

func lowerASCII(b []byte) string {
	out := make([]byte, len(b))
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}

// readLine returns one line without its CRLF.
func readLine(br *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		c, err := br.ReadByte()
		if err != nil {
			return nil, err
		}
		line = append(line, c)
		if c == '\n' {
			break
		}
		if len(line) >= maxLine {
			return nil, errNotH
		}
	}
	n := len(line)
	if n < 2 || line[n-2] != '\r' { // rule: a line ends with CRLF
		return nil, errNotH
	}
	return line[:n-2], nil
}

// parseStatusLine accepts "HTTP/1.1" SP 3DIGIT, optionally followed by SP and field text.
func parseStatusLine(line []byte) (int, bool) {
	const prefix = "HTTP/1.1 "
	if len(line) < len(prefix)+3 || string(line[:len(prefix)]) != prefix {
		return 0, false
	}
	d := line[len(prefix):]
	if !isDigit(d[0]) || !isDigit(d[1]) || !isDigit(d[2]) {
		return 0, false
	}
	code := int(d[0]-'0')*100 + int(d[1]-'0')*10 + int(d[2]-'0')
	rest := d[3:]
	if len(rest) == 0 {
		return code, true
	}
	if rest[0] != ' ' { // rule: SP between the status code and the reason
		return 0, false
	}
	for _, c := range rest[1:] {
		if !isFtext(c) {
			return 0, false
		}
	}
	return code, true
}

// parseField accepts 1*tchar ":" *ftext.
func parseField(line []byte) (string, string, bool) {
	i := 0
	for i < len(line) && isTchar(line[i]) {
		i++
	}
	if i == 0 || i >= len(line) || line[i] != ':' {
		return "", "", false
	}
	v := line[i+1:]
	for _, c := range v {
		if !isFtext(c) {
			return "", "", false
		}
	}
	a, b := 0, len(v)
	for a < b && (v[a] == ' ' || v[a] == '\t') {
		a++
	}
	for b > a && (v[b-1] == ' ' || v[b-1] == '\t') {
		b--
	}
	return lowerASCII(line[:i]), string(v[a:b]), true
}

// readFields reads field lines up to the empty line that ends them.
func readFields(br *bufio.Reader, conn net.Conn, deadline time.Time) ([]headerLine, error) {
	var out []headerLine
	for {
		armDeadline(conn, deadline)
		line, err := readLine(br)
		if err != nil {
			return nil, err
		}
		if len(line) == 0 {
			return out, nil
		}
		if len(out) >= maxFields {
			return nil, errNotH
		}
		name, value, ok := parseField(line)
		if !ok {
			return nil, errNotH
		}
		out = append(out, headerLine{name: name, value: value})
	}
}

func readHead(br *bufio.Reader, conn net.Conn, deadline time.Time) (respHead, error) {
	line, err := readLine(br)
	if err != nil {
		return respHead{}, err
	}
	status, ok := parseStatusLine(line)
	if !ok {
		return respHead{}, errNotH
	}
	lines, err := readFields(br, conn, deadline)
	if err != nil {
		return respHead{}, err
	}
	return respHead{status: status, lines: lines}, nil
}

// parseDecimal accepts 1 to maxLenDigits digits and nothing else.
func parseDecimal(s string) (int, bool) {
	if len(s) == 0 || len(s) > maxLenDigits {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return 0, false
		}
		n = n*10 + int(s[i]-'0')
	}
	return n, true
}

func chunkSizeText(line []byte) []byte { return line } // rule: no chunk extension

// parseChunkSize accepts 1 to maxHexDigits hex digits and nothing else.
func parseChunkSize(line []byte) (int, bool) {
	t := chunkSizeText(line)
	if len(t) == 0 || len(t) > maxHexDigits {
		return 0, false
	}
	n := 0
	for _, c := range t {
		var v int
		switch {
		case isDigit(c):
			v = int(c - '0')
		case c >= 'a' && c <= 'f':
			v = int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v = int(c-'A') + 10
		default:
			return 0, false
		}
		n = n*16 + v
	}
	return n, true
}

func readBody(br *bufio.Reader, conn net.Conn, deadline time.Time, head respHead) ([]byte, error) {
	te := head.values("transfer-encoding")
	cl := head.values("content-length")
	if len(te) > 0 {
		if len(cl) != 0 || len(te) != 1 { // rule: chunked excludes content-length and a second transfer-encoding
			return nil, errNotH
		}
		if lowerASCII([]byte(te[0])) != "chunked" { // rule: the only transfer coding is chunked
			return nil, errNotH
		}
		body, final, err := readChunked(br, conn, deadline)
		if err != nil {
			return nil, err
		}
		if !chunkFrameComplete(final) {
			return nil, errNotH
		}
		return body, nil
	}
	if len(cl) != 1 {
		return nil, errNotH
	}
	n, ok := parseDecimal(cl[0])
	if !ok {
		return nil, errNotH
	}
	if bodyTooBig(n) {
		return nil, errNotH
	}
	buf := make([]byte, n)
	armDeadline(conn, deadline)
	got, err := io.ReadFull(br, buf)
	if !lengthComplete(got, n) {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return buf[:got], nil
}

func readChunked(br *bufio.Reader, conn net.Conn, deadline time.Time) ([]byte, bool, error) {
	var body []byte
	for {
		armDeadline(conn, deadline)
		line, err := readLine(br)
		if err != nil {
			return body, false, err
		}
		size, ok := parseChunkSize(line)
		if !ok {
			return body, false, errNotH
		}
		if size == 0 {
			final, err := readTrailers(br, conn, deadline)
			return body, final, err
		}
		if bodyTooBig(len(body) + size) {
			return body, false, errNotH
		}
		chunk := make([]byte, size)
		armDeadline(conn, deadline)
		if _, err := io.ReadFull(br, chunk); err != nil {
			return body, false, err
		}
		body = append(body, chunk...)
		armDeadline(conn, deadline)
		var crlf [2]byte
		if _, err := io.ReadFull(br, crlf[:]); err != nil || crlf != [2]byte{'\r', '\n'} {
			return body, false, errNotH
		}
	}
}

// readTrailers reads the trailer section and the empty line that ends the body.
// A stream that ends before that empty line is reported as not final.
func readTrailers(br *bufio.Reader, conn net.Conn, deadline time.Time) (bool, error) {
	fields, err := readFields(br, conn, deadline)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, err
	}
	for _, f := range fields {
		if f.name == "content-encoding" || f.name == "content-length" || f.name == "transfer-encoding" { // rule: no framing field in a trailer
			return false, errNotH
		}
	}
	return true, nil
}
