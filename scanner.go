package fastjson

import (
	"errors"
)

// Scanner scans a series of JSON values. Values may be delimited by whitespace.
//
// Scanner may parse JSON lines ( http://jsonlines.org/ ).
//
// Scanner may be re-used for subsequent parsing.
//
// Scanner cannot be used from concurrent goroutines.
//
// Use Parser for parsing only a single JSON value.
type Scanner struct {
	// b contains a working copy of json value passed to Init.
	b []byte

	// s points to the next JSON value to parse.
	s string

	// err contains the last error.
	err error

	// v contains the last parsed JSON value.
	v *Value

	// c is used for caching JSON values.
	c cache
}

// Init initializes sc with the given s.
//
// s may contain multiple JSON values, which may be delimited by whitespace.
func (sc *Scanner) Init(s string) {
	sc.b = append(sc.b[:0], s...) // 重用底层字节切片
	sc.s = b2s(sc.b)              // 字节切片转字符串（零拷贝）
	sc.err = nil
	sc.v = nil
}

// InitBytes initializes sc with the given b.
//
// b may contain multiple JSON values, which may be delimited by whitespace.
func (sc *Scanner) InitBytes(b []byte) {
	sc.Init(b2s(b))
}

// Next parses the next JSON value from s passed to Init.
//
// Returns true on success. The parsed value is available via Value call.
//
// Returns false either on error or on the end of s.
// Call Error in order to determine the cause of the returned false.
func (sc *Scanner) Next() bool {
	// 有错误，不再继续
	if sc.err != nil {
		return false
	}

	// 跳过空白字符
	sc.s = skipWS(sc.s)
	if len(sc.s) == 0 { // 到达字符串末尾
		sc.err = errEOF
		return false
	}

	// 重置缓存，注意，因为底层数组是复用的，Next() 之后需要通过 Value() 访问当前值，下次 Next 之后此前的 Value 都可能失效。
	sc.c.reset()

	// 解析单个 JSON 值
	v, tail, err := parseValue(sc.s, &sc.c, 0)
	if err != nil {
		sc.err = err
		return false
	}

	sc.s = tail // 保存剩余字符串
	sc.v = v    // 存储解析结果
	return true
}

// Error returns the last error.
func (sc *Scanner) Error() error {
	if sc.err == errEOF {
		return nil
	}
	return sc.err
}

// Value returns the last parsed value.
//
// The value is valid until the Next call.
//
// 注意，在调用 Next() 之前，sc.v 指向的数据是安全的。
// 一旦调用下一次 Next()，缓存会被 reset，旧的 Value 就会失效（内部引用被覆盖/重用）。
func (sc *Scanner) Value() *Value {
	return sc.v
}

var errEOF = errors.New("end of s")
